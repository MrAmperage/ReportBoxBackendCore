package static

import (
	"errors"
	"log/slog"

	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	"go.uber.org/fx"
)

// Module installs the static listener. With no argument it reads STATIC_*;
// one complete Config overrides the environment. No other modules are required.
func Module(settings ...Config) fx.Option {
	if len(settings) > 1 {
		return fx.Error(errors.New("static accepts one configuration"))
	}
	load := LoadConfig
	if len(settings) == 1 {
		c := settings[0]
		load = func() (Config, error) { return c, nil }
	}
	return fx.Module("backendcore.static", fx.Provide(load, func(c Config, r *health.Registry, l *slog.Logger, report lifecycle.ReportFailure) (*Server, error) {
		return newServer(c, r, l, report)
	}, fx.Annotate(func(s *Server) lifecycle.Participant { return s.participant() }, fx.ResultTags(`group:"backendcore.lifecycle"`))))
}
