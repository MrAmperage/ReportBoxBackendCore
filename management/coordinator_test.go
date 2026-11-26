package management

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/internal/coordinator"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
)

func TestCoordinatorRollback(t *testing.T) {
	// Fail a later participant after management has bound and activated.
	var addr string
	state := new(coordinator.State)
	reg := health.New(state.Phase, time.Second)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultConfig()
	cfg.Address = "127.0.0.1:0"
	s, e := newServer(cfg, reg, log, func(error) error { return nil }, MetricsHandler{})
	if e != nil {
		t.Fatal(e)
	}
	cause := errors.New("startup failure")
	c, e := coordinator.New(state, log, []lifecycle.Participant{s.participant(), {Name: "test.failure", DependsOn: []string{ParticipantName}, Activate: func(context.Context) error { addr = s.Address(); return cause }}}, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e = c.Start(ctx); !errors.Is(e, cause) {
		t.Fatalf("unexpected startup error: %v", e)
	}
	if s.Address() != "" || reg.Live() || reg.Ready(ctx) {
		t.Fatal("rollback did not release listener/state")
	}
	replacement, e := net.Listen("tcp", addr)
	if e != nil {
		t.Fatal(e)
	}
	replacement.Close()
}
