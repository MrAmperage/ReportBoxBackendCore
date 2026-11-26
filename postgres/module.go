package postgres

import (
	"errors"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	"go.uber.org/fx"
	"log/slog"
	"strings"
)

// Module installs PostgreSQL. Without explicit config, reads ENV.
func Module(settings ...Config) fx.Option {
	if len(settings) > 1 {
		return fx.Error(errors.New("PostgreSQL accepts one configuration"))
	}
	load := LoadConfig
	if len(settings) == 1 {
		c := settings[0]
		load = func() (Config, error) { return c, nil }
	}
	return fx.Module("backendcore.postgres", fx.Provide(load, func(c Config, core config.Config, info app.Info, log *slog.Logger, reg *health.Registry) (*Pool, error) {
		return newPool(c, strings.EqualFold(core.Environment, "production"), info, log, reg)
	}, fx.Annotate(func(p *Pool) lifecycle.Participant { return p.participant() }, fx.ResultTags(`group:"backendcore.lifecycle"`))))
}
