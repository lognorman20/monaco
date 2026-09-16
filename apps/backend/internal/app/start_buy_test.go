package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

type executeOnPassHarness struct {
	Governance  *GovernanceService
	ExecutePass *ExecuteOnPassService
	App         integrationHarness
}

func integrationExecuteOnPassApp(t *testing.T) executeOnPassHarness {
	t.Helper()

	h := integrationApp(t)
	buy := NewBuyService(h.Jupiter, h.XStocks)
	governance := NewGovernanceService(h.Store, h.Privy)
	governance.SetBuyService(buy)
	return executeOnPassHarness{
		Governance:  governance,
		ExecutePass: NewExecuteOnPassService(h.Swap, h.Store),
		App:         h,
	}
}

func TestExecuteOnPass_onlyAfterTallyPassed_callsJupiter(t *testing.T) {
	// Arrange
	h := integrationExecuteOnPassApp(t)
	ctx := context.Background()
	token := privy.AccessToken("token-execute-pass")
	privy.RegisterToken(h.App.Privy, token, privy.Identity{PrivyUserID: "did:privy:execute-pass", DisplayName: "Execute Pass"})
	userID, err := NewSessionService(h.App.Store, h.App.Privy).OpenSession(ctx, string(token))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	created, err := h.Governance.CreateGroupWithRules(ctx, string(token), "Execute Pass Fund", DefaultGroupRules(), "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	const usdcAmount int64 = 2_000_000
	const requestID = "req-execute-on-pass"
	const signature = "sig-execute-on-pass"
	registerHappyBuy(h.App.Jupiter, h.App.XStocks, jupiter.AAPLxMint, usdcAmount, requestID, signature)
	treasury, err := h.App.Privy.EnsureTreasury(ctx, privy.GroupID(created.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	h.App.Swap.SetTreasuryBalances(treasury.SolanaAddress, TreasuryBalances{USDC: 5_000_000})

	proposal, err := h.Governance.CreateProposal(ctx, CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "AAPLx",
		UsdcMicros: usdcAmount,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	// Act — open proposal must not execute
	_, err = h.ExecutePass.ExecuteOnPass(ctx, proposal)

	// Assert
	if !errors.Is(err, ErrProposalNotPassed) {
		t.Fatalf("open proposal execute err = %v, want ErrProposalNotPassed", err)
	}

	// Act — tally pass then execute
	passed, err := h.Governance.CastVote(ctx, CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    userID.UserID,
		Choice:     domain.VoteYes,
	})
	if err != nil {
		t.Fatalf("CastVote: %v", err)
	}
	if passed.Status != ProposalPassed {
		t.Fatalf("status = %q, want passed", passed.Status)
	}

	result, err := h.ExecutePass.ExecuteOnPass(ctx, passed)

	// Assert
	if err != nil {
		t.Fatalf("ExecuteOnPass: %v", err)
	}
	if !result.Created {
		t.Fatal("expected newly created confirmed buy transaction")
	}
	if !result.Transaction.TxSignature.Valid || result.Transaction.TxSignature.String != signature {
		t.Fatalf("tx signature = %v, want %q", result.Transaction.TxSignature, signature)
	}
	if result.Transaction.Status != postgres.TransactionStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", result.Transaction.Status)
	}
}

func TestExecuteOnPass_writesNavSnapshotOnConfirm(t *testing.T) {
	// Arrange
	h := integrationExecuteOnPassApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h, "token-nav-snapshot", "req-nav-snapshot-pass", "sig-nav-snapshot-pass")

	beforeCount, err := h.App.Store.CountNavSnapshotsByGroupAndReason(ctx, passed.GroupID, postgres.NavSnapshotReasonTransactionConfirm)
	if err != nil {
		t.Fatalf("CountNavSnapshotsByGroupAndReason before: %v", err)
	}

	// Act
	result, err := h.ExecutePass.ExecuteOnPass(ctx, passed)

	// Assert
	if err != nil {
		t.Fatalf("ExecuteOnPass: %v", err)
	}
	if !result.Created {
		t.Fatal("expected newly created confirmed buy transaction")
	}
	afterCount, err := h.App.Store.CountNavSnapshotsByGroupAndReason(ctx, passed.GroupID, postgres.NavSnapshotReasonTransactionConfirm)
	if err != nil {
		t.Fatalf("CountNavSnapshotsByGroupAndReason after: %v", err)
	}
	if afterCount <= beforeCount {
		t.Fatalf("nav snapshot count = %d, want > %d", afterCount, beforeCount)
	}
	holdings, err := TreasuryHoldingsForGroup(ctx, h.App.Store, passed.GroupID)
	if err != nil {
		t.Fatalf("TreasuryHoldingsForGroup: %v", err)
	}
	if len(holdings.Holdings) == 0 {
		t.Fatal("expected treasury holdings after confirmed buy")
	}
}

func TestExecuteOnPass_duplicateProposalAndSignature_executesOnce(t *testing.T) {
	// Arrange
	h := integrationExecuteOnPassApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h, "token-execute-idempotent", "req-execute-idempotent", "sig-execute-idempotent")

	// Act
	first, err := h.ExecutePass.ExecuteOnPass(ctx, passed)
	if err != nil {
		t.Fatalf("first ExecuteOnPass: %v", err)
	}
	second, err := h.ExecutePass.ExecuteOnPass(ctx, passed)

	// Assert
	if err != nil {
		t.Fatalf("second ExecuteOnPass: %v", err)
	}
	if !first.Created {
		t.Fatal("expected first execute to create transaction")
	}
	if second.Created {
		t.Fatal("expected second execute to be idempotent")
	}
	if second.Transaction.ID != first.Transaction.ID {
		t.Fatalf("transaction id = %q, want %q", second.Transaction.ID, first.Transaction.ID)
	}
	if !second.Transaction.ProposalID.Valid || second.Transaction.ProposalID.String != passed.ID {
		t.Fatalf("proposal_id = %v, want %q", second.Transaction.ProposalID, passed.ID)
	}
	if !second.Transaction.TxSignature.Valid || second.Transaction.TxSignature.String != first.Transaction.TxSignature.String {
		t.Fatalf("tx signature = %v, want %v", second.Transaction.TxSignature, first.Transaction.TxSignature)
	}
	proposalCount, err := h.App.Store.CountTransactionsForProposal(ctx, passed.ID)
	if err != nil {
		t.Fatalf("CountTransactionsForProposal: %v", err)
	}
	if proposalCount != 1 {
		t.Fatalf("proposal transaction count = %d, want 1", proposalCount)
	}
	sigCount, err := h.App.Store.CountConfirmedTransactionsBySignature(ctx, first.Transaction.TxSignature.String)
	if err != nil {
		t.Fatalf("CountConfirmedTransactionsBySignature: %v", err)
	}
	if sigCount != 1 {
		t.Fatalf("signature transaction count = %d, want 1", sigCount)
	}
}

func seedPassedExecuteProposal(t *testing.T, h executeOnPassHarness, tokenSuffix, requestID, signature string) Proposal {
	t.Helper()
	ctx := context.Background()
	token := privy.AccessToken("token-" + tokenSuffix)
	privy.RegisterToken(h.App.Privy, token, privy.Identity{PrivyUserID: "did:privy:" + tokenSuffix, DisplayName: "Execute Pass"})
	userID, err := NewSessionService(h.App.Store, h.App.Privy).OpenSession(ctx, string(token))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	created, err := h.Governance.CreateGroupWithRules(ctx, string(token), "Execute Pass Fund", DefaultGroupRules(), "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	const usdcAmount int64 = 2_000_000
	registerHappyBuy(h.App.Jupiter, h.App.XStocks, jupiter.AAPLxMint, usdcAmount, requestID, signature)
	treasury, err := h.App.Privy.EnsureTreasury(ctx, privy.GroupID(created.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	h.App.Swap.SetTreasuryBalances(treasury.SolanaAddress, TreasuryBalances{USDC: 5_000_000})
	proposal, err := h.Governance.CreateProposal(ctx, CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "AAPLx",
		UsdcMicros: usdcAmount,
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
		t.Fatalf("status = %q, want passed", passed.Status)
	}
	return passed
}
