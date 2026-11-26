package management

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
)

// ParticipantName identifies the management listener in the Core lifecycle.
const ParticipantName = "management.server"

// MetricsHandler is an optional, explicitly injected metrics endpoint.
// Its owner must keep it usable until all management requests have drained.
// A nil Handler disables /metrics. Handlers must respect request cancellation.
type MetricsHandler struct{ Handler http.Handler }

// Server owns a private HTTP listener; construction is passive.
type Server struct {
	cfg       Config
	registry  *health.Registry
	logger    *slog.Logger
	report    lifecycle.ReportFailure
	metrics   http.Handler
	listener  net.Listener
	server    *http.Server
	cancel    context.CancelFunc
	address   atomic.Value
	stopping  atomic.Bool
	started   bool // lifecycle callbacks are serialized by Core
	done      chan struct{}
	mu        sync.Mutex
	accepting bool
	active    int
	drained   chan struct{}
}

func newServer(cfg Config, registry *health.Registry, logger *slog.Logger, report lifecycle.ReportFailure, metrics MetricsHandler) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, registry: registry, logger: logger, report: report, metrics: metrics.Handler, done: make(chan struct{}), drained: make(chan struct{})}, nil
}

// Address returns the bound TCP address, or empty before Open and after Close.
func (s *Server) Address() string { v, _ := s.address.Load().(string); return v }
func (s *Server) participant() lifecycle.Participant {
	return lifecycle.Participant{Name: ParticipantName, Open: s.open, Activate: s.activate, Quiesce: s.quiesce, Drain: s.drain, Close: s.close}
}
func (s *Server) open(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.cfg.Address)
	if err != nil {
		return errors.New("management listener bind failed")
	}
	if err = ctx.Err(); err != nil {
		_ = l.Close()
		return err
	}
	life, cancel := context.WithCancel(context.Background())
	s.listener = l
	s.cancel = cancel
	s.server = &http.Server{Handler: http.HandlerFunc(s.serveHTTP), ReadHeaderTimeout: s.cfg.ReadHeaderTimeout, ReadTimeout: s.cfg.ReadHeaderTimeout, WriteTimeout: s.cfg.RequestTimeout + s.cfg.ReadHeaderTimeout, IdleTimeout: s.cfg.IdleTimeout, MaxHeaderBytes: s.cfg.MaxHeaderBytes, BaseContext: func(net.Listener) context.Context { return life }, ErrorLog: log.New(io.Discard, "", 0)}
	s.address.Store(l.Addr().String())
	return nil
}
func (s *Server) activate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.accepting = true
	s.mu.Unlock()
	s.started = true
	go func() {
		defer close(s.done)
		_ = s.server.Serve(s.listener)
		if !s.stopping.Load() {
			if err := s.report(errors.New("management listener terminated unexpectedly")); err != nil {
				s.logger.Error("management shutdown request failed")
			}
		}
	}()
	return nil
}
func (s *Server) quiesce(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopping.Swap(true) {
		s.accepting = false
		if s.active == 0 {
			close(s.drained)
		}
	}
	return nil
}
func (s *Server) drain(ctx context.Context) error {
	_ = s.quiesce(ctx)
	if s.server == nil {
		return nil
	}
	// Reserve time for cancellation and the completion of cooperative handlers.
	graceful := ctx
	cancel := func() {}
	if end, ok := ctx.Deadline(); ok {
		if reserve := min(time.Until(end)/5, time.Second); reserve > 0 {
			graceful, cancel = context.WithDeadline(ctx, end.Add(-reserve))
		}
	}
	err := s.server.Shutdown(graceful)
	cancel()
	if err != nil {
		s.cancel()
		err = errors.Join(err, s.server.Close())
	}
	select {
	case <-s.drained:
	case <-ctx.Done():
		s.cancel()
		err = errors.Join(err, ctx.Err(), s.server.Close())
	}
	return err
}
func (s *Server) close(ctx context.Context) error {
	_ = s.quiesce(ctx)
	if s.cancel != nil {
		s.cancel()
	}
	var err error
	if s.server != nil {
		err = s.server.Close()
	}
	if s.listener != nil {
		if e := s.listener.Close(); e != nil && !errors.Is(e, net.ErrClosed) {
			err = errors.Join(err, errors.New("management listener close failed"))
		}
	}
	if s.started {
		select {
		case <-s.done:
		case <-ctx.Done():
			err = errors.Join(err, ctx.Err())
		}
	}
	s.address.Store("")
	return err
}
func (s *Server) admit() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.accepting || !s.registry.Running() || s.active >= s.cfg.MaxConcurrent {
		return false
	}
	s.active++
	return true
}
func (s *Server) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active--
	if s.stopping.Load() && s.active == 0 {
		close(s.drained)
	}
}
