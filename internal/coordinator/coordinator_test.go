package coordinator

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
)

func logger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func TestPhases(t *testing.T) {
	s := new(State)
	var events []string
	participant := func(name string, deps ...string) lifecycle.Participant {
		f := func(phase string) func(context.Context) error {
			return func(context.Context) error {
				events = append(events, phase+":"+name)
				if phase == "quiesce" && s.Phase() != health.Stopping {
					t.Error("ready during shutdown")
				}
				return nil
			}
		}
		return lifecycle.Participant{Name: name, DependsOn: deps, Open: f("open"), Activate: f("activate"), Quiesce: f("quiesce"), Drain: f("drain"), Close: f("close")}
	}
	c, err := New(s, logger(), []lifecycle.Participant{participant("api", "db"), participant("db")}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Start(context.Background()); err != nil || s.Phase() != health.Running {
		t.Fatalf("start %v", err)
	}
	if err = c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"open:db", "open:api", "activate:db", "activate:api", "quiesce:api", "quiesce:db", "drain:api", "drain:db", "close:api", "close:db"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("%v", events)
	}
}
func TestInvalidGraph(t *testing.T) {
	for _, ps := range [][]lifecycle.Participant{{{Name: "a"}, {Name: "a"}}, {{Name: "a", DependsOn: []string{"missing"}}}, {{Name: "a", DependsOn: []string{"b"}}, {Name: "b", DependsOn: []string{"a"}}}} {
		if _, err := New(new(State), logger(), ps, time.Second); err == nil {
			t.Fatal("invalid graph accepted")
		}
	}
}
func TestRollback(t *testing.T) {
	s := new(State)
	var closed, quiesced bool
	failure := errors.New("activation failed")
	cleanup := errors.New("cleanup failed")
	c, err := New(s, logger(), []lifecycle.Participant{{Name: "a", Quiesce: func(context.Context) error { quiesced = true; return nil }, Close: func(context.Context) error { closed = true; return cleanup }}, {Name: "b", DependsOn: []string{"a"}, Activate: func(context.Context) error { return failure }}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	err = c.Start(context.Background())
	if !errors.Is(err, failure) || !errors.Is(err, cleanup) || !closed || !quiesced || s.Phase() != health.Failed {
		t.Fatalf("rollback: %v %v %v", err, closed, quiesced)
	}
}
func TestCanceledStartRollback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var clean bool
	c, err := New(new(State), logger(), []lifecycle.Participant{{Name: "a", Open: func(context.Context) error { cancel(); return nil }, Close: func(ctx context.Context) error { clean = ctx.Err() == nil; return nil }}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Start(ctx); !errors.Is(err, context.Canceled) || !clean {
		t.Fatalf("%v cleanup=%v", err, clean)
	}
}

func TestStartupDeadline(t *testing.T) {
	s := new(State)
	clean := false
	c, err := New(s, logger(), []lifecycle.Participant{{Name: "a", Close: func(ctx context.Context) error { clean = true; return ctx.Err() }}, {Name: "b", DependsOn: []string{"a"}, Open: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err = c.Start(ctx); !errors.Is(err, context.DeadlineExceeded) || !clean || s.Phase() != health.Failed {
		t.Fatalf("%v clean=%v phase=%v", err, clean, s.Phase())
	}
}

func TestHookPanicRollback(t *testing.T) {
	for _, phase := range []string{"open", "activate"} {
		t.Run(phase, func(t *testing.T) {
			state := new(State)
			var events []string
			a := lifecycle.Participant{Name: "a", Close: func(context.Context) error { events = append(events, "close:a"); return nil }}
			b := lifecycle.Participant{Name: "b", DependsOn: []string{"a"}, Close: func(context.Context) error { events = append(events, "close:b"); return nil }}
			crash := func(context.Context) error { panic("secret panic value") }
			want := []string{"close:a"}
			if phase == "open" {
				b.Open = crash
			} else {
				b.Activate = crash
				want = []string{"close:b", "close:a"}
			}
			c, err := New(state, logger(), []lifecycle.Participant{b, a}, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			err = c.Start(context.Background())
			if err == nil || !strings.Contains(err.Error(), "lifecycle b "+phase+": callback panic") || strings.Contains(err.Error(), "secret") || state.Phase() != health.Failed || !reflect.DeepEqual(events, want) {
				t.Fatalf("err=%v state=%v events=%v", err, state.Phase(), events)
			}
		})
	}
}

func TestHookPanicDoesNotSkipShutdown(t *testing.T) {
	var events []string
	normal := func(name string) func(context.Context) error {
		return func(context.Context) error { events = append(events, name); return nil }
	}
	crash := func(name string) func(context.Context) error {
		return func(context.Context) error { events = append(events, name); panic("secret") }
	}
	state := new(State)
	c, err := New(state, logger(), []lifecycle.Participant{
		{Name: "a", Quiesce: normal("quiesce:a"), Drain: normal("drain:a"), Close: normal("close:a")},
		{Name: "b", DependsOn: []string{"a"}, Quiesce: crash("quiesce:b"), Drain: crash("drain:b"), Close: crash("close:b")},
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	err = c.Stop(context.Background())
	if err == nil || strings.Count(err.Error(), "callback panic") != 3 || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"quiesce:b", "quiesce:a", "drain:b", "drain:a", "close:b", "close:a"}
	if !reflect.DeepEqual(events, want) || state.Phase() != health.Stopped {
		t.Fatalf("events=%v phase=%v", events, state.Phase())
	}
	if err = c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
