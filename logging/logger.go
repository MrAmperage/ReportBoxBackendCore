// Package logging supplies structured, per-application loggers.
package logging

import (
	"context"
	"io"
	"log/slog"

	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
	"go.opentelemetry.io/otel/trace"
)

// New builds an independent logger with validated core metadata and settings.
func New(w io.Writer, info app.Info, cfg config.Config) (*slog.Logger, error) {
	info, err := info.Normalize()
	if err != nil {
		return nil, err
	}
	cfg, err = cfg.Normalize()
	if err != nil {
		return nil, err
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{Level: level, AddSource: cfg.LogAddSource}
	var h slog.Handler
	if cfg.LogFormat == "text" {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(traceHandler{h}).With("service", info.Name, "version", info.Version, "commit", info.Commit, "environment", cfg.Environment), nil
}

// traceHandler correlates logs without capturing headers, payloads or baggage.
type traceHandler struct{ slog.Handler }

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	sc := trace.SpanContextFromContext(ctx)
	if sc.IsValid() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return h.Handler.Handle(ctx, r)
}
func (h traceHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return traceHandler{h.Handler.WithAttrs(a)}
}
func (h traceHandler) WithGroup(n string) slog.Handler { return traceHandler{h.Handler.WithGroup(n)} }
