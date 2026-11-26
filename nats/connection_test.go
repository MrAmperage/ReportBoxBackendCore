package nats

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	native "github.com/nats-io/nats.go"
)

func TestAsyncPolicyFailure(t *testing.T) {
	owner := &Connection{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	owner.noteAsyncError(native.ErrSlowConsumer)
	if owner.policyFailed.Load() || owner.drainFailed.Load() {
		t.Fatal("slow consumer must not permanently fail readiness")
	}
	owner.noteAsyncError(fmt.Errorf("wrapped: %w", native.ErrPermissionViolation))
	if !owner.policyFailed.Load() || owner.drainFailed.Load() {
		t.Fatal("permission failure must persist")
	}
	owner.closing.Store(true)
	owner.noteAsyncError(native.ErrDrainTimeout)
	if !owner.policyFailed.Load() || !owner.drainFailed.Load() {
		t.Fatal("drain failure must persist")
	}
	if !isPolicyError(native.ErrMaxSubscriptionsExceeded) {
		t.Fatal("subscription limit must fail policy")
	}
}
