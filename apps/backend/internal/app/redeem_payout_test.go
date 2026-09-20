package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/packages/domain"
)

// payoutFixture is one member holding every share of a USDC-only pot worth 2 USDC.
type payoutFixture struct {
	recoveryFixture
	token    string
	treasury privy.TreasuryRef
}

func newPayoutFixture(t *testing.T, label string) payoutFixture {
	t.Helper()
	f := newRecoveryFixture(t, label)
	treasury, err := f.h.Privy.EnsureTreasury(context.Background(), privy.GroupID(f.groupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	// Tests that wait on the chain must not wait long.
	f.h.Redeem.payoutConfirmTimeout = 150 * time.Millisecond
	f.h.Redeem.payoutPollInterval = 5 * time.Millisecond
	return payoutFixture{recoveryFixture: f, token: f.h.ISO.UniqueToken(label), treasury: treasury}
}

func (f payoutFixture) cashOut(shareUnits int64) (RedeemJobView, error) {
	return f.h.Redeem.WithdrawToBalance(context.Background(), WithdrawToBalanceRequest{
		AccessToken:       f.token,
		GroupID:           f.groupID,
		ShareAmountMicros: &shareUnits,
	})
}

func (f payoutFixture) memberUsdc(t *testing.T) int64 {
	t.Helper()
	balance, err := f.h.Privy.MemberUSDCBalance(context.Background(), f.payoutAddr)
	if err != nil {
		t.Fatalf("MemberUSDCBalance: %v", err)
	}
	return balance
}

func (f payoutFixture) confirmedWithdrawals(t *testing.T) []postgres.WithdrawalRow {
	t.Helper()
	rows, err := f.h.Store.ListWithdrawalsByGroupID(context.Background(), f.groupID)
	if err != nil {
		t.Fatalf("ListWithdrawalsByGroupID: %v", err)
	}
	var confirmed []postgres.WithdrawalRow
	for _, row := range rows {
		if row.Status == "confirmed" {
			confirmed = append(confirmed, row)
		}
	}
	return confirmed
}

func (f payoutFixture) activeJob(t *testing.T) (postgres.RedeemJobRow, bool) {
	t.Helper()
	job, found, err := f.h.Store.GetActiveRedeemJobForUser(context.Background(), f.userID, f.groupID)
	if err != nil {
		t.Fatalf("GetActiveRedeemJobForUser: %v", err)
	}
	return job, found
}

func (f payoutFixture) payoutFor(t *testing.T, jobID string) postgres.RedeemPayoutRow {
	t.Helper()
	payout, found, err := f.h.Store.GetRedeemPayoutByJobID(context.Background(), jobID)
	if err != nil || !found {
		t.Fatalf("GetRedeemPayoutByJobID(%s): found=%v err=%v", jobID, found, err)
	}
	return payout
}

// assertPaidExactlyOnce pins the whole point of this file: one transfer on chain, one
// confirmed withdrawal carrying its signature, and the member holding exactly that USDC.
func (f payoutFixture) assertPaidExactlyOnce(t *testing.T, amount int64) {
	t.Helper()
	if got := privy.LandedPayoutCount(f.h.Privy); got != 1 {
		t.Fatalf("transfers landed on chain = %d, want exactly 1", got)
	}
	if got := f.memberUsdc(t); got != amount {
		t.Fatalf("member usdc = %d, want %d", got, amount)
	}
	withdrawals := f.confirmedWithdrawals(t)
	if len(withdrawals) != 1 || withdrawals[0].Amount != amount {
		t.Fatalf("confirmed withdrawals = %+v, want one of %d", withdrawals, amount)
	}
}

// signAndRecordPayout leaves a job exactly where a request that died right after recording
// its payout intent leaves it: signed, on record, `paying`, and not broadcast.
func (f payoutFixture) signAndRecordPayout(t *testing.T, shareUnits, amount int64) (string, privy.PreparedPayout) {
	t.Helper()
	ctx := context.Background()
	jobID := wedgeRedeemJobInSelling(t, f.h, f.userID, f.groupID, f.payoutAddr, shareUnits, amount)
	prepared, err := f.h.Privy.PrepareUSDCPayout(ctx, privy.PayUSDCRequest{
		TreasuryPrivyWalletID: f.treasury.PrivyWalletID,
		TreasuryAddress:       f.treasury.SolanaAddress,
		ToAddress:             f.payoutAddr,
		Amount:                amount,
		GroupID:               f.groupID,
		UserID:                f.userID,
	})
	if err != nil {
		t.Fatalf("PrepareUSDCPayout: %v", err)
	}
	if _, err := f.h.Store.RecordRedeemPayoutIntent(ctx, postgres.RedeemPayoutIntent{
		RedeemJobID:          jobID,
		UserID:               f.userID,
		GroupID:              f.groupID,
		Amount:               amount,
		ToAddress:            f.payoutAddr,
		TxSignature:          prepared.TxSignature,
		SignedTx:             prepared.SignedTransaction,
		LastValidBlockHeight: prepared.LastValidBlockHeight,
	}); err != nil {
		t.Fatalf("RecordRedeemPayoutIntent: %v", err)
	}
	return jobID, prepared
}

// The audit's double pay: the send call errors (a Privy or RPC timeout) although the transfer
// reached the chain, and the status RPC is down too, so the request cannot learn its fate.
// The next tap must settle that same transfer, not sign another one.
func TestWithdrawToBalance_broadcastErrorsButTransferLanded_retrySettlesWithoutPayingAgain(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-send-error")
	const redeemShares = int64(500_000)
	privy.SetPayoutBehavior(f.h.Privy, privy.FakePayoutBehavior{
		BroadcastErr: errors.New("privy: context deadline exceeded"),
		StatusErr:    errors.New("solana rpc: 503"),
	})

	_, err := f.cashOut(redeemShares)

	if !errors.Is(err, ErrRedeemPayoutPending) {
		t.Fatalf("first tap err = %v, want ErrRedeemPayoutPending", err)
	}
	job, found := f.activeJob(t)
	if !found || job.Status != string(domain.RedeemJobPaying) {
		t.Fatalf("job found=%v status=%q, want it parked in paying", found, job.Status)
	}
	if got := f.shareUnits(t); got != recoveryTotalShares-redeemShares {
		t.Fatalf("share_units = %d, want %d (not handed back while the transfer may have landed)", got, recoveryTotalShares-redeemShares)
	}
	if len(f.confirmedWithdrawals(t)) != 0 {
		t.Fatal("nothing may be recorded as paid before the chain confirms it")
	}

	privy.SetPayoutBehavior(f.h.Privy, privy.FakePayoutBehavior{})
	settled, err := f.cashOut(redeemShares)

	if err != nil {
		t.Fatalf("second tap: %v", err)
	}
	if settled.ID != job.ID || settled.Status != domain.RedeemJobSettled {
		t.Fatalf("second tap job = %s %q, want the first job %s settled", settled.ID, settled.Status, job.ID)
	}
	f.assertPaidExactlyOnce(t, redeemShares)
	if got := f.confirmedWithdrawals(t)[0].TxSignature.String; got != f.payoutFor(t, job.ID).TxSignature {
		t.Fatalf("withdrawal signature = %q, want the signature recorded before the broadcast", got)
	}
	if got := f.shareUnits(t); got != recoveryTotalShares-redeemShares {
		t.Fatalf("share_units = %d, want %d (burnt once)", got, recoveryTotalShares-redeemShares)
	}
}

// The balance RPC failing after the transfer confirmed used to strand the job in `paying`
// with nothing recorded, and the next tap paid again.
func TestWithdrawToBalance_balanceRPCFailsAfterPayoutConfirmed_retrySettlesWithoutPayingAgain(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-balance-rpc")
	const redeemShares = int64(500_000)
	flaky := &treasuryBalanceOutagePrivy{Client: f.h.Privy}
	redeem := NewRedeemService(f.h.Store, flaky, f.h.Pyth, f.h.Jupiter, f.h.Swap, NewFakePrivyTreasurySigner())
	request := WithdrawToBalanceRequest{AccessToken: f.token, GroupID: f.groupID, ShareAmountMicros: ptrInt64(redeemShares)}
	flaky.failOncePaid.Store(true)

	_, err := redeem.WithdrawToBalance(context.Background(), request)

	if err == nil || !errors.Is(err, errTreasuryBalanceOutage) {
		t.Fatalf("first tap err = %v, want the balance outage", err)
	}
	job, found := f.activeJob(t)
	if !found || job.Status != string(domain.RedeemJobPaying) {
		t.Fatalf("job found=%v status=%q, want it parked in paying", found, job.Status)
	}

	flaky.failOncePaid.Store(false)
	settled, err := redeem.WithdrawToBalance(context.Background(), request)

	if err != nil {
		t.Fatalf("second tap: %v", err)
	}
	if settled.ID != job.ID || settled.Status != domain.RedeemJobSettled {
		t.Fatalf("second tap job = %s %q, want the first job %s settled", settled.ID, settled.Status, job.ID)
	}
	f.assertPaidExactlyOnce(t, redeemShares)
}

// A transfer that never reaches the chain is only given up on once its blockhash has expired.
// Until then the shares stay burnt; afterwards they come back and the same tap pays afresh.
func TestWithdrawToBalance_transferDropped_sharesReturnOnlyAfterBlockhashExpires(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-dropped")
	const redeemShares = int64(500_000)
	privy.SetPayoutBehavior(f.h.Privy, privy.FakePayoutBehavior{BroadcastLost: true})

	_, err := f.cashOut(redeemShares)

	if !errors.Is(err, ErrRedeemPayoutPending) {
		t.Fatalf("first tap err = %v, want ErrRedeemPayoutPending while the blockhash is still valid", err)
	}
	firstJob, found := f.activeJob(t)
	if !found {
		t.Fatal("want the job kept while its transfer can still land")
	}
	if got := f.shareUnits(t); got != recoveryTotalShares-redeemShares {
		t.Fatalf("share_units = %d, want %d", got, recoveryTotalShares-redeemShares)
	}

	privy.ExpirePendingPayouts(f.h.Privy)
	privy.SetPayoutBehavior(f.h.Privy, privy.FakePayoutBehavior{})
	settled, err := f.cashOut(redeemShares)

	if err != nil {
		t.Fatalf("second tap: %v", err)
	}
	if settled.Status != domain.RedeemJobSettled || settled.ID == firstJob.ID {
		t.Fatalf("second tap job = %s %q, want a fresh settled job", settled.ID, settled.Status)
	}
	if got := f.payoutFor(t, firstJob.ID).Status; got != postgres.RedeemPayoutStatusDropped {
		t.Fatalf("first payout status = %q, want dropped", got)
	}
	f.assertPaidExactlyOnce(t, redeemShares)
	if got := f.shareUnits(t); got != recoveryTotalShares-redeemShares {
		t.Fatalf("share_units = %d, want %d (returned once, burnt once)", got, recoveryTotalShares-redeemShares)
	}
}

// A transfer that expires while the request is still waiting is reported to that request.
func TestWithdrawToBalance_transferExpiresDuringRequest_returnsSharesAndSaysSo(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-dropped-live")
	f.h.Redeem.payoutConfirmTimeout = 10 * time.Second
	const redeemShares = int64(500_000)
	privy.SetPayoutBehavior(f.h.Privy, privy.FakePayoutBehavior{BroadcastLost: true})

	errCh := make(chan error, 1)
	go func() {
		_, err := f.cashOut(redeemShares)
		errCh <- err
	}()
	waitForCondition(t, "payout intent on record", func() bool {
		job, found := f.activeJob(t)
		return found && job.Status == string(domain.RedeemJobPaying)
	})
	privy.ExpirePendingPayouts(f.h.Privy)

	err := <-errCh

	if !errors.Is(err, ErrRedeemPayoutDropped) {
		t.Fatalf("err = %v, want ErrRedeemPayoutDropped", err)
	}
	if got := f.shareUnits(t); got != recoveryTotalShares {
		t.Fatalf("share_units = %d, want %d (returned)", got, recoveryTotalShares)
	}
	if _, found := f.activeJob(t); found {
		t.Fatal("want no active job once the transfer can never land")
	}
	if got := privy.LandedPayoutCount(f.h.Privy); got != 0 {
		t.Fatalf("transfers landed = %d, want 0", got)
	}
}

// A transfer the chain rejected moved no USDC: the member gets their shares back and an
// error, never a settled withdrawal.
func TestWithdrawToBalance_transferFailsOnChain_returnsSharesAndRecordsNoWithdrawal(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-failed")
	const redeemShares = int64(500_000)
	privy.SetPayoutBehavior(f.h.Privy, privy.FakePayoutBehavior{FailOnChain: true})

	_, err := f.cashOut(redeemShares)

	if !errors.Is(err, ErrRedeemPayoutFailed) {
		t.Fatalf("err = %v, want ErrRedeemPayoutFailed", err)
	}
	if got := f.shareUnits(t); got != recoveryTotalShares {
		t.Fatalf("share_units = %d, want %d (returned)", got, recoveryTotalShares)
	}
	if _, found := f.activeJob(t); found {
		t.Fatal("want no active job after an on-chain failure")
	}
	if len(f.confirmedWithdrawals(t)) != 0 || f.memberUsdc(t) != 0 {
		t.Fatal("a failed transfer must not be recorded or counted as paid")
	}

	privy.SetPayoutBehavior(f.h.Privy, privy.FakePayoutBehavior{})
	if _, err := f.cashOut(redeemShares); err != nil {
		t.Fatalf("cash out after the failure: %v", err)
	}
	f.assertPaidExactlyOnce(t, redeemShares)
}

// Signing moves no money, so a Privy error before the intent is recorded rolls back cleanly.
func TestWithdrawToBalance_signingFails_returnsSharesAndLeavesNothingInFlight(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-sign-error")
	const redeemShares = int64(500_000)
	signErr := errors.New("privy: sign transaction status 502")
	privy.SetPayoutBehavior(f.h.Privy, privy.FakePayoutBehavior{PrepareErr: signErr})

	_, err := f.cashOut(redeemShares)

	if !errors.Is(err, signErr) {
		t.Fatalf("err = %v, want the signing error", err)
	}
	if got := f.shareUnits(t); got != recoveryTotalShares {
		t.Fatalf("share_units = %d, want %d (returned)", got, recoveryTotalShares)
	}
	if _, found := f.activeJob(t); found {
		t.Fatal("want no active job when nothing was signed")
	}

	privy.SetPayoutBehavior(f.h.Privy, privy.FakePayoutBehavior{})
	if _, err := f.cashOut(redeemShares); err != nil {
		t.Fatalf("cash out after the signing error: %v", err)
	}
	f.assertPaidExactlyOnce(t, redeemShares)
}

// The process dies after the intent commits and before the broadcast. The signed transfer is
// on record, so the next tap sends that one instead of signing another.
func TestWithdrawToBalance_crashAfterIntentBeforeBroadcast_nextTapSendsTheRecordedTransfer(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-crash-unsent")
	const redeemShares = int64(500_000)
	jobID, prepared := f.signAndRecordPayout(t, redeemShares, redeemShares)

	settled, err := f.cashOut(redeemShares)

	if err != nil {
		t.Fatalf("WithdrawToBalance: %v", err)
	}
	if settled.ID != jobID || settled.Status != domain.RedeemJobSettled {
		t.Fatalf("job = %s %q, want the crashed job %s settled", settled.ID, settled.Status, jobID)
	}
	f.assertPaidExactlyOnce(t, redeemShares)
	if got := f.confirmedWithdrawals(t)[0].TxSignature.String; got != prepared.TxSignature {
		t.Fatalf("withdrawal signature = %q, want the recorded %q", got, prepared.TxSignature)
	}
}

// The process dies after the broadcast and before anything is settled: the old flow's blind
// spot. The signature was recorded first, so both the next tap and the poller can settle it.
func TestRedeemPayout_crashAfterBroadcast_isSettledFromTheRecordedSignature(t *testing.T) {
	t.Parallel()

	const redeemShares = int64(500_000)

	t.Run("by the member's next tap", func(t *testing.T) {
		t.Parallel()
		f := newPayoutFixture(t, "payout-crash-sent-tap")
		jobID, prepared := f.signAndRecordPayout(t, redeemShares, redeemShares)
		if err := f.h.Privy.BroadcastUSDCPayout(context.Background(), prepared); err != nil {
			t.Fatalf("BroadcastUSDCPayout: %v", err)
		}

		settled, err := f.cashOut(redeemShares)

		if err != nil || settled.ID != jobID || settled.Status != domain.RedeemJobSettled {
			t.Fatalf("job = %s %q err = %v, want %s settled", settled.ID, settled.Status, err, jobID)
		}
		f.assertPaidExactlyOnce(t, redeemShares)
	})

	t.Run("by the recovery poller", func(t *testing.T) {
		t.Parallel()
		f := newPayoutFixture(t, "payout-crash-sent-poller")
		jobID, prepared := f.signAndRecordPayout(t, redeemShares, redeemShares)
		if err := f.h.Privy.BroadcastUSDCPayout(context.Background(), prepared); err != nil {
			t.Fatalf("BroadcastUSDCPayout: %v", err)
		}

		first, err := f.h.Redeem.RecoverStaleRedeemJob(context.Background(), jobID)
		if err != nil {
			t.Fatalf("RecoverStaleRedeemJob: %v", err)
		}
		second, err := f.h.Redeem.RecoverStaleRedeemJob(context.Background(), jobID)
		if err != nil {
			t.Fatalf("RecoverStaleRedeemJob (again): %v", err)
		}

		if first != RedeemRecoverySettled || second != RedeemRecoveryGone {
			t.Fatalf("outcomes = %q then %q, want settled then gone", first, second)
		}
		f.assertPaidExactlyOnce(t, redeemShares)
	})
}

// The poller only reads the chain. It never broadcasts, waits while the transfer can still
// land, and returns the shares once it cannot.
func TestRecoverStaleRedeemJob_unsentPayout_waitsThenRollsBackOnceExpired(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-poller-unsent")
	ctx := context.Background()
	const redeemShares = int64(500_000)
	jobID, _ := f.signAndRecordPayout(t, redeemShares, redeemShares)

	pending, err := f.h.Redeem.RecoverStaleRedeemJob(ctx, jobID)
	if err != nil {
		t.Fatalf("RecoverStaleRedeemJob: %v", err)
	}
	if pending != RedeemRecoveryPending || f.shareUnits(t) != recoveryTotalShares-redeemShares {
		t.Fatalf("outcome = %q share_units = %d, want pending with the shares still burnt", pending, f.shareUnits(t))
	}

	privy.ExpirePendingPayouts(f.h.Privy)
	rolledBack, err := f.h.Redeem.RecoverStaleRedeemJob(ctx, jobID)
	if err != nil {
		t.Fatalf("RecoverStaleRedeemJob after expiry: %v", err)
	}

	if rolledBack != RedeemRecoveryRolledBack {
		t.Fatalf("outcome = %q, want rolled_back", rolledBack)
	}
	if got := f.shareUnits(t); got != recoveryTotalShares {
		t.Fatalf("share_units = %d, want %d (returned exactly once)", got, recoveryTotalShares)
	}
	if got := privy.LandedPayoutCount(f.h.Privy); got != 0 {
		t.Fatalf("transfers landed = %d, want 0: recovery must never pay", got)
	}
	if got := f.payoutFor(t, jobID).Status; got != postgres.RedeemPayoutStatusDropped {
		t.Fatalf("payout status = %q, want dropped", got)
	}
}

// An RPC outage tells the poller nothing, so it must change nothing.
func TestRecoverStaleRedeemJob_statusRPCDown_changesNothing(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-poller-rpc-down")
	const redeemShares = int64(500_000)
	jobID, _ := f.signAndRecordPayout(t, redeemShares, redeemShares)
	rpcErr := errors.New("solana rpc: 503")
	privy.SetPayoutBehavior(f.h.Privy, privy.FakePayoutBehavior{StatusErr: rpcErr})

	_, err := f.h.Redeem.RecoverStaleRedeemJob(context.Background(), jobID)

	if !errors.Is(err, rpcErr) {
		t.Fatalf("err = %v, want the rpc error", err)
	}
	if got := f.shareUnits(t); got != recoveryTotalShares-redeemShares {
		t.Fatalf("share_units = %d, want %d", got, recoveryTotalShares-redeemShares)
	}
	if got := f.payoutFor(t, jobID).Status; got != postgres.RedeemPayoutStatusPending {
		t.Fatalf("payout status = %q, want pending", got)
	}
}

// A job the old sign-and-send flow left in `paying` has no signature to check. The member's
// tap used to pay it again; it must refuse exactly as the recovery poller does.
func TestWithdrawToBalance_legacyPayingJobWithoutSignature_isNeverPaidAgain(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-legacy")
	const redeemShares = int64(500_000)
	jobID := wedgeRedeemJobInPaying(t, f.h, f.userID, f.groupID, f.payoutAddr, redeemShares, redeemShares)

	_, err := f.cashOut(redeemShares)

	if !errors.Is(err, ErrRedeemPayoutUnverified) {
		t.Fatalf("err = %v, want ErrRedeemPayoutUnverified", err)
	}
	if got := privy.LandedPayoutCount(f.h.Privy); got != 0 {
		t.Fatalf("transfers landed = %d, want 0", got)
	}
	if got := f.shareUnits(t); got != recoveryTotalShares-redeemShares {
		t.Fatalf("share_units = %d, want %d (neither returned nor burnt again)", got, recoveryTotalShares-redeemShares)
	}
	if job, found := f.activeJob(t); !found || job.ID != jobID || job.Status != string(domain.RedeemJobPaying) {
		t.Fatalf("job found=%v id=%s status=%q, want it untouched", found, job.ID, job.Status)
	}
}

// The live mark can vanish after the shares are debited: the job is re-priced before it pays.
// That refusal has to land before anything is signed, so it leaves no payout on record and
// no `paying` job behind, only the member's shares back where they were.
func TestWithdrawToBalance_liveMarkLostAfterDebit_signsNothingAndReturnsShares(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	governance := NewGovernanceService(h.Store, h.Privy)
	governance.SetRedeemService(h.Redeem)
	token := h.ISO.UniqueToken("payout-mark-lost")
	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "payout-mark-lost", "Mark Lost User")
	group, err := governance.CreateGroupWithRules(ctx, token, testGroupName(h.ISO, "payout-mark-lost"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	const totalShares = int64(2_000_000)
	const redeemShares = int64(500_000)
	seedStakeAndHoldings(t, h, session.UserID, group.GroupID, "payout-mark-lost", totalShares, 1_000_000)
	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 1_000_000)
	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	jobID := wedgeRedeemJobInSelling(t, h, session.UserID, group.GroupID, wallet.SolanaAddress, redeemShares, 500_000)
	pyth.RegisterMarkedPotError(h.Pyth, pyth.TreasuryRef{GroupID: group.GroupID}, errors.New("hermes down"))

	_, err = h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{AccessToken: token, GroupID: group.GroupID})

	if !errors.Is(err, ErrPotMarkUnavailable) {
		t.Fatalf("err = %v, want ErrPotMarkUnavailable", err)
	}
	if got := privy.PreparedPayoutCount(h.Privy); got != 0 {
		t.Fatalf("payouts signed = %d, want 0", got)
	}
	if _, found, err := h.Store.GetRedeemPayoutByJobID(ctx, jobID); err != nil || found {
		t.Fatalf("redeem payout found=%v err=%v, want none on record", found, err)
	}
	if active, err := h.Store.HasActiveRedeemJobForUser(ctx, session.UserID, group.GroupID); err != nil || active {
		t.Fatalf("active redeem job = %v err = %v, want none left behind", active, err)
	}
	position, _, err := h.Store.GetPosition(ctx, session.UserID, group.GroupID)
	if err != nil || position.ShareUnits != totalShares {
		t.Fatalf("share_units = %d err = %v, want %d (returned)", position.ShareUnits, err, totalShares)
	}
}

// A block height that does not fit the bigint column is refused, never wrapped: a wrapped
// height would make a transfer look expired (or alive) when it is not.
func TestRecordRedeemPayoutIntent_blockHeightOutOfRange_isRefused(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-height-range")
	ctx := context.Background()
	const redeemShares = int64(500_000)
	jobID := wedgeRedeemJobInSelling(t, f.h, f.userID, f.groupID, f.payoutAddr, redeemShares, redeemShares)

	for _, height := range []uint64{0, 1 << 63, ^uint64(0)} {
		_, err := f.h.Store.RecordRedeemPayoutIntent(ctx, postgres.RedeemPayoutIntent{
			RedeemJobID:          jobID,
			UserID:               f.userID,
			GroupID:              f.groupID,
			Amount:               redeemShares,
			ToAddress:            f.payoutAddr,
			TxSignature:          "sig-height-range",
			SignedTx:             "SIGNED:sig-height-range",
			LastValidBlockHeight: height,
		})
		if err == nil {
			t.Fatalf("height %d: want the intent refused", height)
		}
	}
	if job, found := f.activeJob(t); !found || job.Status != string(domain.RedeemJobSelling) {
		t.Fatalf("job found=%v status=%q, want it still selling", found, job.Status)
	}
}

// Shares may only go back together with the chain's verdict on the recorded payout.
func TestAbortRedeemJob_payoutOnRecord_isRefused(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-abort-guard")
	const redeemShares = int64(500_000)
	jobID, _ := f.signAndRecordPayout(t, redeemShares, redeemShares)
	job, _, err := f.h.Store.GetRedeemJobByID(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetRedeemJobByID: %v", err)
	}

	err = f.h.Redeem.abortRedeemJob(context.Background(), redeemJobFromRow(job, Position{}))

	if !errors.Is(err, ErrRedeemPayoutUnverified) {
		t.Fatalf("err = %v, want ErrRedeemPayoutUnverified", err)
	}
	if got := f.shareUnits(t); got != recoveryTotalShares-redeemShares {
		t.Fatalf("share_units = %d, want %d (not returned)", got, recoveryTotalShares-redeemShares)
	}
}

// A job gets one signed transfer. Whatever goes wrong above the store, a second one for the
// same job cannot be put on record, so it cannot be broadcast either.
func TestRecordRedeemPayoutIntent_secondIntentForSameJob_isRefused(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-second-intent")
	ctx := context.Background()
	const redeemShares = int64(500_000)
	jobID, first := f.signAndRecordPayout(t, redeemShares, redeemShares)
	second := postgres.RedeemPayoutIntent{
		RedeemJobID:          jobID,
		UserID:               f.userID,
		GroupID:              f.groupID,
		Amount:               redeemShares,
		ToAddress:            f.payoutAddr,
		TxSignature:          first.TxSignature + "-again",
		SignedTx:             "SIGNED:again",
		LastValidBlockHeight: first.LastValidBlockHeight + 1,
	}

	_, errWhilePaying := f.h.Store.RecordRedeemPayoutIntent(ctx, second)
	if err := f.h.Store.UpdateRedeemJobStatus(ctx, jobID, string(domain.RedeemJobSelling)); err != nil {
		t.Fatalf("UpdateRedeemJobStatus: %v", err)
	}
	_, errAfterStatusRewind := f.h.Store.RecordRedeemPayoutIntent(ctx, second)

	if errWhilePaying == nil {
		t.Fatal("want a second intent refused while the job is paying")
	}
	if !errors.Is(errAfterStatusRewind, postgres.ErrRedeemPayoutExists) {
		t.Fatalf("err = %v, want ErrRedeemPayoutExists even if the job status were rewound", errAfterStatusRewind)
	}
	if got := f.payoutFor(t, jobID).TxSignature; got != first.TxSignature {
		t.Fatalf("recorded signature = %q, want the first %q", got, first.TxSignature)
	}
}

// Many taps racing over a job whose transfer already landed settle it once. The job holds
// every share the member has, so a tap that arrives after it settles has nothing to redeem.
func TestWithdrawToBalance_concurrentRetriesOverLandedPayout_settleOnce(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-concurrent-retry")
	_, prepared := f.signAndRecordPayout(t, recoveryTotalShares, recoveryTotalShares)
	if err := f.h.Privy.BroadcastUSDCPayout(context.Background(), prepared); err != nil {
		t.Fatalf("BroadcastUSDCPayout: %v", err)
	}

	errs := raceCashOuts(f, 8, recoveryTotalShares)

	assertOnlyBenignCashOutErrors(t, errs)
	f.assertPaidExactlyOnce(t, recoveryTotalShares)
	if got := f.shareUnits(t); got != 0 {
		t.Fatalf("share_units = %d, want 0", got)
	}
}

// Many first taps racing for the same full exit sign and pay exactly one transfer.
func TestWithdrawToBalance_concurrentFirstTaps_payOnce(t *testing.T) {
	t.Parallel()

	f := newPayoutFixture(t, "payout-concurrent-first")

	errs := raceCashOuts(f, 8, recoveryTotalShares)

	assertOnlyBenignCashOutErrors(t, errs)
	f.assertPaidExactlyOnce(t, recoveryTotalShares)
	if got := f.shareUnits(t); got != 0 {
		t.Fatalf("share_units = %d, want 0", got)
	}
}

func raceCashOuts(f payoutFixture, taps int, shareUnits int64) []error {
	errs := make([]error, taps)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = f.cashOut(shareUnits)
		}(i)
	}
	close(start)
	wg.Wait()
	return errs
}

func assertOnlyBenignCashOutErrors(t *testing.T, errs []error) {
	t.Helper()
	succeeded := 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrRedeemAlreadyInProgress), errors.Is(err, ErrInvalidRedeemRequest):
		default:
			t.Fatalf("unexpected cash out error: %v", err)
		}
	}
	if succeeded == 0 {
		t.Fatal("want at least one tap to see the cash out settle")
	}
}

func waitForCondition(t *testing.T, what string, met func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !met() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func ptrInt64(v int64) *int64 { return &v }

var errTreasuryBalanceOutage = errors.New("solana rpc: balance outage")

// treasuryBalanceOutagePrivy fails treasury balance reads once a payout has landed, which is
// the read that follows a confirmed transfer.
type treasuryBalanceOutagePrivy struct {
	privy.Client
	failOncePaid atomic.Bool
}

func (c *treasuryBalanceOutagePrivy) TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error) {
	if c.failOncePaid.Load() && privy.LandedPayoutCount(c.Client) > 0 {
		return 0, errTreasuryBalanceOutage
	}
	return c.Client.TreasuryUSDCBalance(ctx, treasuryAddress)
}
