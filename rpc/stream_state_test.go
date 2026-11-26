package rpc

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStreamTrackerCloseFinishesObservations(t *testing.T) {
	var tracker streamTracker
	var completed atomic.Int32
	for range 64 {
		tracker.start(context.Background(), func(outcome string) {
			if outcome != "canceled" {
				t.Errorf("unexpected outcome %s", outcome)
			}
			completed.Add(1)
		})
	}
	tracker.close()
	if completed.Load() != 64 {
		t.Fatal("close returned before spans ended")
	}
	tracker.start(context.Background(), func(outcome string) {
		if outcome != "canceled" {
			t.Error(outcome)
		}
		completed.Add(1)
	})
	tracker.close()
	if completed.Load() != 65 {
		t.Fatal("late or duplicate span")
	}
}
func TestStreamTrackerConcurrentEnd(t *testing.T) {
	var tracker streamTracker
	var completed atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for range 64 {
		state := tracker.start(ctx, func(string) { completed.Add(1) })
		wg.Add(1)
		go func() { defer wg.Done(); state.end("ok") }()
	}
	cancel()
	tracker.close()
	wg.Wait()
	if completed.Load() != 64 {
		t.Fatalf("completed %d", completed.Load())
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if len(tracker.active) != 0 {
		t.Fatal("leaked observation")
	}
}
func TestStreamTrackerDeadline(t *testing.T) {
	var tracker streamTracker
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	done := make(chan string, 1)
	tracker.start(ctx, func(s string) { done <- s })
	select {
	case s := <-done:
		if s != "deadline_exceeded" {
			t.Fatal(s)
		}
	case <-time.After(time.Second):
		t.Fatal("span not ended")
	}
	tracker.close()
}

func TestStreamTrackerWaitsForRacingFinish(t *testing.T) {
	var tracker streamTracker
	entered, release, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s := tracker.start(context.Background(), func(string) { close(entered); <-release })
	go s.end("ok")
	<-entered
	go func() { tracker.close(); close(closed) }()
	select {
	case <-closed:
		close(release)
		t.Fatal("close overtook finish")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close deadlocked")
	}
}
