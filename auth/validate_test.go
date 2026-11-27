package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func signed(t *testing.T, method jwt.SigningMethod, key any, claims jwt.MapClaims, typ string) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	token.Header["typ"] = typ
	token.Header["kid"] = "key-1"
	s, e := token.SignedString(key)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func claims() jwt.MapClaims {
	return jwt.MapClaims{"iss": "test-issuer", "sub": "alice", "aud": []string{"test-service"}, "exp": time.Now().Add(time.Minute).Unix(), "rev": int64(9007199254740993)}
}
func TestValidateSignatures(t *testing.T) {
	edpub, edpriv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	rsapriv, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	ecpriv, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		method    jwt.SigningMethod
		pub, priv any
	}{{jwt.SigningMethodEdDSA, edpub, edpriv}, {jwt.SigningMethodRS256, &rsapriv.PublicKey, rsapriv}, {jwt.SigningMethodES256, &ecpriv.PublicKey, ecpriv}} {
		t.Run(tc.method.Alg(), func(t *testing.T) {
			c := testConfig()
			c.Algorithm = tc.method.Alg()
			calls := 0
			v, e := NewValidator(c, keysFunc(func(_ context.Context, i, k, a string) (crypto.PublicKey, error) {
				if i != c.Issuer || k != "key-1" || a != c.Algorithm {
					t.Fatal("bad key lookup")
				}
				return tc.pub, nil
			}), stateFunc(func(_ context.Context, i Identity, m ValidationMode) error {
				calls++
				if i.Subject() != "alice" || m != RemoteRequired {
					t.Fatal("bad state request")
				}
				return nil
			}))
			if e != nil {
				t.Fatal(e)
			}
			raw := signed(t, tc.method, tc.priv, claims(), "at+jwt")
			i, e := v.Validate(context.Background(), raw, RemoteRequired)
			if e != nil || i.Subject() != "alice" || calls != 1 {
				t.Fatal("valid token rejected", e)
			}
			// Corrupt the signature: state must never be called.
			parts := strings.Split(raw, ".")
			last := parts[2]
			if last[0] == 'A' {
				last = "B" + last[1:]
			} else {
				last = "A" + last[1:]
			}
			bad := parts[0] + "." + parts[1] + "." + last
			if _, e = v.Validate(context.Background(), bad, RemoteRequired); e != ErrInvalidToken || calls != 1 {
				t.Fatal("bad signature accepted", e)
			}
		})
	}
}
func TestValidateFailurePaths(t *testing.T) {
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	c := testConfig()
	keyCalls, stateCalls := 0, 0
	var keyErr, stateErr error
	v, e := NewValidator(c, keysFunc(func(context.Context, string, string, string) (crypto.PublicKey, error) {
		keyCalls++
		return pub, keyErr
	}), stateFunc(func(context.Context, Identity, ValidationMode) error { stateCalls++; return stateErr }))
	if e != nil {
		t.Fatal(e)
	}
	raw := signed(t, jwt.SigningMethodEdDSA, priv, claims(), "at+jwt")
	wrong := claims()
	wrong["iss"] = "untrusted"
	for _, bad := range []string{signed(t, jwt.SigningMethodEdDSA, priv, wrong, "at+jwt"), signed(t, jwt.SigningMethodEdDSA, priv, claims(), "refresh+jwt"), signed(t, jwt.SigningMethodHS256, []byte("test"), claims(), "at+jwt")} {
		if _, e = v.Validate(context.Background(), bad, LocalPreferred); e != ErrInvalidToken || keyCalls != 0 || stateCalls != 0 {
			t.Fatal("untrusted header/claims reached provider", e)
		}
	}
	keyErr = ErrUnknownKey
	if _, e = v.Validate(context.Background(), raw, LocalPreferred); e != ErrInvalidToken || stateCalls != 0 {
		t.Fatal(e)
	}
	keyErr = errors.New("secret upstream error")
	if _, e = v.Validate(context.Background(), raw, LocalPreferred); e != ErrUnavailable || stateCalls != 0 {
		t.Fatal(e)
	}
	keyErr = nil
	stateErr = ErrRevoked
	if i, e := v.Validate(context.Background(), raw, RemoteRequired); e != ErrRevoked || i.owner != nil {
		t.Fatal("revoked token exposed identity", e)
	}
}

func TestKeyTimeout(t *testing.T) {
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	c := testConfig()
	c.ProviderTimeout = time.Millisecond
	v, e := NewValidator(c, keysFunc(func(ctx context.Context, _, _, _ string) (crypto.PublicKey, error) { <-ctx.Done(); return pub, nil }), stateFunc(func(context.Context, Identity, ValidationMode) error {
		t.Fatal("late key reached state provider")
		return nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = v.Validate(context.Background(), signed(t, jwt.SigningMethodEdDSA, priv, claims(), "at+jwt"), LocalPreferred); e != ErrUnavailable {
		t.Fatal(e)
	}
}
