package worker

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/errreport"
)

const (
	// DefaultSupervisorMinBackoff is the wait before the first restart of a crashed worker.
	DefaultSupervisorMinBackoff = time.Second
	// DefaultSupervisorMaxBackoff caps the wait for a worker that keeps crashing, so a
	// poison row slows the loop down instead of spinning the process and the alert channel.
	DefaultSupervisorMaxBackoff = time.Minute
	// DefaultSupervisorHealthyAfter is how long a run must last for the next crash to be
	// treated as a fresh incident (backoff back to the minimum).
	DefaultSupervisorHealthyAfter = 5 * time.Minute

	supervisorStackBytes = 16 << 10
)

// RestartObserver is told every time a supervised worker is restarted.
type RestartObserver interface {
	WorkerRestarted(worker string)
}

// Supervisor keeps one background worker loop alive. An unrecovered panic in a goroutine
// kills the whole process, which for this API means dying between a Solana submit and the
// database write that records it. The supervisor recovers the panic, logs the stack,
// reports it, and starts the loop again after a backoff.
type Supervisor struct {
	// Name identifies the worker in logs, reports and metrics.
	Name string
	// Reporter receives each panic; nil drops them.
	Reporter errreport.Reporter
	// Observer counts restarts; nil skips it.
	Observer RestartObserver

	// MinBackoff, MaxBackoff and HealthyAfter default to the DefaultSupervisor* values.
	MinBackoff   time.Duration
	MaxBackoff   time.Duration
	HealthyAfter time.Duration
}

// Run calls run until ctx is cancelled. run is expected to block until then; a panic or an
// early return restarts it. Run returns only after ctx is cancelled and run has unwound,
// so shutdown can wait on it.
func (s Supervisor) Run(ctx context.Context, run func(ctx context.Context)) {
	minBackoff, maxBackoff, healthyAfter := s.MinBackoff, s.MaxBackoff, s.HealthyAfter
	if minBackoff <= 0 {
		minBackoff = DefaultSupervisorMinBackoff
	}
	if maxBackoff < minBackoff {
		maxBackoff = max(DefaultSupervisorMaxBackoff, minBackoff)
	}
	if healthyAfter <= 0 {
		healthyAfter = DefaultSupervisorHealthyAfter
	}

	backoff := minBackoff
	for {
		started := time.Now()
		panicked := s.runOnce(ctx, run)
		if ctx.Err() != nil {
			return
		}
		if !panicked {
			// The loops only return on cancellation; anything else would silently stop
			// sweeps or cash-out recovery, so it is as loud as a crash.
			slog.ErrorContext(ctx, "worker exited unexpectedly", "worker", s.Name)
		}
		if time.Since(started) >= healthyAfter {
			backoff = minBackoff
		}

		slog.WarnContext(ctx, "worker restarting", "worker", s.Name, "backoff", backoff.String())
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, maxBackoff)
		if s.Observer != nil {
			s.Observer.WorkerRestarted(s.Name)
		}
	}
}

// runOnce runs the loop once and reports whether it ended in a panic.
func (s Supervisor) runOnce(ctx context.Context, run func(ctx context.Context)) (panicked bool) {
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		panicked = true

		stack := make([]byte, supervisorStackBytes)
		stack = stack[:runtime.Stack(stack, false)]
		slog.ErrorContext(ctx, "worker panic",
			"worker", s.Name,
			errreport.PanicAttrKey, fmt.Sprintf("%v", rec),
			"stack", string(stack),
		)
		if s.Reporter != nil {
			s.Reporter.Report(ctx, errreport.Event{
				Level:   errreport.LevelFatal,
				Message: "worker panic",
				Panic:   rec,
				Stack:   string(stack),
				Tags:    map[string]string{"worker": s.Name},
			})
		}
	}()
	run(ctx)
	return false
}
