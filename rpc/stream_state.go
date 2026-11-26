package rpc

import (
	"context"
	"sync"
)

// streamTracker finishes all admitted streaming observations before the client
// lifecycle closes the telemetry provider. It never waits for network I/O.
// The zero value is ready to use; a tracker is not reopened after Close.
type streamTracker struct {
	mu     sync.Mutex
	closed bool
	active map[*streamObservation]struct{}
}

func (t *streamTracker) start(ctx context.Context, finish func(string)) *streamObservation {
	s := &streamObservation{tracker: t, finish: finish}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		s.end("canceled")
		return s
	}
	if t.active == nil {
		t.active = make(map[*streamObservation]struct{})
	}
	t.active[s] = struct{}{}
	t.mu.Unlock()
	s.mu.Lock()
	if !s.done {
		s.stop = context.AfterFunc(ctx, func() {
			outcome := "canceled"
			if ctx.Err() == context.DeadlineExceeded {
				outcome = "deadline_exceeded"
			}
			s.end(outcome)
		})
	}
	s.mu.Unlock()
	return s
}

func (t *streamTracker) close() {
	t.mu.Lock()
	t.closed = true
	active := make([]*streamObservation, 0, len(t.active))
	for s := range t.active {
		active = append(active, s)
	}
	t.mu.Unlock()
	for _, s := range active {
		s.end("canceled")
	}
}

type streamObservation struct {
	tracker *streamTracker
	finish  func(string)
	mu      sync.Mutex
	done    bool
	stop    func() bool
}

func (s *streamObservation) end(outcome string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	s.done = true
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
	// Keep the observation locked and registered until Finish actually returns.
	// A racing tracker.close must not return while this span is being ended.
	defer func() {
		s.tracker.mu.Lock()
		delete(s.tracker.active, s)
		s.tracker.mu.Unlock()
	}()
	s.finish(outcome)
}
