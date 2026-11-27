// auth-demo demonstrates the local authorization boundary with ephemeral keys.
// It is not a token issuer or a production state provider.
package main

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"time"

	core "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/auth"
	authrpc "github.com/MrAmperage/ReportBoxBackendCore/auth/rpc"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/fx"
)

type demoProvider struct{ key ed25519.PublicKey }

func (p demoProvider) ResolveKey(_ context.Context, issuer, kid, algorithm string) (crypto.PublicKey, error) {
	if issuer != "demo-issuer" || kid != "demo-key" || algorithm != "EdDSA" {
		return nil, auth.ErrUnknownKey
	}
	return p.key, nil
}
func (p demoProvider) Validate(_ context.Context, i auth.Identity, mode auth.ValidationMode) error {
	// Only this in-memory demo identity is accepted. There is no remote service.
	if mode == auth.RemoteRequired {
		return auth.ErrUnavailable
	}
	if i.Subject() != "demo-user" || i.KeyID() != "demo-key" {
		return auth.ErrRevoked
	}
	return nil
}
func main() {
	if e := run(); e != nil {
		log.Fatal("auth demo failed")
	}
}
func run() (err error) {
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return e
	}
	p := demoProvider{key: pub}
	cfg := auth.DefaultConfig()
	cfg.Issuer = "demo-issuer"
	cfg.Audience = "demo-service"
	var v *auth.Validator
	a := core.New(app.Info{Name: "auth-demo"}, auth.Module(cfg), fx.Provide(func() auth.KeyProvider { return p }, func() auth.StateValidator { return p }), fx.Populate(&v))
	if e = a.Err(); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e = a.Start(ctx); e != nil {
		return e
	}
	defer func() {
		c, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if e := a.Stop(c); err == nil {
			err = e
		}
	}()
	const procedure = "/example.v1.Demo/Read"
	authorize, e := authrpc.NewAuthorize(v, map[string]authrpc.Policy{procedure: {}})
	if e != nil {
		return e
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{"iss": cfg.Issuer, "aud": cfg.Audience, "sub": "demo-user", "exp": time.Now().Add(time.Minute).Unix()})
	token.Header["kid"] = "demo-key"
	token.Header["typ"] = cfg.TokenType
	raw, e := token.SignedString(priv)
	if e != nil {
		return e
	}
	h := http.Header{}
	h.Set("Authorization", "Bearer "+raw)
	verified, e := authorize(ctx, procedure, h)
	if e != nil {
		return e
	}
	if _, ok := auth.FromContext(verified); !ok {
		return auth.ErrInvalidToken
	}
	fmt.Println("demo identity verified")
	return nil
}
