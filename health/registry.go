// Package health provides process and dependency health without opening listeners.
package health

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// Phase describes the coordinator state.
type Phase uint8

const (
	Idle Phase = iota
	Starting
	Running
	Stopping
	Stopped
	Failed
)

// Check describes a named dependency probe. Optional checks do not gate readiness.
type Check struct {
	Name     string
	Required bool
	Timeout  time.Duration
	Run      func(context.Context) error
}

// Result exposes only a check name and status, not raw dependency errors.
type Result struct {
	Name    string
	Healthy bool
}

// Registry serializes probe execution and protects registrations.
type Registry struct {
	phase  func() Phase
	budget time.Duration
	mu     sync.RWMutex
	checks map[string]Check
	gate   chan struct{}
}

// New creates a registry. The phase function must be concurrency-safe.
func New(phase func() Phase, budget time.Duration) *Registry {
	if budget <= 0 {
		budget = 2 * time.Second
	}
	return &Registry{phase: phase, budget: budget, checks: make(map[string]Check), gate: make(chan struct{}, 1)}
}

// Register adds a probe; names must be unique. Register before starting the app.
func (r *Registry) Register(c Check) error {
	if strings.TrimSpace(c.Name) == "" || c.Run == nil || c.Timeout < 0 {
		return errors.New("invalid health check")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.phase() != Idle {
		return errors.New("health registration is closed")
	}
	if _, ok := r.checks[c.Name]; ok {
		return errors.New("duplicate health check")
	}
	r.checks[c.Name] = c
	return nil
}

// Live reports process health, independently of external dependencies.
func (r *Registry) Live() bool { p := r.phase(); return p == Starting || p == Running || p == Stopping }

// Ready checks required dependencies within a shared time budget. A successful
// late probe cannot restore readiness after shutdown. Checks must honor context.
func (r *Registry) Ready(ctx context.Context) bool {
	if r.phase() != Running {
		return false
	}
	results, ok := r.probe(ctx, true)
	if !ok {
		return false
	}
	for _, result := range results {
		if !result.Healthy {
			return false
		}
	}
	return r.phase() == Running
}

// Probe returns sanitized results of required and optional dependency checks.
// complete is false if the probe budget expired or the app is not running.
func (r *Registry) Probe(ctx context.Context) (results []Result, complete bool) {
	return r.probe(ctx, false)
}

func (r *Registry) probe(ctx context.Context, requiredOnly bool) ([]Result, bool) {
	if r.phase() != Running {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, r.budget)
	defer cancel()
	select {
	case r.gate <- struct{}{}:
		defer func() { <-r.gate }()
	case <-ctx.Done():
		return nil, false
	}
	if ctx.Err() != nil || r.phase() != Running {
		return nil, false
	}
	r.mu.RLock()
	checks := make([]Check, 0, len(r.checks))
	for _, c := range r.checks {
		if !requiredOnly || c.Required {
			checks = append(checks, c)
		}
	}
	r.mu.RUnlock()
	sort.Slice(checks, func(i, j int) bool {
		if checks[i].Required != checks[j].Required {
			return checks[i].Required
		}
		return checks[i].Name < checks[j].Name
	})
	results := make([]Result, 0, len(checks))
	for _, c := range checks {
		if ctx.Err() != nil || r.phase() != Running {
			return results, false
		}
		checkCtx := ctx
		cancelCheck := func() {}
		if c.Timeout > 0 {
			checkCtx, cancelCheck = context.WithTimeout(ctx, c.Timeout)
		}
		err := c.Run(checkCtx)
		healthy := err == nil && checkCtx.Err() == nil
		cancelCheck()
		results = append(results, Result{Name: c.Name, Healthy: healthy})
	}
	return results, ctx.Err() == nil && r.phase() == Running
}
