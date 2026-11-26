// Package observe holds passive instrumentation shared by optional modules.
package observe

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const ParticipantName = "telemetry.runtime"

type providerBox struct{ provider trace.TracerProvider }

// Instruments contains bounded-label metrics and an atomically installed tracer.
// Nil is a no-op. No SDK or exporter is created here.
type Instruments struct {
	provider     atomic.Pointer[providerBox]
	counts       *prometheus.CounterVec
	durations    *prometheus.HistogramVec
	rpcCounts    *prometheus.CounterVec
	rpcDurations *prometheus.HistogramVec
}

func New(reg *prometheus.Registry) *Instruments {
	i := &Instruments{
		counts:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "reportbox_operations_total", Help: "Completed managed operations."}, []string{"operation", "outcome"}),
		durations: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "reportbox_operation_duration_seconds", Help: "Managed operation duration in seconds."}, []string{"operation"}),
	}
	i.rpcCounts = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "reportbox_rpc_requests_total", Help: "Completed generated RPCs by direction, procedure and code."}, []string{"direction", "procedure", "code"})
	i.rpcDurations = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "reportbox_rpc_request_duration_seconds", Help: "Generated RPC duration."}, []string{"direction", "procedure"})
	reg.MustRegister(i.counts, i.durations, i.rpcCounts, i.rpcDurations)
	return i
}
func (i *Instruments) SetProvider(p trace.TracerProvider) {
	if p == nil {
		i.provider.Store(nil)
		return
	}
	i.provider.Store(&providerBox{p})
}
func (i *Instruments) Provider() trace.TracerProvider {
	if i != nil {
		if p := i.provider.Load(); p != nil {
			return p.provider
		}
	}
	return noop.NewTracerProvider()
}
func (i *Instruments) Dependencies(existing ...string) []string {
	if i != nil {
		return append(existing, ParticipantName)
	}
	return existing
}

// Extract and Inject carry only W3C trace context, not baggage or credentials.
func (i *Instruments) Extract(ctx context.Context, h http.Header) context.Context {
	if i == nil {
		return ctx
	}
	// Native NATS headers are not necessarily canonical HTTP keys. Copy only trace context.
	header := make(http.Header, 2)
	for k, values := range h {
		if strings.EqualFold(k, "traceparent") || strings.EqualFold(k, "tracestate") {
			for _, v := range values {
				header.Add(k, v)
			}
		}
	}
	if len(header.Values("traceparent")) != 1 {
		return ctx
	}
	return propagation.TraceContext{}.Extract(ctx, propagation.HeaderCarrier(header))
}
func (i *Instruments) Inject(ctx context.Context, h http.Header) {
	if i != nil && h != nil {
		propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(h))
	}
}

// Start returns a finish callback that may safely be called more than once.
// operation must be a static, low-cardinality identifier; name may be a generated RPC procedure.
func (i *Instruments) Start(ctx context.Context, operation, name string, kind trace.SpanKind) (context.Context, func(string)) {
	if i == nil {
		return ctx, func(string) {}
	}
	start := time.Now()
	ctx, span := i.Provider().Tracer("reportbox.backendcore").Start(ctx, name, trace.WithSpanKind(kind))
	var once sync.Once
	return ctx, func(outcome string) {
		once.Do(func() {
			if operation == "rpc.server" || operation == "rpc.client" {
				direction := strings.TrimPrefix(operation, "rpc.")
				outcome = rpcCode(outcome)
				i.rpcCounts.WithLabelValues(direction, name, outcome).Inc()
				i.rpcDurations.WithLabelValues(direction, name).Observe(time.Since(start).Seconds())
			} else {
				outcome = operationOutcome(outcome)
				i.Record(operation, outcome, time.Since(start))
			}
			if outcome != "ok" {
				span.SetStatus(codes.Error, outcome)
			}
			span.End()
		})
	}
}
func (i *Instruments) Record(operation, outcome string, d time.Duration) {
	if i == nil {
		return
	}
	// Only static call sites can select these values. Do not accept arbitrary labels.
	switch operation {
	case "nats.receive", "jetstream.process", "jetstream.ack", "jetstream.nak", "gorm.query":
	default:
		return
	}
	outcome = operationOutcome(outcome)
	i.counts.WithLabelValues(operation, outcome).Inc()
	i.durations.WithLabelValues(operation).Observe(d.Seconds())
}

func rpcCode(code string) string {
	switch code {
	case "ok", "canceled", "unknown", "invalid_argument", "deadline_exceeded", "not_found", "already_exists", "permission_denied", "resource_exhausted", "failed_precondition", "aborted", "out_of_range", "unimplemented", "internal", "unavailable", "data_loss", "unauthenticated":
		return code
	default:
		return "unknown"
	}
}

func operationOutcome(outcome string) string {
	switch outcome {
	case "ok", "error", "canceled", "retry", "not_found":
		return outcome
	default:
		return "error"
	}
}
