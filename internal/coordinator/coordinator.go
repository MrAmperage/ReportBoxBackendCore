// Package coordinator implements ordered lifecycle phases for managed participants.
package coordinator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
)

// State provides the process phase independently of slow hooks.
type State struct{ value atomic.Uint32 }

func (s *State) Phase() health.Phase { return health.Phase(s.value.Load()) }
func (s *State) Fail() {
	for {
		p := s.Phase()
		if p != health.Starting && p != health.Running {
			return
		}
		if s.value.CompareAndSwap(uint32(p), uint32(health.Failed)) {
			return
		}
	}
}

// Coordinator owns participants and serializes start/stop.
type Coordinator struct {
	state          *State
	logger         *slog.Logger
	order          []lifecycle.Participant
	rollbackLimit  time.Duration
	mu             sync.Mutex
	opened, active int
}

// New validates the participant graph without performing resource operations.
func New(state *State, logger *slog.Logger, participants []lifecycle.Participant, rollbackLimit time.Duration) (*Coordinator, error) {
	order, err := ordered(participants)
	if err != nil {
		return nil, err
	}
	return &Coordinator{state: state, logger: logger, order: order, rollbackLimit: rollbackLimit}, nil
}

func ordered(participants []lifecycle.Participant) ([]lifecycle.Participant, error) {
	byName := make(map[string]lifecycle.Participant, len(participants))
	names := make([]string, 0, len(participants))
	for _, p := range participants {
		if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Name) != p.Name {
			return nil, errors.New("invalid lifecycle participant name")
		}
		if _, ok := byName[p.Name]; ok {
			return nil, fmt.Errorf("duplicate lifecycle participant %q", p.Name)
		}
		p.DependsOn = append([]string(nil), p.DependsOn...)
		sort.Strings(p.DependsOn)
		byName[p.Name] = p
		names = append(names, p.Name)
	}
	sort.Strings(names)
	visited := make(map[string]uint8, len(names))
	result := make([]lifecycle.Participant, 0, len(names))
	var visit func(string) error
	visit = func(name string) error {
		if visited[name] == 1 {
			return fmt.Errorf("lifecycle dependency cycle at %q", name)
		}
		if visited[name] == 2 {
			return nil
		}
		p, ok := byName[name]
		if !ok {
			return fmt.Errorf("missing lifecycle dependency %q", name)
		}
		visited[name] = 1
		for _, dep := range p.DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visited[name] = 2
		result = append(result, p)
		return nil
	}
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// Start opens all resources before activating any participant. A portion of
// the caller's deadline is reserved for rollback, when a deadline exists.
func (c *Coordinator) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.state.value.CompareAndSwap(uint32(health.Idle), uint32(health.Starting)) {
		return errors.New("lifecycle can only start once")
	}
	work := ctx
	cancel := func() {}
	if end, ok := ctx.Deadline(); ok {
		remaining := time.Until(end)
		reserve := min(remaining/5, 5*time.Second, c.rollbackLimit)
		if reserve > 0 {
			work, cancel = context.WithDeadline(ctx, end.Add(-reserve))
		}
	}
	defer cancel()
	fail := func(err error) error {
		c.state.Fail()
		cleanupBase := context.WithoutCancel(ctx)
		end := time.Now().Add(c.rollbackLimit)
		if original, ok := ctx.Deadline(); ok && original.Before(end) {
			end = original
		}
		cleanup, done := context.WithDeadline(cleanupBase, end)
		defer done()
		return errors.Join(err, c.release(cleanup))
	}
	for _, p := range c.order {
		if err := work.Err(); err != nil {
			return fail(err)
		}
		if c.state.Phase() != health.Starting {
			return fail(errors.New("runtime failure during startup"))
		}
		if err := call(work, p.Name, "open", p.Open); err != nil {
			return fail(err)
		}
		c.opened++
	}
	for _, p := range c.order {
		if err := work.Err(); err != nil {
			return fail(err)
		}
		if c.state.Phase() != health.Starting {
			return fail(errors.New("runtime failure during startup"))
		}
		if err := call(work, p.Name, "activate", p.Activate); err != nil {
			return fail(err)
		}
		c.active++
	}
	if err := work.Err(); err != nil {
		return fail(err)
	}
	if !c.state.value.CompareAndSwap(uint32(health.Starting), uint32(health.Running)) {
		return fail(errors.New("runtime failure during startup"))
	}
	c.logger.Info("managed lifecycle running")
	return nil
}

// Stop removes readiness before quiescing all participants, draining work,
// and closing resources. Each phase follows reverse dependency order.
func (c *Coordinator) Stop(ctx context.Context) error {
	// Mark immediately, even if a cooperative startup hook is still unwinding.
	for {
		p := c.state.Phase()
		if p == health.Failed || p == health.Stopped || p == health.Stopping {
			break
		}
		if c.state.value.CompareAndSwap(uint32(p), uint32(health.Stopping)) {
			break
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Phase() == health.Stopped {
		return nil
	}
	err := c.release(ctx)
	c.state.value.Store(uint32(health.Stopped))
	c.logger.Info("managed lifecycle stopped")
	return err
}

func (c *Coordinator) release(ctx context.Context) error {
	var errs []error
	// Even on deadline/error, give every callback a chance to perform its
	// nonblocking forced cleanup. Callbacks must respect cancellation.
	for i := c.active - 1; i >= 0; i-- {
		p := c.order[i]
		if err := call(ctx, p.Name, "quiesce", p.Quiesce); err != nil {
			errs = append(errs, err)
		}
	}
	for i := c.active - 1; i >= 0; i-- {
		p := c.order[i]
		if err := call(ctx, p.Name, "drain", p.Drain); err != nil {
			errs = append(errs, err)
		}
	}
	c.active = 0
	for i := c.opened - 1; i >= 0; i-- {
		p := c.order[i]
		if err := call(ctx, p.Name, "close", p.Close); err != nil {
			errs = append(errs, err)
		}
	}
	c.opened = 0
	if err := ctx.Err(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
func call(ctx context.Context, name, phase string, fn func(context.Context) error) error {
	if fn == nil {
		return nil
	}
	if err := fn(ctx); err != nil {
		return fmt.Errorf("lifecycle %s %s: %w", name, phase, err)
	}
	return nil
}
