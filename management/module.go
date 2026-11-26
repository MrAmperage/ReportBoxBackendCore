package management

import (
	"errors"
	"log/slog"

	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	"go.uber.org/fx"
)

type serverParams struct {
	fx.In
	Config   Config
	Registry *health.Registry
	Logger   *slog.Logger
	Report   lifecycle.ReportFailure
	Metrics  MetricsHandler `optional:"true"`
}

// Module installs the private HTTP listener. An optional MetricsHandler can
// be provided by a metrics module. The default module reads environment settings.
func Module(settings ...Config) fx.Option {
	if len(settings) > 1 {
		return fx.Error(errors.New("management accepts one configuration"))
	}
	load := LoadConfig
	if len(settings) == 1 {
		c := settings[0]
		load = func() (Config, error) { return c, nil }
	}
	return fx.Module("backendcore.management", fx.Provide(load, func(p serverParams) (*Server, error) {
		return newServer(p.Config, p.Registry, p.Logger, p.Report, p.Metrics)
	}, fx.Annotate(func(s *Server) lifecycle.Participant { return s.participant() }, fx.ResultTags(`group:"backendcore.lifecycle"`))))
}
