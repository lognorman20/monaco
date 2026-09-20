package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/monaco/monaco/packages/domain"
)

// ErrRedeemPayoutUnverified means a job is in `paying` with no payout signature on record, so
// whether a payout reached the chain cannot be established. Only jobs left behind by the old
// sign-and-send path look like this: a payout is now recorded before it is broadcast. Paying
// again could pay twice and returning the shares could hand back a claim that was already
// paid, so neither the recovery poller nor the member's next request does either.
var ErrRedeemPayoutUnverified = errors.New("redeem payout unverified")

// RedeemRecoveryOutcome is what RecoverStaleRedeemJob did with a job.
type RedeemRecoveryOutcome string

const (
	// RedeemRecoveryRolledBack: the debited share units went back to the member and the job is gone.
	RedeemRecoveryRolledBack RedeemRecoveryOutcome = "rolled_back"
	// RedeemRecoveryGone: the job settled or was cleared by someone else before recovery looked.
	RedeemRecoveryGone RedeemRecoveryOutcome = "gone"
	// RedeemRecoveryBusy: a live request holds the member's redeem lock; leave it alone.
	RedeemRecoveryBusy RedeemRecoveryOutcome = "busy"
	// RedeemRecoverySettled: the recorded payout was confirmed on chain and the job settled.
	RedeemRecoverySettled RedeemRecoveryOutcome = "settled"
	// RedeemRecoveryPending: the recorded payout can still land; look again later.
	RedeemRecoveryPending RedeemRecoveryOutcome = "pending"
)

// RecoverStaleRedeemJob resolves a redeem job that a request abandoned, without the member
// having to tap Cash out again.
//
// Jobs in `debited` or `selling` are rolled back with the same abortRedeemJob the request
// path uses: the status only becomes `paying` before the transfer is attempted, so nothing
// has left the treasury, and a sell that did happen only left the pot holding more USDC. The
// credit and the job delete share one transaction, so a second call finds no job and reports
// RedeemRecoveryGone. Recovery never pays: money only moves with the member's request behind it.
//
// Jobs in `paying` are decided by the payout signature recorded before the broadcast, read
// from the chain once: confirmed settles the job, failed or dropped rolls it back, and a
// transfer that can still land is left alone. A `paying` job with no payout on record returns
// ErrRedeemPayoutUnverified and is left untouched.
func (r *RedeemService) RecoverStaleRedeemJob(ctx context.Context, jobID string) (RedeemRecoveryOutcome, error) {
	job, found, err := r.store.GetRedeemJobByID(ctx, jobID)
	if err != nil {
		return "", err
	}
	if !found {
		return RedeemRecoveryGone, nil
	}

	release, acquired, err := r.store.TryAcquireMemberRedeemLock(ctx, job.UserID, job.GroupID)
	if err != nil {
		return "", err
	}
	if !acquired {
		logRedeemLockContended(job.UserID, job.GroupID)
		return RedeemRecoveryBusy, nil
	}
	defer release()

	// Re-read under the lock: the request that owned the job may have finished it in between.
	job, found, err = r.store.GetRedeemJobByID(ctx, jobID)
	if err != nil {
		return "", err
	}
	if !found {
		return RedeemRecoveryGone, nil
	}
	view := redeemJobFromRow(job, Position{})
	if view.Status == domain.RedeemJobSettled || view.WithdrawalID != "" {
		return RedeemRecoveryGone, nil
	}

	switch view.Status {
	case domain.RedeemJobDebited, domain.RedeemJobSelling:
		slog.Warn("redeem recovery rolling back abandoned job",
			"job_id", view.ID, "user_id", view.UserID, "group_id", view.GroupID,
			"status", string(view.Status), "share_units", view.ShareUnits, "slice_usdc", view.SliceUsdc)
		if err := r.abortRedeemJob(ctx, view); err != nil {
			return "", fmt.Errorf("roll back redeem job %s: %w", view.ID, err)
		}
		return RedeemRecoveryRolledBack, nil
	case domain.RedeemJobPaying:
		_, err := r.resolveRedeemPayout(ctx, view, false)
		switch {
		case err == nil:
			return RedeemRecoverySettled, nil
		case errors.Is(err, ErrRedeemPayoutPending):
			return RedeemRecoveryPending, nil
		case errors.Is(err, ErrRedeemPayoutDropped), errors.Is(err, ErrRedeemPayoutFailed):
			return RedeemRecoveryRolledBack, nil
		default:
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported redeem job status %q", job.Status)
	}
}
