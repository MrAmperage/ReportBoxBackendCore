// Package lifecycle defines the extension point for coordinated resources.
package lifecycle

import "context"

// Group is the Fx value group containing Participant values.
const Group = "backendcore.lifecycle"

// Participant describes one managed component. All callbacks must honor their
// context. A callback panic is reported as a failed phase; the coordinator
// continues cleanup of previously opened resources. Open must clean up its
// own partial failure (including panic). Activate must undo its
// partial activation on error; Close will still be called after a successful Open.
// Quiesce stops intake without waiting for ongoing work; Drain waits for it.
// Close releases resources and must tolerate failed activation and drain.
// Work goroutines need an independent lifetime context, not the hook context.
type Participant struct {
	Name      string
	DependsOn []string
	Open      func(context.Context) error
	Activate  func(context.Context) error
	Quiesce   func(context.Context) error
	Drain     func(context.Context) error
	Close     func(context.Context) error
}

// ReportFailure marks managed readiness false and requests a nonzero process
// exit through Fx. Pass only a sanitized error; the return value reports whether
// the shutdown request could be delivered.
type ReportFailure func(error) error
