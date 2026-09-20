package app

import (
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/packages/domain"
)

func registerHappyBuy(client dex.Client, resolver b20.Catalog, outputMint string, usdcAmount int64, _, _ string) {
	registerTestB20Asset(resolver, "AAPLx", outputMint)
	dex.RegisterQuote(client, dex.Quote{
		TokenIn:   dex.USDCAddress(),
		TokenOut:  outputMint,
		AmountIn:  big.NewInt(usdcAmount),
		AmountOut: big.NewInt(500_000),
		Routable:  true,
	})
}

type executeOnPassHarness struct {
	Governance  *GovernanceService
	ExecutePass *ExecuteOnPassService
	App         integrationHarness
}

func integrationExecuteOnPassApp(t *testing.T) executeOnPassHarness {
	t.Helper()

	h := integrationApp(t)
	buy := NewBuyService(h.Jupiter, h.XStocks)
	governance := NewGovernanceService(h.Store, h.Auth, h.Wallets)
	governance.SetBuyService(buy)
	governance.SetSwapService(h.Swap)
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
	sessions := NewSessionService(h.App.Store, h.App.Auth, h.App.Wallets)
	userID := openTestSession(t, h.App.ISO, sessions, h.App.Auth, "execute-pass", "Execute Pass")
	token := h.App.ISO.UniqueToken("execute-pass")
	created, err := h.Governance.CreateGroupWithRules(ctx, token, testGroupName(h.App.ISO, "execute-pass"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.App.ISO.TrackGroup(created.GroupID)
	const usdcAmount int64 = 2_000_000
	requestID := testRequestID(h.App.ISO, "execute-on-pass")
	signature := testTxHash(h.App.ISO, "execute-on-pass")
	registerHappyBuy(h.App.Jupiter, h.App.XStocks, "0xb200000000000000000000c2e324d24d7eecd1fb", usdcAmount, requestID, signature)
	treasury, err := h.App.Wallets.EnsureTreasury(ctx, wallets.GroupID(created.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	h.App.Swap.SetTreasuryBalances(treasury.Address, TreasuryBalances{USDC: 5_000_000})
	wallets.SetTreasuryUSDCBalance(h.App.Wallets, treasury.Address, 5_000_000)

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
	if !result.Transaction.TxHash.Valid || result.Transaction.TxHash.String != signature {
		t.Fatalf("tx signature = %v, want %q", result.Transaction.TxHash, signature)
	}
	if result.Transaction.Status != postgres.TransactionStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", result.Transaction.Status)
	}
}

func TestExecuteOnPass_writesNavSnapshotOnConfirm(t *testing.T) {
	// Arrange
	h := integrationExecuteOnPassApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h, "nav-snapshot")

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
	passed := seedPassedExecuteProposal(t, h, "execute-idempotent")

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
	if !second.Transaction.TxHash.Valid || second.Transaction.TxHash.String != first.Transaction.TxHash.String {
		t.Fatalf("tx signature = %v, want %v", second.Transaction.TxHash, first.Transaction.TxHash)
	}
	proposalCount, err := h.App.Store.CountTransactionsForProposal(ctx, passed.ID)
	if err != nil {
		t.Fatalf("CountTransactionsForProposal: %v", err)
	}
	if proposalCount != 1 {
		t.Fatalf("proposal transaction count = %d, want 1", proposalCount)
	}
	sigCount, err := h.App.Store.CountConfirmedTransactionsBySignature(ctx, first.Transaction.TxHash.String)
	if err != nil {
		t.Fatalf("CountConfirmedTransactionsBySignature: %v", err)
	}
	if sigCount != 1 {
		t.Fatalf("signature transaction count = %d, want 1", sigCount)
	}
}

func seedPassedExecuteProposal(t *testing.T, h executeOnPassHarness, label string) Proposal {
	t.Helper()
	ctx := context.Background()
	sessions := NewSessionService(h.App.Store, h.App.Auth, h.App.Wallets)
	userID := openTestSession(t, h.App.ISO, sessions, h.App.Auth, label, "Execute Pass")
	token := h.App.ISO.UniqueToken(label)
	created, err := h.Governance.CreateGroupWithRules(ctx, token, testGroupName(h.App.ISO, label), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.App.ISO.TrackGroup(created.GroupID)
	const usdcAmount int64 = 2_000_000
	requestID := testRequestID(h.App.ISO, label)
	signature := testTxHash(h.App.ISO, label)
	registerHappyBuy(h.App.Jupiter, h.App.XStocks, "0xb200000000000000000000c2e324d24d7eecd1fb", usdcAmount, requestID, signature)
	treasury, err := h.App.Wallets.EnsureTreasury(ctx, wallets.GroupID(created.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	h.App.Swap.SetTreasuryBalances(treasury.Address, TreasuryBalances{USDC: 5_000_000})
	wallets.SetTreasuryUSDCBalance(h.App.Wallets, treasury.Address, 5_000_000)
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

func TestExecuteOnPass_sellDoesNotChangeMemberShareUnits(t *testing.T) {
	h := integrationExecuteOnPassApp(t)
	ctx := context.Background()
	sessions := NewSessionService(h.App.Store, h.App.Auth, h.App.Wallets)
	user := openTestSession(t, h.App.ISO, sessions, h.App.Auth, "sell-exec", "Sell Exec")
	token := h.App.ISO.UniqueToken("sell-exec")
	created, err := h.Governance.CreateGroupWithRules(ctx, token, testGroupName(h.App.ISO, "sell-exec"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.App.ISO.TrackGroup(created.GroupID)

	const held = int64(100_000_000)
	registerTestB20Asset(h.App.XStocks, "AAPLx", "0xb200000000000000000000c2e324d24d7eecd1fb")
	_, _, err = h.App.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          created.GroupID,
		Amount:           10_000_000,
		InputToken:        evm.USDCAddress,
		OutputToken:       "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash:      testTxHash(h.App.ISO, "sell-exec-buy"),
		ExecuteRequestID: testRequestID(h.App.ISO, "sell-exec-buy"),
		CostBasisPrice:   10_000_000,
		CostBasisAmount:  held,
	})
	if err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}

	beforeShares, err := h.App.Store.SumShareUnitsByGroup(ctx, created.GroupID)
	if err != nil {
		t.Fatalf("SumShareUnitsByGroup before: %v", err)
	}

	registerDexSellQuote(t, h.App.Jupiter, "0xb200000000000000000000c2e324d24d7eecd1fb", held, 10_000_000)
	treasury, err := h.App.Wallets.EnsureTreasury(ctx, wallets.GroupID(created.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	h.App.Swap.SetTreasuryBalances(treasury.Address, TreasuryBalances{USDC: 1_000_000, Token: held})

	proposal, err := h.Governance.CreateProposal(ctx, CreateProposalInput{
		GroupID:     created.GroupID,
		ProposerID:  user.UserID,
		Symbol:      "AAPLx",
		Kind:        domain.ProposalKindSell,
		TokenAmount: held,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	passed, err := h.Governance.CastVote(ctx, CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    user.UserID,
		Choice:     domain.VoteYes,
	})
	if err != nil {
		t.Fatalf("CastVote: %v", err)
	}
	if passed.Status != ProposalPassed {
		t.Fatalf("status = %q, want passed", passed.Status)
	}

	result, err := h.ExecutePass.ExecuteOnPass(ctx, passed)
	if err != nil {
		t.Fatalf("ExecuteOnPass sell: %v", err)
	}
	if result.Transaction.Action != postgres.TransactionActionSell {
		t.Fatalf("action = %q, want sell", result.Transaction.Action)
	}

	afterShares, err := h.App.Store.SumShareUnitsByGroup(ctx, created.GroupID)
	if err != nil {
		t.Fatalf("SumShareUnitsByGroup after: %v", err)
	}
	if afterShares != beforeShares {
		t.Fatalf("share units changed: before=%d after=%d", beforeShares, afterShares)
	}
}
