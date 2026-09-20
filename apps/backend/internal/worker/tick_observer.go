package worker

import (
	"context"
	"log/slog"
	"time"
)

// Worker names used in logs, crash reports and metrics labels.
const (
	NameSweep           = "sweep_poller"
	NameProposalExecute = "proposal_execute_poller"
	NameRedeemRecovery  = "redeem_recovery_poller"
)

// sweepTickTimeout bounds one sweep tick, matching Run.
const sweepTickTimeout = 30 * time.Second

// TickObserver records the outcome of every worker tick. A worker that is alive but whose
// ticks all fail moves no money, and only the tick result shows that.
type TickObserver interface {
	ObserveTick(worker string, duration time.Duration, err error)
}

// observeTick times tick and hands the result to observer (nil skips observation). The
// observation is deferred so a panicking tick is still counted before the supervisor
// takes over.
func observeTick(observer TickObserver, worker string, tick func() error) {
	if observer == nil {
		_ = tick()
		return
	}
	start := time.Now()
	err := errTickPanicked
	defer func() { observer.ObserveTick(worker, time.Since(start), err) }()
	err = tick()
}

type tickPanicError struct{}

func (tickPanicError) Error() string { return "tick panicked" }

var errTickPanicked error = tickPanicError{}

// RunObserved is Run with every tick reported to observer. It drives SweepPoller.Tick from
// here so the sweep poller itself stays unaware of metrics.
func RunObserved(ctx context.Context, poller *SweepPoller, interval time.Duration, wake *PollerWake, observer TickObserver) {
	if poller == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultPollInterval
	}

	logSweepPollerStarted(interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer logSweepPollerStopped()

	runTick := func() {
		observeTick(observer, NameSweep, func() error {
			tickCtx, cancel := context.WithTimeout(ctx, sweepTickTimeout)
			defer cancel()
			err := poller.Tick(tickCtx)
			if ctx.Err() != nil {
				// Shutdown cancelled the tick; that is not a failed sweep.
				return nil
			}
			if err != nil {
				slog.Error("sweep poller tick failed", "err", err)
			}
			return err
		})
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runTick()
		case <-wake.wakeChan():
			runTick()
		}
	}
}
