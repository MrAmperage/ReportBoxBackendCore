package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
	"go.opentelemetry.io/otel/trace"
)

func TestLogger(t *testing.T) {
	var b bytes.Buffer
	before := slog.Default()
	l, err := New(&b, app.Info{Name: "example"}, config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	l.Debug("hidden")
	l.Info("hello")
	var m map[string]any
	if err = json.Unmarshal(b.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["service"] != "example" || m["version"] != "dev" || m["commit"] != "unknown" || m["msg"] != "hello" || slog.Default() != before {
		t.Fatalf("%v", m)
	}
}

func TestTraceCorrelation(t *testing.T) {
	var b bytes.Buffer
	logger, err := New(&b, app.Info{Name: "example"}, config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	tid, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	sid, _ := trace.SpanIDFromHex("0123456789abcdef")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid}))
	logger.InfoContext(ctx, "correlated")
	var v map[string]any
	if e := json.Unmarshal(b.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	if v["trace_id"] != tid.String() || v["span_id"] != sid.String() {
		t.Fatal("trace correlation missing")
	}
}
