package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func registerHappySell(client jupiter.Client, inputMint string, amount int64, requestID string, signature string) {
	jupiter.RegisterSellQuote(client, inputMint, amount, jupiter.SellQuote{
		Routable:    true,
		InputMint:   inputMint,
		OutputMint:  jupiter.USDCMint,
		InAmount:    "1000000",
		OutAmount:   "900000",
		RequestID:   requestID,
		Transaction: "unsigned-sell-tx",
	})
	jupiter.RegisterExecutePoll(client, requestID, []jupiter.ExecuteResult{
		{Status: jupiter.ExecuteStatusPending, Code: -1},
		{
			Status:             jupiter.ExecuteStatusSuccess,
			Code:               0,
			Signature:          signature,
			InputAmountResult:  "1000000",
			OutputAmountResult: "900000",
		},
	})
}

func TestRetryFailedSwap_rejectsNonFailedTransaction(t *testing.T) {
	h := integrationApp(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	session := openTestSession(t, h.ISO, sessions, h.Privy, "retry-reject", "Retry Reject")
	governance := NewGovernanceService(h.Store, h.Privy)
	group, err := governance.CreateGroupWithRules(ctx, h.ISO.UniqueToken("retry-reject"), testGroupName(h.ISO, "retry-reject"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	confirmed, _, err := h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           1_000_000,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      testTxSignature(h.ISO, "confirmed-other"),
		ExecuteRequestID: testRequestID(h.ISO, "confirmed-other"),
		CostBasisPrice:   1_000_000,
		CostBasisAmount:  500_000,
	})
	if err != nil {
		t.Fatalf("confirm buy: %v", err)
	}

	_, err = h.Swap.RetryFailedSwap(ctx, RetryFailedSwapRequest{
		TransactionID: confirmed.ID,
		UserID:        session.UserID,
	})
	if !errors.Is(err, ErrTransactionNotRetryable) {
		t.Fatalf("retry confirmed err = %v, want ErrTransactionNotRetryable", err)
	}
}

func TestRetryFailedSwap_buySuccessCreatesNewConfirmedRow(t *testing.T) {
	h := integrationApp(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	session := openTestSession(t, h.ISO, sessions, h.Privy, "retry-buy", "Retry Buy")
	governance := NewGovernanceService(h.Store, h.Privy)
	group, err := governance.CreateGroupWithRules(ctx, h.ISO.UniqueToken("retry-buy"), testGroupName(h.ISO, "retry-buy"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	const usdcAmount int64 = 2_000_000
	requestID := testRequestID(h.ISO, "retry-buy")
	signature := testTxSignature(h.ISO, "retry-buy")
	registerHappyBuy(h.Jupiter, h.XStocks, jupiter.AAPLxMint, usdcAmount, requestID, signature)

	treasury, err := h.Privy.EnsureTreasury(ctx, privy.GroupID(group.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	privy.SetTreasuryUSDCBalance(h.Privy, treasury.SolanaAddress, 5_000_000)

	failed, err := h.Store.InsertFailedTransaction(ctx, group.GroupID, postgres.TransactionActionBuy, jupiter.USDCMint, jupiter.AAPLxMint, usdcAmount, testRequestID(h.ISO, "buy-failed"))
	if err != nil {
		t.Fatalf("insert failed buy: %v", err)
	}

	result, err := h.Swap.RetryFailedSwap(ctx, RetryFailedSwapRequest{
		TransactionID: failed.ID,
		UserID:        session.UserID,
	})
	if err != nil {
		t.Fatalf("RetryFailedSwap: %v", err)
	}
	if !result.Created {
		t.Fatal("expected newly created confirmed transaction")
	}
	if result.Transaction.Status != postgres.TransactionStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", result.Transaction.Status)
	}
	if result.Transaction.ID == failed.ID {
		t.Fatal("expected new transaction row, not reuse of failed row")
	}
}

func TestRetryFailedSwap_sellSuccessCreatesNewConfirmedRow(t *testing.T) {
	h := integrationApp(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	session := openTestSession(t, h.ISO, sessions, h.Privy, "retry-sell", "Retry Sell")
	governance := NewGovernanceService(h.Store, h.Privy)
	group, err := governance.CreateGroupWithRules(ctx, h.ISO.UniqueToken("retry-sell"), testGroupName(h.ISO, "retry-sell"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	const amount int64 = 1_000_000
	requestID := testRequestID(h.ISO, "retry-sell")
	signature := testTxSignature(h.ISO, "retry-sell")
	registerHappySell(h.Jupiter, jupiter.AAPLxMint, amount, requestID, signature)

	treasury, err := h.Privy.EnsureTreasury(ctx, privy.GroupID(group.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	privy.SetTreasuryUSDCBalance(h.Privy, treasury.SolanaAddress, 0)

	failed, err := h.Store.InsertFailedTransaction(ctx, group.GroupID, postgres.TransactionActionSell, jupiter.AAPLxMint, jupiter.USDCMint, amount, testRequestID(h.ISO, "sell-failed"))
	if err != nil {
		t.Fatalf("insert failed sell: %v", err)
	}

	result, err := h.Swap.RetryFailedSwap(ctx, RetryFailedSwapRequest{
		TransactionID: failed.ID,
		UserID:        session.UserID,
	})
	if err != nil {
		t.Fatalf("RetryFailedSwap: %v", err)
	}
	if !result.Created {
		t.Fatal("expected newly created confirmed transaction")
	}
	if result.Transaction.Status != postgres.TransactionStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", result.Transaction.Status)
	}
}

func TestRetryFailedSwap_idempotentWhenProposalAlreadyConfirmed(t *testing.T) {
	h := integrationApp(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	session := openTestSession(t, h.ISO, sessions, h.Privy, "retry-idem", "Retry Idem")
	governance := NewGovernanceService(h.Store, h.Privy)
	governance.SetBuyService(NewBuyService(h.Jupiter, h.XStocks))
	group, err := governance.CreateGroupWithRules(ctx, h.ISO.UniqueToken("retry-idem"), testGroupName(h.ISO, "retry-idem"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	xstocks.RegisterSolanaMint(h.XStocks, "AAPLx", jupiter.AAPLxMint)
	jupiter.RegisterQuoteBuy(h.Jupiter, jupiter.AAPLxMint, 2_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "2000000",
		OutAmount:  "1000000",
		RequestID:  testRequestID(h.ISO, "proposal-quote"),
	})
	treasury, err := h.Privy.EnsureTreasury(ctx, privy.GroupID(group.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	privy.SetTreasuryUSDCBalance(h.Privy, treasury.SolanaAddress, 5_000_000)

	proposal, err := governance.CreateProposal(ctx, CreateProposalInput{
		GroupID:    group.GroupID,
		ProposerID: session.UserID,
		Symbol:     "AAPLx",
		UsdcMicros: 2_000_000,
	})
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	proposalID := proposal.ID

	executeRequestID := testRequestID(h.ISO, "failed-with-proposal")
	pending, _, err := h.Store.InsertPendingTransaction(ctx, postgres.InsertPendingTransactionParams{
		GroupID:          group.GroupID,
		ProposalID:       proposalID,
		Action:           postgres.TransactionActionBuy,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		Amount:           2_000_000,
		ExecuteRequestID: executeRequestID,
	})
	if err != nil {
		t.Fatalf("insert pending buy: %v", err)
	}
	failed, ok, err := h.Store.FailTransactionByExecuteRequestID(ctx, executeRequestID)
	if err != nil || !ok {
		t.Fatalf("fail pending buy: err=%v ok=%v", err, ok)
	}
	if failed.ID != pending.ID {
		t.Fatalf("failed id = %q, want pending %q", failed.ID, pending.ID)
	}

	// The earlier attempt failed for good and a later one confirmed: a proposal never holds a
	// pending and a confirmed swap at once.
	confirmed, _, err := h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           2_000_000,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      testTxSignature(h.ISO, "already-confirmed"),
		ExecuteRequestID: testRequestID(h.ISO, "already-confirmed"),
		CostBasisPrice:   2_000_000,
		CostBasisAmount:  1_000_000,
	})
	if err != nil {
		t.Fatalf("confirm buy: %v", err)
	}
	if _, _, err := h.Store.SetTransactionProposalID(ctx, confirmed.ID, proposal.ID); err != nil {
		t.Fatalf("link proposal: %v", err)
	}

	result, err := h.Swap.RetryFailedSwap(ctx, RetryFailedSwapRequest{
		TransactionID: failed.ID,
		UserID:        session.UserID,
	})
	if err != nil {
		t.Fatalf("RetryFailedSwap: %v", err)
	}
	if result.Created {
		t.Fatal("expected idempotent retry without new swap")
	}
	if result.Transaction.ID != confirmed.ID {
		t.Fatalf("transaction id = %q, want confirmed %q", result.Transaction.ID, confirmed.ID)
	}
}
