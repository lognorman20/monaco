package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// panickyStaleJobs panics on its first call and answers normally afterwards, like a poller
// that hits one poisoned row.
type panickyStaleJobs struct {
	calls atomic.Int32
}

func (p *panickyStaleJobs) ListStaleActiveRedeemJobs(context.Context, time.Time, int) ([]postgres.RedeemJobRow, error) {
	if p.calls.Add(1) == 1 {
		panic("index out of range on a poisoned row")
	}
	return nil, nil
}

func TestRunRedeemRecoveryPoller_tickPanic_doesNotKillTheLoop(t *testing.T) {
	// Arrange
	store := &panickyStaleJobs{}
	poller := NewRedeemRecoveryPoller(store, &fakeRedeemRecoverer{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})

	// Act: before the guard, the first tick's panic took the whole process down.
	go func() {
		defer close(stopped)
		RunRedeemRecoveryPoller(ctx, poller, 5*time.Millisecond)
	}()

	// Assert: the loop keeps ticking after the panic.
	deadline := time.After(3 * time.Second)
	for store.calls.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("poller ticked %d times; it did not survive the panic", store.calls.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("poller did not stop on cancel")
	}
	if err := telemetry.CheckPollers(context.Background()); err != nil {
		t.Fatalf("a surviving poller must report live: %v", err)
	}
}

func TestRedeemRecoveryPoller_wedgedJob_raisesMetricEveryTick(t *testing.T) {
	// Arrange
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	store := &fakeStaleRedeemJobs{jobs: []postgres.RedeemJobRow{staleJob("job-wedged", now.Add(-time.Hour))}}
	recoverer := &fakeRedeemRecoverer{err: app.ErrRedeemPayoutUnverified}
	poller := NewRedeemRecoveryPoller(store, recoverer, NewStubClock(now))

	// Act + Assert: must not panic or block with no alert webhook configured.
	poller.tick(context.Background())

	if len(recoverer.calls) != 1 {
		t.Fatalf("recover calls = %d, want 1", len(recoverer.calls))
	}
}
