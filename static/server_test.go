package static

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/health"
)

func TestForcedDrainWaitsForHandler(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Root = t.TempDir()
	cfg.Address = "127.0.0.1:0"
	cfg.MaxConcurrent = 1
	reg := health.New(func() health.Phase { return health.Running }, time.Second)
	s, err := newServer(cfg, reg, slog.New(slog.NewTextHandler(io.Discard, nil)), func(error) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err = s.open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.close(ctx); err != nil {
			t.Error(err)
		}
	})
	entered := make(chan struct{})
	canceled := make(chan struct{})
	allowExit := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-allowExit:
		default:
			close(allowExit)
		}
	})
	s.http.Handler = s.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(canceled)
		<-allowExit
	}))
	if err = s.activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		resp, err := client.Get("http://" + s.Address() + "/")
		if err == nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("handler not entered")
	}
	resp, err := client.Get("http://" + s.Address() + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("overload status %d", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.drain(ctx) }()
	select {
	case <-canceled:
	case <-ctx.Done():
		t.Fatal("handler not canceled before deadline")
	}
	select {
	case err := <-result:
		t.Fatalf("drain returned before handler exit: %v", err)
	default:
	}
	close(allowExit)
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected forced shutdown error, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("drain did not wait successfully")
	}
	select {
	case <-s.drained:
	default:
		t.Fatal("active handlers not drained")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("client did not exit")
	}
}
