package nats

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	"go.uber.org/fx"
)

// Module installs the optional managed Core NATS connection.
func Module(settings ...Config) fx.Option {
	if len(settings) > 1 {
		return fx.Error(errors.New("NATS accepts one configuration"))
	}
	load := LoadConfig
	if len(settings) == 1 {
		c := settings[0]
		load = func() (Config, error) { return c, nil }
	}
	return fx.Module("backendcore.nats", fx.Provide(newSubscriptions, fx.Annotate(func(s *subscriptions) lifecycle.Participant { return s.participant() }, fx.ResultTags(`group:"backendcore.lifecycle"`)), load, func(c Config, core config.Config, info app.Info, l *slog.Logger, h *health.Registry, r lifecycle.ReportFailure) (*Connection, error) {
		return newConnection(c, strings.EqualFold(core.Environment, "production"), info, l, h, r)
	}, fx.Annotate(func(c *Connection) lifecycle.Participant { return c.participant() }, fx.ResultTags(`group:"backendcore.lifecycle"`))))
}
