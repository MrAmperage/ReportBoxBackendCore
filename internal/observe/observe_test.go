package observe

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestTracePropagationAndBoundedMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	o := New(reg)
	rec := tracetest.NewSpanRecorder()
	p := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer p.Shutdown(context.Background())
	o.SetProvider(p)
	ctx, finish := o.Start(context.Background(), "rpc.client", "/demo.Service/Method", trace.SpanKindClient)
	h := make(http.Header)
	o.Inject(ctx, h)
	if h.Get("traceparent") == "" || h.Get("baggage") != "" {
		t.Fatal("propagation")
	}
	incoming := http.Header{"traceparent": h.Values("traceparent"), "baggage": {"secret=private"}}
	child, end := o.Start(o.Extract(context.Background(), incoming), "rpc.server", "/demo.Service/Method", trace.SpanKindServer)
	if trace.SpanContextFromContext(child).TraceID() != trace.SpanContextFromContext(ctx).TraceID() {
		t.Fatal("trace continuity")
	}
	end("permission_denied")
	end("ok")
	finish("ok")
	finish("error")
	o.Record("private-subject", "secret", time.Second)
	o.Record("nats.receive", "private", time.Second)
	spans := rec.Ended()
	if len(spans) != 2 || spans[0].Parent().SpanID() != spans[1].SpanContext().SpanID() {
		t.Fatal("span relationship or duplicate end")
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var calls, operations float64
	for _, f := range families {
		switch f.GetName() {
		case "reportbox_rpc_requests_total":
			for _, m := range f.Metric {
				calls += m.GetCounter().GetValue()
			}
		case "reportbox_operations_total":
			for _, m := range f.Metric {
				operations += m.GetCounter().GetValue()
				for _, l := range m.Label {
					if l.GetValue() == "private" || l.GetValue() == "private-subject" {
						t.Fatal("unbounded label")
					}
				}
			}
		}
	}
	if calls != 2 || operations != 1 {
		t.Fatalf("calls=%v ops=%v", calls, operations)
	}
}
func TestNilObserver(t *testing.T) {
	var o *Instruments
	ctx := context.Background()
	got, end := o.Start(ctx, "rpc.server", "test", trace.SpanKindServer)
	end("ok")
	if got != ctx || len(o.Dependencies()) != 0 {
		t.Fatal("nil observer changed behavior")
	}
}

func TestInjectReplacesStaleContext(t *testing.T) {
	o := New(prometheus.NewRegistry())
	h := http.Header{"traceparent": {"stale"}, "Traceparent": {"stale"}, "tracestate": {"private"}, "Authorization": {"preserved"}}
	o.Inject(context.Background(), h)
	for k := range h {
		if strings.EqualFold(k, "traceparent") || strings.EqualFold(k, "tracestate") {
			t.Fatal("stale context")
		}
	}
	if h.Get("Authorization") != "preserved" {
		t.Fatal("unrelated header removed")
	}
}
