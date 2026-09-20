package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// DefaultRedeemRecoveryInterval is how often abandoned redeem jobs are looked for.
const DefaultRedeemRecoveryInterval = time.Minute

// DefaultRedeemStaleAfter is how long a job may sit without a status change before it counts
// as abandoned. It must stay well above the longest a live cash out request can run (selling
// and paying both confirm on Solana inside the request).
const DefaultRedeemStaleAfter = 10 * time.Minute

const maxRedeemRecoveryBackoff = 30 * time.Minute

// RedeemJobRecoverer resolves one abandoned redeem job. *app.RedeemService implements it.
type RedeemJobRecoverer interface {
	RecoverStaleRedeemJob(ctx context.Context, jobID string) (app.RedeemRecoveryOutcome, error)
}

// StaleRedeemJobLister lists abandoned redeem jobs. *postgres.Store implements it.
type StaleRedeemJobLister interface {
	ListStaleActiveRedeemJobs(ctx context.Context, cutoff time.Time, limit int) ([]postgres.RedeemJobRow, error)
}

// RedeemRecoveryPoller finds redeem jobs that a request abandoned after burning share units
// and hands them to the redeem service, so a member is not left without shares or cash until
// they happen to tap Cash out again.
type RedeemRecoveryPoller struct {
	store      StaleRedeemJobLister
	redeem     RedeemJobRecoverer
	clock      Clock
	limit      int
	staleAfter time.Duration
	backoff    map[string]time.Time
	attempts   map[string]int
}

// NewRedeemRecoveryPoller wires redeem recovery dependencies.
func NewRedeemRecoveryPoller(store StaleRedeemJobLister, redeem RedeemJobRecoverer, clock Clock) *RedeemRecoveryPoller {
	if clock == nil {
		clock = systemClock{}
	}
	return &RedeemRecoveryPoller{
		store:      store,
		redeem:     redeem,
		clock:      clock,
		limit:      20,
		staleAfter: DefaultRedeemStaleAfter,
		backoff:    make(map[string]time.Time),
		attempts:   make(map[string]int),
	}
}

// RunRedeemRecoveryPoller ticks until ctx is cancelled.
func RunRedeemRecoveryPoller(ctx context.Context, poller *RedeemRecoveryPoller, interval time.Duration) {
	if poller == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultRedeemRecoveryInterval
	}

	slog.Info("redeem recovery poller started", "interval", interval, "stale_after", poller.staleAfter)
	telemetry.RegisterPoller(PollerRedeemRecovery, interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer slog.Info("redeem recovery poller stopped")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			telemetry.GuardTick(ctx, PollerRedeemRecovery, func() error {
				poller.tick(tickCtx)
				return nil
			})
			cancel()
		}
	}
}

func (p *RedeemRecoveryPoller) tick(ctx context.Context) {
	if p == nil || p.store == nil || p.redeem == nil {
		return
	}

	now := p.clock.Now()
	jobs, err := p.store.ListStaleActiveRedeemJobs(ctx, now.Add(-p.staleAfter), p.limit)
	if err != nil {
		slog.Error("redeem recovery poller list stale jobs failed", "err", err)
		return
	}
	p.forgetResolved(jobs)
	if len(jobs) == 0 {
		return
	}

	slog.Warn("redeem recovery poller found abandoned jobs", "count", len(jobs))
	for _, job := range jobs {
		if next, ok := p.backoff[job.ID]; ok && now.Before(next) {
			continue
		}
		attempt := p.attempts[job.ID] + 1
		attrs := []any{
			"job_id", job.ID,
			"user_id", job.UserID,
			"group_id", job.GroupID,
			"status", job.Status,
			"share_units", job.ShareUnits,
			"slice_usdc", job.SliceUsdc,
			"stale_for", now.Sub(job.UpdatedAt).Round(time.Second).String(),
			"attempt", attempt,
		}

		outcome, err := p.redeem.RecoverStaleRedeemJob(ctx, job.ID)
		switch {
		case errors.Is(err, app.ErrRedeemPayoutUnverified):
			// Nothing automatic is safe here; keep saying so until a person resolves it.
			p.recordAttempt(job.ID, attempt, now)
			slog.Error("redeem job wedged in paying: payout unverified, share units burnt, needs manual review", attrs...)
			telemetry.MoneyEvent(telemetry.EventRedeemRescue, "wedged")
			telemetry.Alert(ctx, telemetry.AlertEvent{
				Kind:     "redeem_wedged",
				Key:      "redeem_wedged:" + job.ID,
				Severity: telemetry.SeverityCritical,
				Title:    "Cash out wedged: payout unverified, needs manual review",
				Detail:   "Share units are burnt but the USDC payout could not be verified on chain. Nothing automatic is safe.",
				Fields: map[string]string{
					"job_id":     job.ID,
					"group_id":   job.GroupID,
					"user_id":    job.UserID,
					"slice_usdc": fmt.Sprint(job.SliceUsdc),
					"stale_for":  now.Sub(job.UpdatedAt).Round(time.Second).String(),
				},
			})
		case err != nil:
			p.recordAttempt(job.ID, attempt, now)
			slog.Error("redeem recovery failed", append(attrs, "err", err)...)
			telemetry.MoneyEvent(telemetry.EventRedeemRescue, telemetry.OutcomeError)
		case outcome == app.RedeemRecoveryBusy:
			slog.Info("redeem recovery skipped", append(attrs, "reason", "member redeem lock held")...)
		default:
			p.clearAttempts(job.ID)
			slog.Warn("redeem recovery resolved job", append(attrs, "outcome", string(outcome))...)
			telemetry.MoneyEvent(telemetry.EventRedeemRescue, string(outcome))
		}
	}
}

func (p *RedeemRecoveryPoller) recordAttempt(jobID string, attempt int, now time.Time) {
	p.attempts[jobID] = attempt
	delay := DefaultRedeemRecoveryInterval
	for i := 1; i < attempt && delay < maxRedeemRecoveryBackoff; i++ {
		delay *= 2
	}
	if delay > maxRedeemRecoveryBackoff {
		delay = maxRedeemRecoveryBackoff
	}
	p.backoff[jobID] = now.Add(delay)
}

func (p *RedeemRecoveryPoller) clearAttempts(jobID string) {
	delete(p.backoff, jobID)
	delete(p.attempts, jobID)
}

// forgetResolved drops bookkeeping for jobs that are no longer stale (settled on a later tap,
// or cleared by hand) so the maps cannot grow without bound.
func (p *RedeemRecoveryPoller) forgetResolved(stale []postgres.RedeemJobRow) {
	if len(p.attempts) == 0 {
		return
	}
	live := make(map[string]bool, len(stale))
	for _, job := range stale {
		live[job.ID] = true
	}
	for jobID := range p.attempts {
		if !live[jobID] {
			p.clearAttempts(jobID)
		}
	}
}
