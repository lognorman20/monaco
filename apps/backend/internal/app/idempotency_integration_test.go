package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

func TestIdempotency_depositSweepBuyWithdrawal_replaySignatureOnceEach(t *testing.T) {
	// Arrange
	h := integrationExecuteOnPassApp(t)
	ctx := context.Background()

	const sweepSignature = "SWEEP-idempotency-e2e"
	const buyRequestID = "req-idempotency-e2e-buy"
	const buySignature = "BUY-idempotency-e2e"
	const payoutSignature = "PAYOUT-idempotency-e2e"
	const depositAmount = int64(4_000_000)
	const buyUSDC = int64(1_000_000)
	const withdrawalAmount = int64(500_000)

	token := privy.AccessToken("token-idempotency-e2e")
	privy.RegisterToken(h.App.Privy, token, privy.Identity{
		PrivyUserID: "did:privy:idempotency-e2e",
		DisplayName: "Idempotency E2E",
	})
	userID, err := NewSessionService(h.App.Store, h.App.Privy).OpenSession(ctx, string(token))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	created, err := h.Governance.CreateGroupWithRules(ctx, string(token), "Idempotency Fund", DefaultGroupRules(), "")
	if err != nil {
		t.Fatalf("CreateGroupWithRules: %v", err)
	}

	deposit, err := h.App.Deposits.CreateDeposit(ctx, string(token), created.GroupID, depositAmount)
	if err != nil {
		t.Fatalf("CreateDeposit: %v", err)
	}
	treasury, err := h.App.Privy.EnsureTreasury(ctx, privy.GroupID(created.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	sweep := ObservedSweep{
		TxSignature: sweepSignature,
		FromAddress: deposit.Deposit.FromAddress,
		ToAddress:   treasury.SolanaAddress,
		Amount:      depositAmount,
		DepositID:   deposit.Deposit.ID,
		UserID:      userID.UserID,
		GroupID:     created.GroupID,
	}

	registerHappyBuy(h.App.Jupiter, h.App.XStocks, jupiter.AAPLxMint, buyUSDC, buyRequestID, buySignature)
	h.App.Swap.SetTreasuryBalances(treasury.SolanaAddress, TreasuryBalances{USDC: depositAmount})

	proposal, err := h.Governance.CreateProposal(ctx, CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "AAPLx",
		UsdcMicros: buyUSDC,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	passed, err := h.Governance.CastVote(ctx, CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    userID.UserID,
		Choice:     domain.VoteYes,
	})
	if err != nil {
		t.Fatalf("CastVote: %v", err)
	}
	if passed.Status != ProposalPassed {
		t.Fatalf("proposal status = %q, want passed", passed.Status)
	}

	withdrawal, err := h.App.Store.InsertWithdrawal(ctx, userID.UserID, created.GroupID, withdrawalAmount, "FAKEpayout-idempotency")
	if err != nil {
		t.Fatalf("InsertWithdrawal: %v", err)
	}
	treasuryAfterBuy := depositAmount - buyUSDC

	// Act — deposit sweep first apply and signature replay
	firstSweep, err := h.App.Deposits.ObserveSweep(ctx, sweep)
	if err != nil {
		t.Fatalf("first ObserveSweep: %v", err)
	}
	replaySweep, err := h.App.Deposits.ObserveSweep(ctx, sweep)

	// Assert — deposit credit idempotent on tx_signature
	if err != nil {
		t.Fatalf("replay ObserveSweep: %v", err)
	}
	if !firstSweep.Credited {
		t.Fatal("expected first sweep to credit position")
	}
	if replaySweep.Credited {
		t.Fatal("expected replay sweep to skip second credit")
	}
	if replaySweep.Position.ShareUnits != firstSweep.Position.ShareUnits {
		t.Fatalf("share_units drifted from %d to %d", firstSweep.Position.ShareUnits, replaySweep.Position.ShareUnits)
	}

	// Act — buy execute first apply and signature replay via proposal re-execute
	firstBuy, err := h.ExecutePass.ExecuteOnPass(ctx, passed)
	if err != nil {
		t.Fatalf("first ExecuteOnPass: %v", err)
	}
	replayBuy, err := h.ExecutePass.ExecuteOnPass(ctx, passed)

	// Assert — buy execute idempotent on tx_signature / proposal
	if err != nil {
		t.Fatalf("replay ExecuteOnPass: %v", err)
	}
	if !firstBuy.Created {
		t.Fatal("expected first buy execute to create transaction")
	}
	if replayBuy.Created {
		t.Fatal("expected replay buy execute to be idempotent")
	}
	if replayBuy.Transaction.ID != firstBuy.Transaction.ID {
		t.Fatalf("buy transaction id = %q, want %q", replayBuy.Transaction.ID, firstBuy.Transaction.ID)
	}
	buySigCount, err := h.App.Store.CountConfirmedTransactionsBySignature(ctx, buySignature)
	if err != nil {
		t.Fatalf("CountConfirmedTransactionsBySignature buy: %v", err)
	}
	if buySigCount != 1 {
		t.Fatalf("buy signature row count = %d, want 1", buySigCount)
	}

	// Act — withdrawal payout first apply and signature replay
	payoutTx1, err := h.App.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx payout first: %v", err)
	}
	_, newlyPaidFirst, err := h.App.Store.ConfirmWithdrawalPayoutTx(ctx, payoutTx1, withdrawal.ID, payoutSignature, treasuryAfterBuy)
	if err != nil {
		t.Fatalf("first ConfirmWithdrawalPayoutTx: %v", err)
	}
	if err := payoutTx1.Commit(); err != nil {
		t.Fatalf("commit payout first: %v", err)
	}

	payoutTx2, err := h.App.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx payout replay: %v", err)
	}
	_, newlyPaidReplay, err := h.App.Store.ConfirmWithdrawalPayoutTx(ctx, payoutTx2, withdrawal.ID, payoutSignature, treasuryAfterBuy)
	if err != nil {
		t.Fatalf("replay ConfirmWithdrawalPayoutTx: %v", err)
	}
	if err := payoutTx2.Commit(); err != nil {
		t.Fatalf("commit payout replay: %v", err)
	}

	// Assert — withdrawal payout idempotent on tx_signature
	if !newlyPaidFirst {
		t.Fatal("expected first withdrawal payout to settle")
	}
	if newlyPaidReplay {
		t.Fatal("expected replay withdrawal payout to skip second apply")
	}
	position, found, err := h.App.Store.GetPosition(ctx, userID.UserID, created.GroupID)
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}
	if !found {
		t.Fatal("expected position after deposit and withdrawal")
	}
	if position.AmountWithdrawn != withdrawalAmount {
		t.Fatalf("amount_withdrawn = %d, want %d", position.AmountWithdrawn, withdrawalAmount)
	}
	withdrawalCount, err := h.App.Store.CountNavSnapshotsByGroupAndReason(ctx, created.GroupID, postgres.NavSnapshotReasonWithdrawalPayout)
	if err != nil {
		t.Fatalf("CountNavSnapshotsByGroupAndReason withdrawal_payout: %v", err)
	}
	if withdrawalCount != 1 {
		t.Fatalf("withdrawal_payout snapshot count = %d, want 1", withdrawalCount)
	}
}
