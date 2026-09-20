package worker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/errreport"
)

type recordingReporter struct {
	mu     sync.Mutex
	events []errreport.Event
}

func (r *recordingReporter) Report(_ context.Context, event errreport.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingReporter) Flush(time.Duration) bool { return true }

func (r *recordingReporter) snapshot() []errreport.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]errreport.Event(nil), r.events...)
}

type recordingObserver struct {
	mu       sync.Mutex
	restarts map[string]int
	ticks    []error
}

func (o *recordingObserver) WorkerRestarted(worker string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.restarts == nil {
		o.restarts = map[string]int{}
	}
	o.restarts[worker]++
}

func (o *recordingObserver) ObserveTick(_ string, _ time.Duration, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.ticks = append(o.ticks, err)
}

func (o *recordingObserver) restartCount(worker string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.restarts[worker]
}

func testSupervisor(reporter errreport.Reporter, observer RestartObserver) Supervisor {
	return Supervisor{
		Name:       "test_worker",
		Reporter:   reporter,
		Observer:   observer,
		MinBackoff: time.Millisecond,
		MaxBackoff: 4 * time.Millisecond,
	}
}

func TestSupervisor_workerPanics_reportsAndRestartsLoop(t *testing.T) {
	// Arrange
	reporter := &recordingReporter{}
	observer := &recordingObserver{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var runs atomic.Int32
	recovered := make(chan struct{})
	run := func(ctx context.Context) {
		if runs.Add(1) == 1 {
			var deposits map[string]int
			deposits["d1"] = 1 // nil map write: the kind of bug that used to kill the API
		}
		close(recovered)
		<-ctx.Done()
	}
	done := make(chan struct{})

	// Act
	go func() {
		defer close(done)
		testSupervisor(reporter, observer).Run(ctx, run)
	}()

	// Assert
	select {
	case <-recovered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker was not restarted after the panic")
	}
	events := reporter.snapshot()
	if len(events) != 1 {
		t.Fatalf("reported %d events, want 1", len(events))
	}
	event := events[0]
	if event.Level != errreport.LevelFatal || event.Tags["worker"] != "test_worker" {
		t.Fatalf("event = %+v", event)
	}
	if !strings.Contains(event.Stack, "supervisor_test.go") {
		t.Fatalf("stack does not point at the panicking frame:\n%s", event.Stack)
	}
	if got := observer.restartCount("test_worker"); got != 1 {
		t.Fatalf("restarts = %d, want 1", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not return after cancellation")
	}
}

func TestSupervisor_workerKeepsPanicking_backsOffUpToMax(t *testing.T) {
	// Arrange
	reporter := &recordingReporter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var starts []time.Time
	run := func(context.Context) {
		mu.Lock()
		starts = append(starts, time.Now())
		count := len(starts)
		mu.Unlock()
		if count == 5 {
			cancel()
			return
		}
		panic(errors.New("poison row"))
	}
	supervisor := Supervisor{Name: "w", Reporter: reporter, MinBackoff: 10 * time.Millisecond, MaxBackoff: 40 * time.Millisecond}

	// Act
	supervisor.Run(ctx, run)

	// Assert: waits are 10, 20, 40, 40 (capped), so restarts never spin.
	if len(starts) != 5 {
		t.Fatalf("runs = %d, want 5", len(starts))
	}
	for i, wantMs := range []time.Duration{10, 20, 40, 40} {
		if gap := starts[i+1].Sub(starts[i]); gap < wantMs*time.Millisecond {
			t.Fatalf("restart %d came after %v, want at least %dms", i+1, gap, int64(wantMs))
		}
	}
	if total := starts[4].Sub(starts[0]); total > 2*time.Second {
		t.Fatalf("backoff not capped: 4 restarts took %v", total)
	}
	if len(reporter.snapshot()) != 4 {
		t.Fatalf("reported %d panics, want 4", len(reporter.snapshot()))
	}
}

func TestSupervisor_cancelledDuringBackoff_returnsWithoutRestart(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(context.Background())
	var runs atomic.Int32
	run := func(context.Context) {
		runs.Add(1)
		panic("boom")
	}
	supervisor := Supervisor{Name: "w", MinBackoff: time.Hour, MaxBackoff: time.Hour}
	done := make(chan struct{})

	// Act
	go func() {
		defer close(done)
		supervisor.Run(ctx, run)
	}()
	for runs.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()

	// Assert
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown blocked on the restart backoff")
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("runs = %d, want 1", got)
	}
}

func TestSupervisor_workerReturnsEarly_isRestarted(t *testing.T) {
	// Arrange
	observer := &recordingObserver{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var runs atomic.Int32
	run := func(ctx context.Context) {
		if runs.Add(1) < 3 {
			return
		}
		cancel()
	}

	// Act
	testSupervisor(nil, observer).Run(ctx, run)

	// Assert
	if got := runs.Load(); got != 3 {
		t.Fatalf("runs = %d, want 3", got)
	}
	if got := observer.restartCount("test_worker"); got != 2 {
		t.Fatalf("restarts = %d, want 2", got)
	}
}

func (o *recordingObserver) tickCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.ticks)
}

func TestRunObserved_wakeTicksSweepPoller_observesItAndStopsOnCancel(t *testing.T) {
	// Arrange
	poller, _, _ := setupPoller(t)
	observer := &recordingObserver{}
	wake := NewPollerWake()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	// Act
	go func() {
		defer close(done)
		RunObserved(ctx, poller, time.Hour, wake, observer)
	}()
	wake.Notify()

	// Assert
	deadline := time.Now().Add(10 * time.Second)
	for observer.tickCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("wake did not produce an observed tick")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := observer.ticks[0]; err != nil {
		t.Fatalf("tick err = %v, want a clean tick on an empty queue", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunObserved did not stop on cancellation")
	}
}

func TestObserveTick_recordsSuccessFailureAndPanic(t *testing.T) {
	// Arrange
	observer := &recordingObserver{}
	failure := errors.New("rpc down")

	// Act
	observeTick(observer, "w", func() error { return nil })
	observeTick(observer, "w", func() error { return failure })
	func() {
		defer func() { _ = recover() }()
		observeTick(observer, "w", func() error { panic("boom") })
	}()

	// Assert
	if len(observer.ticks) != 3 {
		t.Fatalf("ticks = %d, want 3", len(observer.ticks))
	}
	if observer.ticks[0] != nil || !errors.Is(observer.ticks[1], failure) {
		t.Fatalf("ticks = %v", observer.ticks)
	}
	if observer.ticks[2] == nil {
		t.Fatal("a panicking tick was recorded as a success")
	}
}
