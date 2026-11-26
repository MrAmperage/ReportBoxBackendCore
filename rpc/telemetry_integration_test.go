package rpc_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	core "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/management"
	"github.com/MrAmperage/ReportBoxBackendCore/rpc"
	"github.com/MrAmperage/ReportBoxBackendCore/telemetry"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestRPCWithTelemetry(t *testing.T) {
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); w.WriteHeader(200) }))
	defer collector.Close()
	tc := telemetry.DefaultConfig()
	tc.Endpoint = collector.URL + "/v1/traces"
	tc.AllowPlaintext = true
	tc.SampleRatio = 1
	sc := rpc.DefaultServerConfig()
	sc.Address = "127.0.0.1:0"
	mc := management.DefaultConfig()
	mc.Address = "127.0.0.1:0"
	var s *rpc.Server
	var c *rpc.Client
	var m *management.Server
	a := core.New(app.Info{Name: "rpc-telemetry-test"}, rpc.ServerModule(sc), rpc.ClientModule(rpc.DefaultClientConfig()), rpc.ProvideHandler(func() rpc.Handler {
		return registration(func(ctx context.Context, _ string, h http.Header) (context.Context, error) {
			if !trace.SpanContextFromContext(ctx).IsValid() || h.Get("traceparent") == "" {
				t.Error("missing RPC trace context")
			}
			return ctx, nil
		})
	}), management.Module(mc), telemetry.Module(tc), fx.Populate(&s, &c, &m))
	if e := a.Err(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if e := a.Start(ctx); e != nil {
		t.Fatal(e)
	}
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := a.Stop(stop); e != nil {
			t.Error(e)
		}
	}()
	target, e := c.Target("http://"+s.Address(), rpc.Connect)
	if e != nil {
		t.Fatal(e)
	}
	client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](target.HTTPClient, target.BaseURL+unary, target.Options...)
	if _, e = client.CallUnary(ctx, connect.NewRequest(wrapperspb.String("hello"))); e != nil {
		t.Fatal(e)
	}
	streamClient := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](target.HTTPClient, target.BaseURL+stream, target.Options...)
	st, e := streamClient.CallServerStream(ctx, connect.NewRequest(wrapperspb.String("hello")))
	if e != nil {
		t.Fatal(e)
	}
	for st.Receive() {
	}
	if e = st.Err(); e != nil {
		t.Fatal(e)
	}
	if e = st.Close(); e != nil {
		t.Fatal(e)
	}
	httpClient := &http.Client{Timeout: time.Second}
	defer httpClient.CloseIdleConnections()
	res, e := httpClient.Get("http://" + m.Address() + "/metrics")
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(res.Body)
	res.Body.Close()
	if e != nil || res.StatusCode != 200 {
		t.Fatal("metrics unavailable")
	}
	for _, expected := range []string{`reportbox_rpc_requests_total{code="ok",direction="client",procedure="` + unary + `"} 1`, `reportbox_rpc_requests_total{code="ok",direction="server",procedure="` + unary + `"} 1`, `reportbox_rpc_requests_total{code="ok",direction="client",procedure="` + stream + `"} 1`} {
		if !strings.Contains(string(b), expected) {
			t.Errorf("missing metric %s", expected)
		}
	}
}
