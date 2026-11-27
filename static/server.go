package static

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
)

// ParticipantName identifies the static listener in the Core lifecycle.
const ParticipantName = "static.server"

// Server owns the filesystem root and HTTP listener. It is constructed passively.
type Server struct {
	cfg      Config
	registry *health.Registry
	logger   *slog.Logger
	report   lifecycle.ReportFailure
	root     *os.Root
	listener net.Listener
	http     *http.Server
	address  atomic.Value
	stopping atomic.Bool
	started  bool
	done     chan struct{}
	slots    chan struct{}
}

func newServer(cfg Config, registry *health.Registry, logger *slog.Logger, report lifecycle.ReportFailure) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, registry: registry, logger: logger, report: report, done: make(chan struct{}), slots: make(chan struct{}, cfg.MaxConcurrent)}, nil
}

// Address returns the bound address, or empty before Open and after Close.
func (s *Server) Address() string { v, _ := s.address.Load().(string); return v }
func (s *Server) participant() lifecycle.Participant {
	return lifecycle.Participant{Name: ParticipantName, Open: s.open, Activate: s.activate, Quiesce: s.quiesce, Drain: s.drain, Close: s.close}
}
func (s *Server) open(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.cfg.Root)
	if err != nil {
		return errors.New("static root open failed")
	}
	// The deployment tree is immutable while running. Reject links/special files
	// at startup as an additional safeguard; os.Root confines later file opens.
	files := root.FS()
	err = fs.WalkDir(files, ".", func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if !d.IsDir() {
			// Lstat does not follow symlinks and keeps validation inside Root.
			info, err := root.Lstat(name)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return errors.New("invalid static file type")
			}
		}
		return nil
	})
	if err != nil {
		root.Close()
		return errors.New("static root validation failed")
	}
	if s.cfg.SPA {
		info, e := fs.Stat(files, "index.html")
		if e != nil || !info.Mode().IsRegular() {
			root.Close()
			return errors.New("static SPA requires index.html")
		}
	}
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.cfg.Address)
	if err != nil {
		root.Close()
		return errors.New("static listener bind failed")
	}
	if err = ctx.Err(); err != nil {
		l.Close()
		root.Close()
		return err
	}
	h := handler{files: files, spa: s.cfg.SPA}
	s.root = root
	s.listener = l
	s.http = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if s.stopping.Load() || !s.registry.Running() {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "unavailable", 503)
			return
		}
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		default:
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "unavailable", 503)
			return
		}
		if s.stopping.Load() || !s.registry.Running() {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "unavailable", 503)
			return
		}
		h.ServeHTTP(w, r)
	}), ReadHeaderTimeout: s.cfg.ReadHeaderTimeout, ReadTimeout: s.cfg.ReadHeaderTimeout, WriteTimeout: s.cfg.WriteTimeout, IdleTimeout: s.cfg.IdleTimeout, MaxHeaderBytes: s.cfg.MaxHeaderBytes, ErrorLog: log.New(io.Discard, "", 0)}
	s.address.Store(l.Addr().String())
	return nil
}
func (s *Server) activate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.started = true
	go func() {
		defer close(s.done)
		_ = s.http.Serve(s.listener)
		if !s.stopping.Load() {
			if err := s.report(errors.New("static listener terminated unexpectedly")); err != nil {
				s.logger.Error("static shutdown request failed")
			}
		}
	}()
	return nil
}
func (s *Server) quiesce(context.Context) error { s.stopping.Store(true); return nil }
func (s *Server) drain(ctx context.Context) error {
	_ = s.quiesce(ctx)
	if s.http == nil {
		return nil
	}
	if err := s.http.Shutdown(ctx); err != nil {
		return errors.Join(err, s.http.Close())
	}
	return nil
}
func (s *Server) close(ctx context.Context) error {
	_ = s.quiesce(ctx)
	var err error
	if s.http != nil {
		err = s.http.Close()
	}
	if s.listener != nil {
		if e := s.listener.Close(); e != nil && !errors.Is(e, net.ErrClosed) {
			err = errors.Join(err, errors.New("static listener close failed"))
		}
	}
	if s.started {
		select {
		case <-s.done:
		case <-ctx.Done():
			err = errors.Join(err, ctx.Err())
		}
	}
	if s.root != nil {
		if e := s.root.Close(); e != nil {
			err = errors.Join(err, errors.New("static root close failed"))
		}
		s.root = nil
	}
	s.address.Store("")
	return err
}
