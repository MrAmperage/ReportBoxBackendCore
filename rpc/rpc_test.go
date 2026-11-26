package rpc_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	core "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	"github.com/MrAmperage/ReportBoxBackendCore/rpc"
	"go.uber.org/fx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const prefix = "/reportbox.test.v1.TestService/"
const unary = prefix + "Echo"
const stream = prefix + "Stream"

func registration(auth rpc.AuthorizeFunc) rpc.Handler {
	return rpc.Handler{Authorize: auth, AllowUnauthenticated: auth == nil, Build: func(opts ...connect.HandlerOption) (string, http.Handler) {
		mux := http.NewServeMux()
		mux.Handle(unary, connect.NewUnaryHandler(unary, func(ctx context.Context, r *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
			switch r.Msg.Value {
			case "error":
				return nil, errors.New("secret-driver-message")
			case "panic":
				panic("secret-panic")
			case "denied":
				return nil, connect.NewError(connect.CodePermissionDenied, errors.New("secret-permission"))
			}
			if rpc.RequestID(ctx) == "" {
				return nil, errors.New("request ID missing")
			}
			return connect.NewResponse(wrapperspb.String(r.Msg.Value)), nil
		}, opts...))
		mux.Handle(stream, connect.NewServerStreamHandler(stream, func(ctx context.Context, r *connect.Request[wrapperspb.StringValue], s *connect.ServerStream[wrapperspb.StringValue]) error {
			return s.Send(wrapperspb.String(r.Msg.Value))
		}, opts...))
		return prefix, mux
	}}
}

func start(t *testing.T, auth rpc.AuthorizeFunc) (*rpc.Server, *rpc.Client, func()) {
	t.Helper()
	var s *rpc.Server
	var c *rpc.Client
	cfg := rpc.DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	cfg.MaxMessageBytes = 256
	a := core.NewWithConfig(app.Info{Name: "rpc-test"}, config.Defaults(), rpc.ServerModule(cfg), rpc.ClientModule(rpc.DefaultClientConfig()), rpc.ProvideHandler(func() rpc.Handler { return registration(auth) }), fx.Populate(&s, &c))
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	if s.Address() != "" {
		t.Fatal("opened before startup")
	}
	if _, err := c.Target("http://127.0.0.1:1", rpc.Connect); err == nil {
		t.Fatal("client active before startup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.Stop(ctx); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(stop)
	return s, c, stop
}

func TestProtocolsAndSanitizedErrors(t *testing.T) {
	s, c, stop := start(t, nil)
	for _, p := range []rpc.Protocol{rpc.Connect, rpc.GRPC, rpc.GRPCWeb} {
		target, err := c.Target("http://"+s.Address(), p)
		if err != nil {
			t.Fatal(err)
		}
		client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](target.HTTPClient, target.BaseURL+unary, target.Options...)
		response, err := client.CallUnary(context.Background(), connect.NewRequest(wrapperspb.String("hello")))
		if err != nil || response.Msg.Value != "hello" {
			t.Fatalf("protocol %d: %v %v", p, response, err)
		}
		for _, tc := range []struct {
			input string
			code  connect.Code
		}{{"error", connect.CodeInternal}, {"panic", connect.CodeInternal}, {"denied", connect.CodePermissionDenied}} {
			_, err = client.CallUnary(context.Background(), connect.NewRequest(wrapperspb.String(tc.input)))
			if connect.CodeOf(err) != tc.code || strings.Contains(err.Error(), "secret-") {
				t.Fatalf("unsafe error: %v", err)
			}
		}
	}
	stop()
	if s.Address() != "" {
		t.Fatal("address not cleared")
	}
	if _, err := c.Target("http://localhost:9090", rpc.GRPC); err == nil {
		t.Fatal("client still active")
	}
}
func TestAuthorizationAndStream(t *testing.T) {
	s, c, _ := start(t, func(ctx context.Context, _ string, h http.Header) (context.Context, error) {
		if h.Get("Authorization") != "Bearer test" {
			return ctx, connect.NewError(connect.CodeUnauthenticated, errors.New("missing credential"))
		}
		return ctx, nil
	})
	target, err := c.Target("http://"+s.Address(), rpc.Connect)
	if err != nil {
		t.Fatal(err)
	}
	client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](target.HTTPClient, target.BaseURL+unary, target.Options...)
	_, err = client.CallUnary(context.Background(), connect.NewRequest(wrapperspb.String("hello")))
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("want unauthenticated: %v", err)
	}
	request := connect.NewRequest(wrapperspb.String("hello"))
	request.Header().Set("Authorization", "Bearer test")
	if _, err = client.CallUnary(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	sc := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](target.HTTPClient, target.BaseURL+stream, target.Options...)
	ss, err := sc.CallServerStream(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	if !ss.Receive() || ss.Msg().Value != "hello" {
		t.Fatalf("stream: %v", ss.Err())
	}
	if ss.Receive() || ss.Err() != nil {
		t.Fatalf("stream termination: %v", ss.Err())
	}
	request = connect.NewRequest(wrapperspb.String(strings.Repeat("x", 1024)))
	request.Header().Set("Authorization", "Bearer test")
	_, err = client.CallUnary(context.Background(), request)
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("message limit: %v", err)
	}
	if _, err = c.Target("http://example.com", rpc.GRPC); err == nil {
		t.Fatal("non-loopback plaintext allowed")
	}
	if _, err = c.Target("http://user:secret@localhost", rpc.GRPC); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("bad endpoint validation")
	}
}
func TestBindFailureAndDuplicate(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	c := rpc.DefaultServerConfig()
	c.Address = l.Addr().String()
	provider := rpc.ProvideHandler(func() rpc.Handler { return registration(nil) })
	a := core.NewWithConfig(app.Info{Name: "bind"}, config.Defaults(), rpc.ServerModule(c), provider)
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err == nil {
		t.Fatal("bind failure not reported")
	}
	a = core.NewWithConfig(app.Info{Name: "duplicate"}, config.Defaults(), rpc.ServerModule(c), provider, rpc.ProvideHandler(func() rpc.Handler { return registration(nil) }))
	if a.Err() == nil {
		t.Fatal("duplicate route accepted")
	}
	a = core.NewWithConfig(app.Info{Name: "no-policy"}, config.Defaults(), rpc.ServerModule(c), rpc.ProvideHandler(func() rpc.Handler { h := registration(nil); h.AllowUnauthenticated = false; return h }))
	if a.Err() == nil {
		t.Fatal("missing auth policy accepted")
	}
}

func TestClientOnly(t *testing.T) {
	var c *rpc.Client
	a := core.NewWithConfig(app.Info{Name: "client-only"}, config.Defaults(), rpc.ClientModule(rpc.DefaultClientConfig()), fx.Populate(&c))
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := a.Stop(ctx); err != nil {
			t.Error(err)
		}
	}()
	target, err := c.Target("http://localhost:9090", rpc.GRPC)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.HTTPClient.Do(req); err == nil || !strings.Contains(err.Error(), "origin mismatch") {
		t.Fatalf("origin protection failed: %v", err)
	}
}

func TestStreamingDrain(t *testing.T) {
	var s *rpc.Server
	var c *rpc.Client
	cfg := rpc.DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	started := make(chan struct{})
	ended := make(chan struct{})
	a := core.NewWithConfig(app.Info{Name: "rpc-drain"}, config.Defaults(), rpc.ServerModule(cfg), rpc.ClientModule(rpc.DefaultClientConfig()), rpc.ProvideHandler(func(signal *rpc.ShutdownSignal) rpc.Handler {
		return rpc.Handler{AllowUnauthenticated: true, Build: func(opts ...connect.HandlerOption) (string, http.Handler) {
			mux := http.NewServeMux()
			mux.Handle(stream, connect.NewServerStreamHandler(stream, func(ctx context.Context, req *connect.Request[wrapperspb.StringValue], ss *connect.ServerStream[wrapperspb.StringValue]) error {
				defer close(ended)
				if err := ss.Send(wrapperspb.String("start")); err != nil {
					return err
				}
				close(started)
				select {
				case <-signal.Done():
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}, opts...))
			return prefix, mux
		}}
	}), fx.Populate(&s, &c))
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	startCtx, startCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer startCancel()
	if err := a.Start(startCtx); err != nil {
		t.Fatal(err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = a.Stop(ctx)
		}
	})
	target, err := c.Target("http://"+s.Address(), rpc.GRPC)
	if err != nil {
		t.Fatal(err)
	}
	client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](target.HTTPClient, target.BaseURL+stream, target.Options...)
	callCtx, callCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer callCancel()
	ss, err := client.CallServerStream(callCtx, connect.NewRequest(wrapperspb.String("hello")))
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	if !ss.Receive() || ss.Msg().Value != "start" {
		t.Fatalf("stream start: %v", ss.Err())
	}
	select {
	case <-started:
	case <-callCtx.Done():
		t.Fatal("stream not started")
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := a.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	stopped = true
	select {
	case <-ended:
	default:
		t.Fatal("stream was not drained")
	}
	if ss.Receive() || ss.Err() != nil {
		t.Fatalf("stream did not finish gracefully: %v", ss.Err())
	}
}

// A listener may be bound and serving before other participants have finished
// activation, but it must not admit business requests until Core is Running.
func TestAdmissionDuringStartup(t *testing.T) {
	var s *rpc.Server
	var c *rpc.Client
	cfg := rpc.DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	a := core.NewWithConfig(app.Info{Name: "startup-admission"}, config.Defaults(), rpc.ServerModule(cfg), rpc.ClientModule(rpc.DefaultClientConfig()), rpc.ProvideHandler(func() rpc.Handler { return registration(nil) }), fx.Populate(&s, &c), fx.Provide(fx.Annotate(func(server *rpc.Server) lifecycle.Participant {
		return lifecycle.Participant{Name: "test.startup", DependsOn: []string{"rpc.server"}, Activate: func(ctx context.Context) error {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+server.Address()+unary, strings.NewReader(`"hello"`))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusServiceUnavailable {
				return errors.New("RPC admitted before startup completed")
			}
			return nil
		}}
	}, fx.ResultTags(`group:"backendcore.lifecycle"`))))
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := a.Stop(stop); err != nil {
			t.Error(err)
		}
	}()
	target, err := c.Target("http://"+s.Address(), rpc.Connect)
	if err != nil {
		t.Fatal(err)
	}
	client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](target.HTTPClient, target.BaseURL+unary, target.Options...)
	response, err := client.CallUnary(ctx, connect.NewRequest(wrapperspb.String("hello")))
	if err != nil || response.Msg.Value != "hello" {
		t.Fatalf("RPC not admitted after startup: %v %v", response, err)
	}
}
