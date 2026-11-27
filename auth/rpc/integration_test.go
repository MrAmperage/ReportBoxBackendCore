package authrpc

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	core "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/auth"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
	"github.com/MrAmperage/ReportBoxBackendCore/rpc"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/fx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type integrationProvider struct {
	key     ed25519.PublicKey
	revoked atomic.Bool
	calls   atomic.Int64
}

func (p *integrationProvider) ResolveKey(context.Context, string, string, string) (crypto.PublicKey, error) {
	return p.key, nil
}
func (p *integrationProvider) Validate(_ context.Context, _ auth.Identity, mode auth.ValidationMode) error {
	p.calls.Add(1)
	if mode != auth.RemoteRequired {
		return auth.ErrUnavailable
	}
	if p.revoked.Load() {
		return auth.ErrRevoked
	}
	return nil
}

// Tests the actual BackendCore RPC server, Fx composition and request context.
func TestRPCIntegration(t *testing.T) {
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	p := &integrationProvider{key: pub}
	cfg := auth.DefaultConfig()
	cfg.Issuer = "integration-issuer"
	cfg.Audience = "integration-audience"
	sc := rpc.DefaultServerConfig()
	sc.Address = "127.0.0.1:0"
	const prefix = "/example.v1.Protected/"
	const procedure = prefix + "Read"
	var server *rpc.Server
	a := core.NewWithConfig(app.Info{Name: "auth-integration"}, config.Defaults(), auth.Module(cfg), fx.Provide(func() auth.KeyProvider { return p }, func() auth.StateValidator { return p }), rpc.ServerModule(sc), rpc.ProvideHandler(func(v *auth.Validator) (rpc.Handler, error) {
		authorize, e := NewAuthorize(v, map[string]Policy{procedure: {Mode: auth.RemoteRequired}})
		if e != nil {
			return rpc.Handler{}, e
		}
		return rpc.Handler{Authorize: authorize, Build: func(opts ...connect.HandlerOption) (string, http.Handler) {
			return prefix, connect.NewUnaryHandler(procedure, func(ctx context.Context, _ *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
				i, ok := auth.FromContext(ctx)
				if !ok || rpc.RequestID(ctx) == "" {
					return nil, connect.NewError(connect.CodeInternal, auth.ErrUnavailable)
				}
				return connect.NewResponse(wrapperspb.String(i.Subject())), nil
			}, opts...)
		}}, nil
	}), fx.Populate(&server))
	if e = a.Err(); e != nil {
		t.Fatal(e)
	}
	if server.Address() != "" || p.calls.Load() != 0 {
		t.Fatal("I/O during DI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e = a.Start(ctx); e != nil {
		t.Fatal(e)
	}
	defer func() {
		c, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if e := a.Stop(c); e != nil {
			t.Error(e)
		}
	}()
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{"iss": cfg.Issuer, "sub": "demo-user", "aud": cfg.Audience, "exp": time.Now().Add(time.Minute).Unix()})
	tok.Header["typ"] = cfg.TokenType
	tok.Header["kid"] = "integration-key"
	raw, e := tok.SignedString(priv)
	if e != nil {
		t.Fatal(e)
	}
	httpClient := &http.Client{Timeout: 3 * time.Second}
	defer httpClient.CloseIdleConnections()
	client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](httpClient, "http://"+server.Address()+procedure)
	call := func(token string) (*connect.Response[wrapperspb.StringValue], error) {
		req := connect.NewRequest(wrapperspb.String("read"))
		if token != "" {
			req.Header().Set("Authorization", "Bearer "+token)
		}
		return client.CallUnary(ctx, req)
	}
	if _, e = call(""); connect.CodeOf(e) != connect.CodeUnauthenticated {
		t.Fatal(e)
	}
	response, e := call(raw)
	if e != nil || response.Msg.Value != "demo-user" || p.calls.Load() != 1 {
		t.Fatal("authorized call failed", e)
	}
	p.revoked.Store(true)
	if _, e = call(raw); connect.CodeOf(e) != connect.CodeUnauthenticated || p.calls.Load() != 2 {
		t.Fatal("revocation not enforced", e)
	}
}
