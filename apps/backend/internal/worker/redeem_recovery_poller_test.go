package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

type fakeStaleRedeemJobs struct {
	jobs    []postgres.RedeemJobRow
	err     error
	cutoffs []time.Time
}

func (f *fakeStaleRedeemJobs) ListStaleActiveRedeemJobs(_ context.Context, cutoff time.Time, _ int) ([]postgres.RedeemJobRow, error) {
	f.cutoffs = append(f.cutoffs, cutoff)
	return f.jobs, f.err
}

type fakeRedeemRecoverer struct {
	outcome app.RedeemRecoveryOutcome
	err     error
	calls   []string
}

func (f *fakeRedeemRecoverer) RecoverStaleRedeemJob(_ context.Context, jobID string) (app.RedeemRecoveryOutcome, error) {
	f.calls = append(f.calls, jobID)
	return f.outcome, f.err
}

func staleJob(id string, updatedAt time.Time) postgres.RedeemJobRow {
	return postgres.RedeemJobRow{ID: id, UserID: "u1", GroupID: "g1", Status: postgres.RedeemJobStatusPaying, ShareUnits: 5, SliceUsdc: 5, UpdatedAt: updatedAt}
}

func TestRedeemRecoveryPoller_listsJobsOlderThanStaleAfter(t *testing.T) {
	// Arrange
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	store := &fakeStaleRedeemJobs{}
	poller := NewRedeemRecoveryPoller(store, &fakeRedeemRecoverer{}, NewStubClock(now))

	// Act
	poller.tick(context.Background())

	// Assert
	if len(store.cutoffs) != 1 || !store.cutoffs[0].Equal(now.Add(-DefaultRedeemStaleAfter)) {
		t.Fatalf("cutoffs = %v, want one at now-%s", store.cutoffs, DefaultRedeemStaleAfter)
	}
}

func TestRedeemRecoveryPoller_unverifiedPayout_backsOffBetweenAlarms(t *testing.T) {
	// Arrange
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	clock := NewStubClock(now)
	store := &fakeStaleRedeemJobs{jobs: []postgres.RedeemJobRow{staleJob("job-1", now.Add(-time.Hour))}}
	redeem := &fakeRedeemRecoverer{err: app.ErrRedeemPayoutUnverified}
	poller := NewRedeemRecoveryPoller(store, redeem, clock)

	// Act: first tick tries, the next tick inside the backoff window does not, a later one does.
	poller.tick(context.Background())
	clock.now = now.Add(30 * time.Second)
	poller.tick(context.Background())
	clock.now = now.Add(DefaultRedeemRecoveryInterval)
	poller.tick(context.Background())
	clock.now = now.Add(DefaultRedeemRecoveryInterval + 90*time.Second)
	poller.tick(context.Background())

	// Assert
	if len(redeem.calls) != 2 {
		t.Fatalf("recover calls = %d, want 2 (second delay doubles to 2m)", len(redeem.calls))
	}
	if poller.attempts["job-1"] != 2 {
		t.Fatalf("attempts = %d, want 2", poller.attempts["job-1"])
	}
}

func TestRedeemRecoveryPoller_backoffIsCapped(t *testing.T) {
	// Arrange
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	poller := NewRedeemRecoveryPoller(&fakeStaleRedeemJobs{}, &fakeRedeemRecoverer{}, NewStubClock(now))

	// Act
	poller.recordAttempt("job-1", 40, now)

	// Assert
	if got := poller.backoff["job-1"].Sub(now); got != maxRedeemRecoveryBackoff {
		t.Fatalf("backoff = %s, want capped at %s", got, maxRedeemRecoveryBackoff)
	}
}

func TestRedeemRecoveryPoller_resolvedJob_clearsBookkeeping(t *testing.T) {
	// Arrange
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	clock := NewStubClock(now)
	store := &fakeStaleRedeemJobs{jobs: []postgres.RedeemJobRow{staleJob("job-1", now.Add(-time.Hour))}}
	redeem := &fakeRedeemRecoverer{err: errors.New("db down")}
	poller := NewRedeemRecoveryPoller(store, redeem, clock)
	poller.tick(context.Background())

	// Act: the job settles on a later tap and drops out of the stale list.
	store.jobs = nil
	poller.tick(context.Background())

	// Assert
	if len(poller.attempts) != 0 || len(poller.backoff) != 0 {
		t.Fatalf("attempts = %v backoff = %v, want both empty", poller.attempts, poller.backoff)
	}
}

func TestRedeemRecoveryPoller_busyJob_isRetriedNextTickWithoutBackoff(t *testing.T) {
	// Arrange
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	store := &fakeStaleRedeemJobs{jobs: []postgres.RedeemJobRow{staleJob("job-1", now.Add(-time.Hour))}}
	redeem := &fakeRedeemRecoverer{outcome: app.RedeemRecoveryBusy}
	poller := NewRedeemRecoveryPoller(store, redeem, NewStubClock(now))

	// Act
	poller.tick(context.Background())
	poller.tick(context.Background())

	// Assert
	if len(redeem.calls) != 2 {
		t.Fatalf("recover calls = %d, want 2", len(redeem.calls))
	}
}

func TestRedeemRecoveryPoller_pendingPayout_isRecheckedNextTickWithoutBackoff(t *testing.T) {
	// Arrange
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	store := &fakeStaleRedeemJobs{jobs: []postgres.RedeemJobRow{staleJob("job-1", now.Add(-time.Hour))}}
	redeem := &fakeRedeemRecoverer{outcome: app.RedeemRecoveryPending}
	poller := NewRedeemRecoveryPoller(store, redeem, NewStubClock(now))

	// Act
	poller.tick(context.Background())
	poller.tick(context.Background())

	// Assert
	if len(redeem.calls) != 2 || len(poller.backoff) != 0 {
		t.Fatalf("recover calls = %d backoff = %v, want 2 calls and no backoff", len(redeem.calls), poller.backoff)
	}
}

func TestRedeemRecoveryPoller_listFailure_doesNotRecover(t *testing.T) {
	// Arrange
	redeem := &fakeRedeemRecoverer{}
	poller := NewRedeemRecoveryPoller(&fakeStaleRedeemJobs{err: errors.New("db down")}, redeem, nil)

	// Act
	poller.tick(context.Background())

	// Assert
	if len(redeem.calls) != 0 {
		t.Fatalf("recover calls = %d, want 0", len(redeem.calls))
	}
}
