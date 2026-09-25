package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// countingMatchupRunner records every weekly pass and can fail them.
type countingMatchupRunner struct {
	mu    sync.Mutex
	calls int
	err   error
	ran   chan struct{}
}

func newCountingMatchupRunner(err error) *countingMatchupRunner {
	return &countingMatchupRunner{err: err, ran: make(chan struct{}, 16)}
}

func (r *countingMatchupRunner) RunWeekly(context.Context) error {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	r.ran <- struct{}{}
	return r.err
}

func (r *countingMatchupRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func runMatchupPollerUntil(t *testing.T, runner *countingMatchupRunner, interval time.Duration, passes int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunMatchupPoller(ctx, runner, interval)
	}()
	for i := 0; i < passes; i++ {
		select {
		case <-runner.ran:
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatalf("pass %d never ran", i+1)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("poller did not stop when its context ended")
	}
}

func TestRunMatchupPoller_runsOnBootBeforeTheFirstInterval(t *testing.T) {
	// Arrange: an interval far longer than the test, so only the boot pass can run.
	runner := newCountingMatchupRunner(nil)

	// Act
	runMatchupPollerUntil(t, runner, time.Hour, 1)

	// Assert
	if runner.count() != 1 {
		t.Fatalf("passes = %d, want exactly the boot pass", runner.count())
	}
}

func TestRunMatchupPoller_keepsTickingAfterAFailedPass(t *testing.T) {
	// Arrange
	runner := newCountingMatchupRunner(errors.New("database away"))

	// Act: the boot pass and two ticks, all failing.
	runMatchupPollerUntil(t, runner, 10*time.Millisecond, 3)

	// Assert
	if runner.count() < 3 {
		t.Fatalf("passes = %d, want the loop to survive failures", runner.count())
	}
}

func TestRunMatchupPoller_withoutARunnerReturnsAtOnce(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunMatchupPoller(context.Background(), nil, time.Millisecond)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a nil runner should not start a loop")
	}
}
