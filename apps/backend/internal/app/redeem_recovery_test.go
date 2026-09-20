package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

type recoveryFixture struct {
	h          integrationHarness
	userID     string
	groupID    string
	payoutAddr string
}

const recoveryTotalShares = int64(2_000_000)

func newRecoveryFixture(t *testing.T, label string) recoveryFixture {
	t.Helper()
	h := integrationApp(t)
	ctx := context.Background()
	governance := NewGovernanceService(h.Store, h.Privy)
	governance.SetRedeemService(h.Redeem)

	token := privy.AccessToken(h.ISO.UniqueToken(label))
	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, label, "Recovery User")
	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, label), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	seedStakeAndHoldings(t, h, session.UserID, group.GroupID, label, recoveryTotalShares, 0)
	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 2_000_000)

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	return recoveryFixture{h: h, userID: session.UserID, groupID: group.GroupID, payoutAddr: wallet.SolanaAddress}
}

func (f recoveryFixture) shareUnits(t *testing.T) int64 {
	t.Helper()
	position, _, err := f.h.Store.GetPosition(context.Background(), f.userID, f.groupID)
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}
	return position.ShareUnits
}

// A job abandoned before the payout step has moved no money: recovery gives the burnt shares
// back exactly once, however many times it runs.
func TestRecoverStaleRedeemJob_abandonedBeforePaying_rollsBackSharesOnce(t *testing.T) {
	t.Parallel()

	for _, status := range []domain.RedeemJobStatus{domain.RedeemJobDebited, domain.RedeemJobSelling} {
		status := status
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			f := newRecoveryFixture(t, "recover-"+string(status))
			ctx := context.Background()
			const redeemShares = int64(500_000)
			jobID := wedgeRedeemJobInPaying(t, f.h, f.userID, f.groupID, f.payoutAddr, redeemShares, 500_000)
			if err := f.h.Store.UpdateRedeemJobStatus(ctx, jobID, string(status)); err != nil {
				t.Fatalf("UpdateRedeemJobStatus: %v", err)
			}

			first, err := f.h.Redeem.RecoverStaleRedeemJob(ctx, jobID)
			if err != nil {
				t.Fatalf("RecoverStaleRedeemJob: %v", err)
			}
			second, err := f.h.Redeem.RecoverStaleRedeemJob(ctx, jobID)
			if err != nil {
				t.Fatalf("RecoverStaleRedeemJob (again): %v", err)
			}

			if first != RedeemRecoveryRolledBack || second != RedeemRecoveryGone {
				t.Fatalf("outcomes = %q then %q, want rolled_back then gone", first, second)
			}
			if got := f.shareUnits(t); got != recoveryTotalShares {
				t.Fatalf("share_units = %d, want %d (credited back exactly once)", got, recoveryTotalShares)
			}
			if _, ok := privy.LastPayUSDCRequest(f.h.Privy); ok {
				t.Fatal("recovery must never pay out on its own")
			}
			active, err := f.h.Store.HasActiveRedeemJobForUser(ctx, f.userID, f.groupID)
			if err != nil || active {
				t.Fatalf("active job = %v err = %v, want the member free to cash out again", active, err)
			}
		})
	}
}

// A job stopped in `paying` may already have paid. Recovery must neither pay again nor hand
// the shares back; it reports the job and leaves every row as it found it.
func TestRecoverStaleRedeemJob_wedgedInPaying_isLeftUntouched(t *testing.T) {
	t.Parallel()

	f := newRecoveryFixture(t, "recover-paying")
	ctx := context.Background()
	const redeemShares = int64(500_000)
	jobID := wedgeRedeemJobInPaying(t, f.h, f.userID, f.groupID, f.payoutAddr, redeemShares, 500_000)

	_, err := f.h.Redeem.RecoverStaleRedeemJob(ctx, jobID)

	if !errors.Is(err, ErrRedeemPayoutUnverified) {
		t.Fatalf("err = %v, want ErrRedeemPayoutUnverified", err)
	}
	if got := f.shareUnits(t); got != recoveryTotalShares-redeemShares {
		t.Fatalf("share_units = %d, want %d (not credited back)", got, recoveryTotalShares-redeemShares)
	}
	if _, ok := privy.LastPayUSDCRequest(f.h.Privy); ok {
		t.Fatal("recovery must never pay out on its own")
	}
	stored, found, err := f.h.Store.GetRedeemJobByID(ctx, jobID)
	if err != nil || !found || stored.Status != string(domain.RedeemJobPaying) {
		t.Fatalf("job found=%v status=%q err=%v, want it still paying", found, stored.Status, err)
	}
}

// A live request holds the member's redeem lock while it drives the job.
func TestRecoverStaleRedeemJob_liveRequestHoldsLock_isSkipped(t *testing.T) {
	t.Parallel()

	f := newRecoveryFixture(t, "recover-busy")
	ctx := context.Background()
	jobID := wedgeRedeemJobInPaying(t, f.h, f.userID, f.groupID, f.payoutAddr, 500_000, 500_000)
	if err := f.h.Store.UpdateRedeemJobStatus(ctx, jobID, string(domain.RedeemJobSelling)); err != nil {
		t.Fatalf("UpdateRedeemJobStatus: %v", err)
	}
	release, acquired, err := f.h.Store.TryAcquireMemberRedeemLock(ctx, f.userID, f.groupID)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireMemberRedeemLock: acquired=%v err=%v", acquired, err)
	}
	defer release()

	outcome, err := f.h.Redeem.RecoverStaleRedeemJob(ctx, jobID)

	if err != nil || outcome != RedeemRecoveryBusy {
		t.Fatalf("outcome = %q err = %v, want busy", outcome, err)
	}
	if got := f.shareUnits(t); got != recoveryTotalShares-500_000 {
		t.Fatalf("share_units = %d, want the debit left in place", got)
	}
}

func TestRecoverStaleRedeemJob_unknownJob_isGone(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)

	outcome, err := h.Redeem.RecoverStaleRedeemJob(context.Background(), "00000000-0000-4000-8000-000000000000")

	if err != nil || outcome != RedeemRecoveryGone {
		t.Fatalf("outcome = %q err = %v, want gone", outcome, err)
	}
}

func TestListStaleActiveRedeemJobs_onlyReturnsJobsOlderThanCutoff(t *testing.T) {
	t.Parallel()

	f := newRecoveryFixture(t, "recover-list")
	ctx := context.Background()
	jobID := wedgeRedeemJobInPaying(t, f.h, f.userID, f.groupID, f.payoutAddr, 500_000, 500_000)
	contains := func(cutoff time.Time) bool {
		jobs, err := f.h.Store.ListStaleActiveRedeemJobs(ctx, cutoff, 1000)
		if err != nil {
			t.Fatalf("ListStaleActiveRedeemJobs: %v", err)
		}
		for _, job := range jobs {
			if job.ID == jobID {
				return true
			}
		}
		return false
	}

	if contains(time.Now().Add(-10 * time.Minute)) {
		t.Fatal("a job touched seconds ago was listed as stale")
	}
	if !contains(time.Now().Add(time.Minute)) {
		t.Fatal("a job older than the cutoff was not listed")
	}
}
