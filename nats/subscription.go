package nats

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/internal/observe"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	native "github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
	"net/http"
)

// Subscription describes a loss-tolerant Core NATS subscription. One callback
// runs at a time per registration; queue groups distribute across subscribers.
type Subscription struct {
	Subject, Queue                string
	Handle                        func(context.Context, *native.Msg) error
	Timeout                       time.Duration
	PendingMessages, PendingBytes int
}

// ProvideSubscription registers a constructor returning one Subscription.
func ProvideSubscription(constructor any) fx.Option {
	return fx.Provide(fx.Annotate(constructor, fx.ResultTags(`group:"backendcore.nats.subscriptions"`)))
}

type subParams struct {
	fx.In
	Conn     *Connection
	Health   *health.Registry
	Logger   *slog.Logger
	Observer *observe.Instruments `optional:"true"`
	Specs    []Subscription       `group:"backendcore.nats.subscriptions"`
}
type subscriptions struct {
	p         subParams
	subs      []*native.Subscription
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	accepting bool
	active    int
	done      chan struct{}
	stopped   bool
}

func newSubscriptions(p subParams) (*subscriptions, error) {
	for _, s := range p.Specs {
		if s.Handle == nil || strings.TrimSpace(s.Subject) == "" || s.Timeout <= 0 || s.PendingMessages <= 0 || s.PendingBytes <= 0 {
			return nil, errors.New("invalid NATS subscription")
		}
	}
	return &subscriptions{p: p, done: make(chan struct{})}, nil
}
func (s *subscriptions) participant() lifecycle.Participant {
	return lifecycle.Participant{Name: "nats.subscriptions", DependsOn: s.p.Observer.Dependencies("nats.connection"), Activate: s.activate, Quiesce: s.quiesce, Drain: s.drain, Close: s.close}
}
func (s *subscriptions) activate(ctx context.Context) error {
	if len(s.p.Specs) == 0 {
		return nil
	}
	nc, err := s.p.Conn.Conn()
	if err != nil {
		return err
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.mu.Lock()
	s.accepting = true
	s.mu.Unlock()
	fail := func() error { _ = s.quiesce(ctx); s.cancel(); return errors.New("NATS subscription activation failed") }
	for _, spec := range s.p.Specs {
		if ctx.Err() != nil {
			return fail()
		}
		cb := func(m *native.Msg) { s.handle(spec, m) }
		var sub *native.Subscription
		if spec.Queue == "" {
			sub, err = nc.Subscribe(spec.Subject, cb)
		} else {
			sub, err = nc.QueueSubscribe(spec.Subject, spec.Queue, cb)
		}
		if err != nil {
			return fail()
		}
		s.subs = append(s.subs, sub)
		if sub.SetPendingLimits(spec.PendingMessages, spec.PendingBytes) != nil {
			return fail()
		}
	}
	flush, done := context.WithTimeout(ctx, s.p.Conn.cfg.ConnectTimeout)
	defer done()
	if err := nc.FlushWithContext(flush); err != nil || !nc.IsConnected() || s.p.Conn.policyFailed.Load() || isPolicyError(nc.LastError()) {
		return fail()
	}
	return nil
}
func (s *subscriptions) handle(spec Subscription, m *native.Msg) {
	s.mu.Lock()
	if !s.accepting || !s.p.Health.Running() {
		s.mu.Unlock()
		return
	}
	s.active++
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(s.ctx, spec.Timeout)
	defer cancel()
	ctx, finish := s.p.Observer.Start(s.p.Observer.Extract(ctx, http.Header(m.Header)), "nats.receive", "nats.receive", trace.SpanKindConsumer)
	outcome := "ok"
	defer func() {
		if recover() != nil {
			outcome = "error"
			s.p.Logger.ErrorContext(ctx, "NATS subscription handler panic")
		}
		if ctx.Err() != nil {
			outcome = "canceled"
		}
		finish(outcome)
		s.mu.Lock()
		s.active--
		if s.stopped && s.active == 0 {
			close(s.done)
		}
		s.mu.Unlock()
	}()
	if spec.Handle(ctx, m) != nil {
		outcome = "error"
		s.p.Logger.WarnContext(ctx, "NATS subscription handler failed")
	}
}
func (s *subscriptions) quiesce(context.Context) error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	s.accepting = false
	if s.active == 0 {
		close(s.done)
	}
	s.mu.Unlock()
	var err error
	for _, sub := range s.subs {
		if e := sub.Unsubscribe(); e != nil && !errors.Is(e, native.ErrBadSubscription) && !errors.Is(e, native.ErrConnectionClosed) {
			err = errors.New("NATS unsubscribe failed")
		}
	}
	return err
}
func (s *subscriptions) drain(ctx context.Context) error {
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		if s.cancel != nil {
			s.cancel()
		}
		return ctx.Err()
	}
}
func (s *subscriptions) close(ctx context.Context) error {
	err := s.quiesce(ctx)
	if s.cancel != nil {
		s.cancel()
	}
	return err
}
