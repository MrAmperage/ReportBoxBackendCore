// Package backendcore composes reusable backend service infrastructure.
package backendcore

import (
	"log/slog"
	"os"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/internal/coordinator"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	"github.com/MrAmperage/ReportBoxBackendCore/logging"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

// New resolves environment settings and creates an application, without starting it.
func New(info app.Info, modules ...fx.Option) *fx.App {
	return NewWithOverrides(info, config.Overrides{}, modules...)
}

// NewWithOverrides resolves explicitly supplied settings before constructing Fx.
func NewWithOverrides(info app.Info, overrides config.Overrides, modules ...fx.Option) *fx.App {
	cfg, err := config.Load(overrides)
	if err != nil {
		return fx.New(fx.Error(err))
	}
	return NewWithConfig(info, cfg, modules...)
}

// NewWithConfig uses a fully resolved configuration, ignoring environment settings.
func NewWithConfig(info app.Info, cfg config.Config, modules ...fx.Option) *fx.App {
	info, err := info.Normalize()
	if err != nil {
		return fx.New(fx.Error(err))
	}
	cfg, err = cfg.Normalize()
	if err != nil {
		return fx.New(fx.Error(err))
	}
	logger, err := logging.New(os.Stdout, info, cfg)
	if err != nil {
		return fx.New(fx.Error(err))
	}
	state := new(coordinator.State)
	registry := health.New(state.Phase, 2*time.Second)
	options := []fx.Option{
		fx.StartTimeout(cfg.StartTimeout), fx.StopTimeout(cfg.StopTimeout),
		fx.Supply(info, cfg, logger, registry),
		fx.WithLogger(func() fxevent.Logger {
			l := &fxevent.SlogLogger{Logger: logger.With("component", "fx")}
			l.UseLogLevel(slog.LevelDebug)
			l.UseErrorLevel(slog.LevelError)
			return l
		}),
		fx.Provide(func(shutdown fx.Shutdowner) lifecycle.ReportFailure {
			return func(err error) error {
				state.Fail()
				logger.Error("fatal runtime failure", "error", err)
				return shutdown.Shutdown(fx.ExitCode(1))
			}
		}),
	}
	options = append(options, modules...)
	options = append(options, fx.Invoke(func(p structParams) error {
		c, err := coordinator.New(state, logger, p.Participants, cfg.StopTimeout)
		if err != nil {
			return err
		}
		p.Lifecycle.Append(fx.Hook{OnStart: c.Start, OnStop: c.Stop})
		return nil
	}))
	return fx.New(options...)
}

type structParams struct {
	fx.In
	Lifecycle    fx.Lifecycle
	Participants []lifecycle.Participant `group:"backendcore.lifecycle"`
}

// Run creates an app, starts it and waits for a shutdown signal using Fx Run.
// CLI use only: Fx may exit the process on errors.
func Run(info app.Info, modules ...fx.Option) { New(info, modules...).Run() }
