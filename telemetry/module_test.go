package telemetry_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/management"
	"github.com/MrAmperage/ReportBoxBackendCore/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"go.uber.org/fx"
	"google.golang.org/protobuf/proto"
)

func TestModuleMetricsAndExport(t *testing.T) {
	var exported atomic.Int32
	collectorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, e := io.ReadAll(r.Body)
		var q collector.ExportTraceServiceRequest
		if e != nil || proto.Unmarshal(b, &q) != nil {
			t.Error("invalid collector request")
			w.WriteHeader(400)
			return
		}
		for _, rs := range q.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				for _, s := range ss.Spans {
					if s.Name == "test-span" {
						exported.Add(1)
					}
				}
			}
		}
		w.WriteHeader(200)
	}))
	defer collectorServer.Close()
	cfg := telemetry.DefaultConfig()
	cfg.Endpoint = collectorServer.URL + "/v1/traces"
	cfg.AllowPlaintext = true
	cfg.SampleRatio = 1
	mc := management.DefaultConfig()
	mc.Address = "127.0.0.1:0"
	var rt *telemetry.Runtime
	var server *management.Server
	before := otel.GetTracerProvider()
	globalProm := prometheus.DefaultRegisterer
	a := core.New(app.Info{Name: "telemetry-test"}, telemetry.Module(cfg), management.Module(mc), fx.Populate(&rt, &server))
	if e := a.Err(); e != nil {
		t.Fatal(e)
	}
	_, early := rt.TracerProvider().Tracer("test").Start(context.Background(), "early")
	if early.IsRecording() {
		t.Fatal("provider started during graph construction")
	}
	early.End()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "reportbox_demo_test", Help: "Test custom metric."})
	if e := rt.Registerer().Register(gauge); e != nil {
		t.Fatal(e)
	}
	gauge.Set(3)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if e := a.Start(ctx); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = a.Stop(stop)
	})
	_, span := rt.TracerProvider().Tracer("test").Start(ctx, "test-span")
	if !span.IsRecording() {
		t.Fatal("provider not started")
	}
	span.End()
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	resp, e := client.Get("http://" + server.Address() + "/metrics")
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || resp.StatusCode != 200 || !strings.Contains(string(b), "reportbox_demo_test 3") || !strings.Contains(string(b), "go_goroutines") {
		t.Fatal("metrics missing")
	}
	stop, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	if e = a.Stop(stop); e != nil {
		t.Fatal(e)
	}
	if exported.Load() != 1 {
		t.Fatalf("expected shutdown flush, got %d", exported.Load())
	}
	if otel.GetTracerProvider() != before || prometheus.DefaultRegisterer != globalProm {
		t.Fatal("global state mutated")
	}
	_, late := rt.TracerProvider().Tracer("test").Start(context.Background(), "late")
	if late.IsRecording() {
		t.Fatal("provider still recording after close")
	}
	late.End()
}
func TestMetricsOnlyWithoutManagement(t *testing.T) {
	a := core.New(app.Info{Name: "metrics-only"}, telemetry.Module(telemetry.DefaultConfig()))
	if e := a.Err(); e != nil {
		t.Fatal(e)
	}
	ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	if e := a.Start(ctx); e != nil {
		t.Fatal(e)
	}
	if e := a.Stop(ctx); e != nil {
		t.Fatal(e)
	}
}
