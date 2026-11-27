package authrpc

import (
	"connectrpc.com/connect"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"github.com/MrAmperage/ReportBoxBackendCore/auth"
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"testing"
	"time"
)

type provider struct {
	key   ed25519.PublicKey
	mode  auth.ValidationMode
	err   error
	calls int
}

func (p *provider) ResolveKey(context.Context, string, string, string) (crypto.PublicKey, error) {
	return p.key, nil
}
func (p *provider) Validate(_ context.Context, _ auth.Identity, m auth.ValidationMode) error {
	p.mode = m
	p.calls++
	return p.err
}
func TestAuthorize(t *testing.T) {
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	p := &provider{key: pub}
	c := auth.DefaultConfig()
	c.Issuer = "issuer"
	c.Audience = "aud"
	v, e := auth.NewValidator(c, p, p)
	if e != nil {
		t.Fatal(e)
	}
	const read = "/example.v1.Service/Read"
	const write = "/example.v1.Service/Write"
	const login = "/example.v1.Service/Login"
	policies := map[string]Policy{read: {}, write: {Mode: auth.RemoteRequired, Check: func(ctx context.Context, i auth.Identity) error {
		if got, ok := auth.FromContext(ctx); !ok || got.Subject() != i.Subject() {
			t.Fatal("identity missing in permission check")
		}
		return auth.ErrPermissionDenied
	}}, login: {AllowUnauthenticated: true}}
	a, e := NewAuthorize(v, policies)
	if e != nil {
		t.Fatal(e)
	}
	delete(policies, read)
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{"iss": "issuer", "sub": "user", "aud": "aud", "exp": time.Now().Add(time.Minute).Unix()})
	token.Header["typ"] = "at+jwt"
	token.Header["kid"] = "key"
	raw, e := token.SignedString(priv)
	if e != nil {
		t.Fatal(e)
	}
	h := http.Header{"Authorization": {"Bearer " + raw}}
	type marker struct{}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), marker{}, "preserved"), time.Second)
	defer cancel()
	got, e := a(ctx, read, h)
	if e != nil || got.Value(marker{}) != "preserved" {
		t.Fatal(e)
	}
	if i, ok := auth.FromContext(got); !ok || i.Subject() != "user" {
		t.Fatal("identity missing")
	}
	if d, ok := got.Deadline(); !ok || d.IsZero() {
		t.Fatal("deadline lost")
	}
	anon, e := a(got, login, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := auth.FromContext(anon); ok {
		t.Fatal("public procedure retained identity")
	}
	if _, e = a(ctx, write, h); connect.CodeOf(e) != connect.CodePermissionDenied || p.mode != auth.RemoteRequired {
		t.Fatal(e)
	}
	if _, e = a(ctx, "/example.v1.Service/New", h); connect.CodeOf(e) != connect.CodePermissionDenied {
		t.Fatal(e)
	}
	for _, bad := range []http.Header{nil, {"Authorization": {"Bearer " + raw, "Bearer " + raw}}, {"Authorization": {"Bearer " + raw}, "authorization": {"Bearer " + raw}}, {"Authorization": {"Bearer  " + raw}}} {
		if _, e = a(ctx, read, bad); connect.CodeOf(e) != connect.CodeUnauthenticated {
			t.Fatal(e)
		}
	}
	p.err = auth.ErrRevoked
	if _, e = a(ctx, read, h); connect.CodeOf(e) != connect.CodeUnauthenticated {
		t.Fatal(e)
	}
	p.err = auth.ErrUnavailable
	if _, e = a(ctx, read, h); connect.CodeOf(e) != connect.CodeUnavailable {
		t.Fatal(e)
	}
	if _, e = NewAuthorize(v, map[string]Policy{"*": {}}); e == nil {
		t.Fatal("wildcard policy allowed")
	}
	if _, e = NewAuthorize(v, map[string]Policy{login: {AllowUnauthenticated: true, Mode: auth.RemoteRequired}}); e == nil {
		t.Fatal("ambiguous public policy")
	}
}
