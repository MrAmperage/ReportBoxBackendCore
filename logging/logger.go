// Package logging supplies structured, per-application loggers.
package logging

import (
	"io"
	"log/slog"

	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
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
	return slog.New(h).With("service", info.Name, "version", info.Version, "commit", info.Commit, "environment", cfg.Environment), nil
}
