// Package errreport sends crashes and money-path errors to an external tracker. Callers
// depend on Reporter only, so the vendor (Sentry today) can be swapped in one file.
package errreport

import (
	"context"
	"time"
)

// Level is the severity of a reported event.
type Level string

const (
	// LevelError is a handled failure: the process carried on.
	LevelError Level = "error"
	// LevelFatal is a panic: a request or a worker loop died.
	LevelFatal Level = "fatal"
)

// Event is one report. Every string in it is scrubbed of credentials before it leaves
// the process; see Scrub.
type Event struct {
	Level   Level
	Message string
	// Err is the underlying error of a handled failure, if any.
	Err error
	// Panic is the recovered value of a panic, if any.
	Panic any
	// Stack is the goroutine stack captured at the recovery point.
	Stack string
	// Tags are low-cardinality and searchable (worker, route, method).
	Tags map[string]string
	// Extra is free-form context (log attributes).
	Extra map[string]any
}

// Reporter delivers events. Report must not block the caller on the network.
type Reporter interface {
	Report(ctx context.Context, event Event)
	// Flush waits up to timeout for queued events and reports whether all were sent.
	Flush(timeout time.Duration) bool
}

// Options configures New.
type Options struct {
	// DSN enables reporting. Empty returns a Reporter that drops everything.
	DSN         string
	Environment string
	Release     string
}

// New returns the Sentry reporter when opts.DSN is set and a no-op reporter otherwise.
func New(opts Options) (Reporter, error) {
	if opts.DSN == "" {
		return Noop{}, nil
	}
	return newSentryReporter(opts)
}

// Noop drops every event. It is the reporter when SENTRY_DSN is unset.
type Noop struct{}

// Report drops the event.
func (Noop) Report(context.Context, Event) {}

// Flush has nothing to wait for.
func (Noop) Flush(time.Duration) bool { return true }
