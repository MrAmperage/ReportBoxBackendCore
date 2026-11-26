package telemetry

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
	"github.com/MrAmperage/ReportBoxBackendCore/internal/observe"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	"github.com/MrAmperage/ReportBoxBackendCore/management"
	"github.com/MrAmperage/ReportBoxBackendCore/nats"
	"github.com/MrAmperage/ReportBoxBackendCore/postgres"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
)

// ParticipantName identifies Telemetry in lifecycle dependencies.
const ParticipantName = observe.ParticipantName

// Runtime owns private telemetry state. Use native APIs, never global providers.
type Runtime struct {
	cfg            Config
	info           app.Info
	core           config.Config
	log            *slog.Logger
	registry       *prometheus.Registry
	instruments    *observe.Instruments
	provider       *sdktrace.TracerProvider
	exporterClient *otlpClient
}

// TracerProvider returns the current provider, or a no-op before Open and after Close.
func (r *Runtime) TracerProvider() trace.TracerProvider { return r.instruments.Provider() }

// Registerer provides the per-app Prometheus registry for application collectors.
func (r *Runtime) Registerer() prometheus.Registerer { return r.registry }

// Propagator carries only W3C trace context. Use it for native publishers.
func (r *Runtime) Propagator() propagation.TextMapPropagator { return propagation.TraceContext{} }

type runtimeParams struct {
	fx.In
	Config      Config
	Info        app.Info
	Core        config.Config
	Log         *slog.Logger
	Registry    *prometheus.Registry
	Instruments *observe.Instruments
	NATS        *nats.Connection `optional:"true"`
	Postgres    *postgres.Pool   `optional:"true"`
}

func newRegistry() *prometheus.Registry {
	r := prometheus.NewRegistry()
	r.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	return r
}
func newRuntime(p runtimeParams) (*Runtime, error) {
	if err := p.Config.Validate(); err != nil {
		return nil, err
	}
	r := &Runtime{cfg: p.Config, info: p.Info, core: p.Core, log: p.Log, registry: p.Registry, instruments: p.Instruments}
	if err := registerSources(p.Registry, p.NATS, p.Postgres); err != nil {
		return nil, err
	}
	return r, nil
}
func (r *Runtime) participant() lifecycle.Participant {
	return lifecycle.Participant{Name: ParticipantName, Open: r.open, Close: r.close}
}
func (r *Runtime) open(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.cfg.Endpoint == "" {
		return nil
	}
	client := newOTLPClient(r.cfg)
	exp, err := otlptrace.New(ctx, client)
	if err != nil {
		_ = client.Stop(ctx)
		return errors.New("OTLP exporter initialization failed")
	}
	if err := ctx.Err(); err != nil {
		_ = exp.Shutdown(ctx)
		return err
	}
	res := resource.NewSchemaless(attribute.String("service.name", r.info.Name), attribute.String("service.version", r.info.Version), attribute.String("deployment.environment.name", r.core.Environment))
	r.provider = sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(r.cfg.SampleRatio))), sdktrace.WithBatcher(safeExporter{SpanExporter: exp, log: r.log}, sdktrace.WithMaxQueueSize(2048), sdktrace.WithMaxExportBatchSize(256), sdktrace.WithBatchTimeout(time.Second), sdktrace.WithExportTimeout(r.cfg.ExportTimeout)))
	r.exporterClient = client
	r.instruments.SetProvider(r.provider)
	return nil
}
func (r *Runtime) close(ctx context.Context) error {
	r.instruments.SetProvider(nil)
	if r.provider == nil {
		return nil
	}
	var err error
	if r.provider.Shutdown(ctx) != nil {
		err = errors.New("telemetry shutdown failed")
	}
	if r.exporterClient != nil && r.exporterClient.Stop(ctx) != nil {
		err = errors.Join(err, errors.New("OTLP client stop incomplete"))
	}
	return err
}

// Only sanitized diagnostics leave the exporter boundary.
type safeExporter struct {
	sdktrace.SpanExporter
	log *slog.Logger
}

func (e safeExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if err := e.SpanExporter.ExportSpans(ctx, spans); err != nil {
		e.log.Warn("OTLP trace export failed")
		return errors.New("OTLP trace export failed")
	}
	return nil
}
func (e safeExporter) Shutdown(ctx context.Context) error {
	if err := e.SpanExporter.Shutdown(ctx); err != nil {
		return errors.New("OTLP exporter shutdown failed")
	}
	return nil
}

// Module enables metrics and optional trace export. Management is not required.
func Module(settings ...Config) fx.Option {
	if len(settings) > 1 {
		return fx.Error(errors.New("telemetry accepts one configuration"))
	}
	load := LoadConfig
	if len(settings) == 1 {
		c := settings[0]
		load = func() (Config, error) { return c, nil }
	}
	return fx.Module("backendcore.telemetry", fx.Provide(load, newRegistry, observe.New, newRuntime, func(r *Runtime) management.MetricsHandler {
		return management.MetricsHandler{Handler: promhttp.HandlerFor(safeGatherer{r.registry}, promhttp.HandlerOpts{ErrorLog: log.New(io.Discard, "", 0), MaxRequestsInFlight: 2, Timeout: 0})}
	}, fx.Annotate(func(r *Runtime) lifecycle.Participant { return r.participant() }, fx.ResultTags(`group:"backendcore.lifecycle"`))))
}

// Custom collectors can return arbitrary errors. Never expose their text over HTTP.
type safeGatherer struct{ prometheus.Gatherer }

func (g safeGatherer) Gather() ([]*dto.MetricFamily, error) {
	families, err := g.Gatherer.Gather()
	if err != nil {
		return nil, errors.New("metrics collection failed")
	}
	return families, nil
}
