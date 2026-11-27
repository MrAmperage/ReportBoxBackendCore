package auth

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

type keysFunc func(context.Context, string, string, string) (crypto.PublicKey, error)

func (f keysFunc) ResolveKey(c context.Context, i, k, a string) (crypto.PublicKey, error) {
	return f(c, i, k, a)
}

type stateFunc func(context.Context, Identity, ValidationMode) error

func (f stateFunc) Validate(c context.Context, i Identity, m ValidationMode) error { return f(c, i, m) }
func testConfig() Config {
	c := DefaultConfig()
	c.Issuer = "test-issuer"
	c.Audience = "test-service"
	return c
}
func testValidator(t *testing.T, s stateFunc) *Validator {
	t.Helper()
	v, e := NewValidator(testConfig(), keysFunc(func(context.Context, string, string, string) (crypto.PublicKey, error) { return nil, ErrUnknownKey }), s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func compact(h, p string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(h)) + "." + base64.RawURLEncoding.EncodeToString([]byte(p)) + ".YQ"
}

const testHeader = `{"alg":"EdDSA","typ":"at+jwt","kid":"key-1"}`

func payload(exp int64) string {
	return fmt.Sprintf(`{"iss":"test-issuer","sub":"alice","aud":["test-service"],"exp":%d,"rev":9007199254740993}`, exp)
}
func TestPreflight(t *testing.T) {
	v := testValidator(t, func(context.Context, Identity, ValidationMode) error { return nil })
	now := time.Now()
	p := payload(now.Add(time.Minute).Unix())
	valid := compact(testHeader, p)
	i, e := v.preflight(valid, now)
	if e != nil || i.Subject() != "alice" || i.KeyID() != "key-1" {
		t.Fatal("valid structure rejected")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", i, i), "alice") {
		t.Fatal("identity leaks into formatted logs")
	}
	a := i.Audience()
	a[0] = "changed"
	if i.Audience()[0] != "test-service" {
		t.Fatal("mutable audience")
	}
	r, _ := i.Claim("rev")
	r[0] = '0'
	r, _ = i.Claim("rev")
	var n int64
	if json.Unmarshal(r, &n) != nil || n != 9007199254740993 {
		t.Fatal("claim mutation or precision loss")
	}
	cases := map[string]string{
		"duplicate claim":  compact(testHeader, strings.Replace(p, `"sub":"alice"`, `"sub":"alice","s\u0075b":"bob"`, 1)),
		"nested duplicate": compact(testHeader, strings.Replace(p, `"rev":9007199254740993`, `"rev":{"a":1,"a":2}`, 1)),
		"duplicate header": compact(strings.Replace(testHeader, `"alg":"EdDSA"`, `"alg":"EdDSA","alg":"none"`, 1), p),
		"other issuer":     compact(testHeader, strings.Replace(p, "test-issuer", "attacker", 1)),
		"other audience":   compact(testHeader, strings.Replace(p, "test-service", "other", 1)),
		"none":             compact(strings.Replace(testHeader, "EdDSA", "none", 1), p),
		"wrong type":       compact(strings.Replace(testHeader, "at+jwt", "JWT", 1), p),
		"jku":              compact(strings.TrimSuffix(testHeader, "}")+`,"jku":"https://example.invalid"}`, p),
		"bad kid":          compact(strings.Replace(testHeader, "key-1", "../../etc", 1), p),
		"expired":          compact(testHeader, payload(now.Add(-time.Minute).Unix())),
		"fraction":         compact(testHeader, strings.Replace(p, fmt.Sprintf(`"exp":%d`, now.Add(time.Minute).Unix()), `"exp":1.5`, 1)),
		"null expiry":      compact(testHeader, strings.Replace(p, fmt.Sprintf(`"exp":%d`, now.Add(time.Minute).Unix()), `"exp":null`, 1)),
		"trailing JSON":    compact(testHeader, p+`{}`),
		"invalid UTF8":     compact(testHeader, strings.Replace(p, "alice", string([]byte{0xff}), 1)),
		"empty signature":  strings.TrimSuffix(valid, "YQ"), "padding": valid + "=", "extra segment": valid + ".YQ", "newline": valid + "\n", "oversize": strings.Repeat("a", v.cfg.MaxTokenBytes+1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, e := v.preflight(raw, now); !errors.Is(e, ErrInvalidToken) {
				t.Fatalf("unexpected error %v", e)
			}
		})
	}
}
func TestStateContract(t *testing.T) {
	calls := 0
	ret := error(nil)
	mode := LocalPreferred
	v := testValidator(t, func(_ context.Context, _ Identity, m ValidationMode) error { calls++; mode = m; return ret })
	i, e := v.preflight(compact(testHeader, payload(time.Now().Add(time.Minute).Unix())), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if e = v.Revalidate(context.Background(), i, RemoteRequired); e != nil || mode != RemoteRequired || calls != 1 {
		t.Fatal("mode not enforced")
	}
	for _, c := range []struct{ in, out error }{{ErrRevoked, ErrRevoked}, {ErrInvalidToken, ErrRevoked}, {ErrPermissionDenied, ErrPermissionDenied}, {errors.New("sensitive details"), ErrUnavailable}} {
		ret = c.in
		if e = v.Revalidate(context.Background(), i, LocalPreferred); e != c.out {
			t.Fatalf("unsafe mapping %v", e)
		}
	}
	before := calls
	if e = v.Revalidate(context.Background(), Identity{}, LocalPreferred); e != ErrInvalidToken || calls != before {
		t.Fatal("zero identity accepted")
	}
	if e = v.Revalidate(context.Background(), i, 99); e != ErrUnavailable || calls != before {
		t.Fatal("invalid mode accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e = v.Revalidate(ctx, i, RemoteRequired); e != context.Canceled || calls != before {
		t.Fatal("cancellation lost")
	}
	other := testValidator(t, func(context.Context, Identity, ValidationMode) error { return nil })
	if e = other.Revalidate(context.Background(), i, LocalPreferred); e != ErrInvalidToken {
		t.Fatal("cross-domain identity accepted")
	}
	i.expiresAt = time.Now().Add(-time.Minute)
	if e = v.Revalidate(context.Background(), i, LocalPreferred); e != ErrInvalidToken {
		t.Fatal("expired stream identity accepted")
	}
}
func TestStateTimeout(t *testing.T) {
	c := testConfig()
	c.ProviderTimeout = time.Millisecond
	v, e := NewValidator(c, keysFunc(func(context.Context, string, string, string) (crypto.PublicKey, error) { return nil, ErrUnknownKey }), stateFunc(func(c context.Context, _ Identity, _ ValidationMode) error { <-c.Done(); return nil }))
	if e != nil {
		t.Fatal(e)
	}
	i, e := v.preflight(compact(testHeader, payload(time.Now().Add(time.Minute).Unix())), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if e = v.Revalidate(context.Background(), i, LocalPreferred); e != ErrUnavailable {
		t.Fatal("late positive result accepted", e)
	}
}
func TestConfigAndKeyTypes(t *testing.T) {
	c := testConfig()
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	c.Algorithm = "HS256"
	if c.Validate() == nil {
		t.Fatal("symmetric algorithm allowed")
	}
	c = testConfig()
	if _, e := NewValidator(c, keysFunc(nil), stateFunc(func(context.Context, Identity, ValidationMode) error { return nil })); e == nil {
		t.Fatal("nil provider allowed")
	}
	t.Setenv("AUTH_ISSUER", "test")
	t.Setenv("AUTH_AUDIENCE", "aud")
	t.Setenv("AUTH_CLOCK_SKEW", "nope")
	if _, e := LoadConfig(); e == nil {
		t.Fatal("invalid ENV accepted")
	}
	if !validKey(make(ed25519.PublicKey, 32), "EdDSA") || validKey([]byte(make([]byte, 32)), "EdDSA") || validKey(&rsa.PublicKey{N: big.NewInt(2), E: 65537}, "RS256") || validKey((*rsa.PublicKey)(nil), "RS256") {
		t.Fatal("key type/size validation failed")
	}
}
