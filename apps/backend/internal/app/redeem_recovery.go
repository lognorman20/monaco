package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/monaco/monaco/packages/domain"
)

// ErrRedeemPayoutUnverified means a job stopped in `paying` with no withdrawal recorded, so
// whether the payout reached the chain is unknown. PayUSDC signs and sends in one Privy call:
// an error from it (a timeout, a dropped response) does not prove the transfer did not land,
// and neither does a failure while recording it afterwards. Paying again could pay twice and
// returning the shares could hand back a claim that was already paid, so neither is automatic.
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
// Jobs in `paying` return ErrRedeemPayoutUnverified and are left untouched.
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
		return "", ErrRedeemPayoutUnverified
	default:
		return "", fmt.Errorf("unsupported redeem job status %q", job.Status)
	}
}
