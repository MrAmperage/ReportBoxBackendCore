package main

import (
	"context"
	"log/slog"

	backendcore "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	"go.uber.org/fx"
)

var version = "dev"
var commit = "unknown"

func main() {
	backendcore.Run(app.Info{Name: "reportbox-basic-service", Version: version, Commit: commit},
		fx.Provide(fx.Annotate(newParticipant, fx.ResultTags(`group:"backendcore.lifecycle"`))),
	)
}
func newParticipant(logger *slog.Logger) lifecycle.Participant {
	return lifecycle.Participant{Name: "example", Activate: func(ctx context.Context) error { logger.InfoContext(ctx, "example activated"); return nil }, Drain: func(ctx context.Context) error { logger.InfoContext(ctx, "example drained"); return nil }}
}
