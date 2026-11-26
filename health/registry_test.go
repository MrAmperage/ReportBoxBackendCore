package health

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadiness(t *testing.T) {
	var state atomic.Uint32
	var unavailable atomic.Bool
	r := New(func() Phase { return Phase(state.Load()) }, time.Second)
	if err := r.Register(Check{Name: "db", Required: true, Run: func(context.Context) error {
		if unavailable.Load() {
			return errors.New("secret")
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Check{Name: "optional", Run: func(context.Context) error { return errors.New("optional") }}); err != nil {
		t.Fatal(err)
	}
	if r.Ready(context.Background()) {
		t.Fatal("ready before start")
	}
	state.Store(uint32(Running))
	if !r.Ready(context.Background()) {
		t.Fatal("not ready")
	}
	unavailable.Store(true)
	if r.Ready(context.Background()) || !r.Live() {
		t.Fatal("invalid dependency status")
	}
	unavailable.Store(false)
	if !r.Ready(context.Background()) {
		t.Fatal("did not recover")
	}
	results, ok := r.Probe(context.Background())
	if !ok || len(results) != 2 || results[1].Healthy {
		t.Fatalf("%+v %v", results, ok)
	}
}
func TestLateProbe(t *testing.T) {
	var state atomic.Uint32
	r := New(func() Phase { return Phase(state.Load()) }, time.Second)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan bool, 1)
	if err := r.Register(Check{Name: "slow", Required: true, Run: func(ctx context.Context) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}); err != nil {
		t.Fatal(err)
	}
	state.Store(uint32(Running))
	go func() { done <- r.Ready(context.Background()) }()
	<-entered
	state.Store(uint32(Stopping))
	close(release)
	if <-done {
		t.Fatal("late success restored readiness")
	}
}
func TestBudget(t *testing.T) {
	r := New(func() Phase { return Running }, time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r.Ready(ctx) {
		t.Fatal("canceled probe ready")
	}
}
