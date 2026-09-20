package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/apps/backend/internal/b20"
)

func registerHappySell(client dex.Client, inputMint string, amount int64, requestID string, signature string) {
	jupiter.RegisterSellQuote(client, inputMint, amount, jupiter.SellQuote{
		Routable:    true,
		InputToken:   inputMint,
		OutputToken:  evm.USDCAddress,
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
		InputToken:        evm.USDCAddress,
		OutputToken:       "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash:      testTxHash(h.ISO, "confirmed-other"),
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
	signature := testTxHash(h.ISO, "retry-buy")
	registerHappyBuy(h.Jupiter, h.XStocks, "0xb200000000000000000000c2e324d24d7eecd1fb", usdcAmount, requestID, signature)

	treasury, err := h.Privy.EnsureTreasury(ctx, wallets.GroupID(group.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	h.Swap.SetTreasuryBalances(treasury.Address, TreasuryBalances{USDC: 5_000_000})
	wallets.SetTreasuryUSDCBalance(h.Privy, treasury.Address, 5_000_000)

	failed, err := h.Store.InsertFailedTransaction(ctx, group.GroupID, postgres.TransactionActionBuy, evm.USDCAddress, "0xb200000000000000000000c2e324d24d7eecd1fb", usdcAmount, testRequestID(h.ISO, "buy-failed"))
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
	signature := testTxHash(h.ISO, "retry-sell")
	registerHappySell(h.Jupiter, "0xb200000000000000000000c2e324d24d7eecd1fb", amount, requestID, signature)

	treasury, err := h.Privy.EnsureTreasury(ctx, wallets.GroupID(group.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	h.Swap.SetTreasuryBalances(treasury.Address, TreasuryBalances{XStock: 2_000_000})

	failed, err := h.Store.InsertFailedTransaction(ctx, group.GroupID, postgres.TransactionActionSell, "0xb200000000000000000000c2e324d24d7eecd1fb", evm.USDCAddress, amount, testRequestID(h.ISO, "sell-failed"))
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
	b20.RegisterTokenAddress(h.XStocks, "AAPLx", "0xb200000000000000000000c2e324d24d7eecd1fb")
	jupiter.RegisterQuoteBuy(h.Jupiter, "0xb200000000000000000000c2e324d24d7eecd1fb", 2_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputToken:  evm.USDCAddress,
		OutputToken: "0xb200000000000000000000c2e324d24d7eecd1fb",
		InAmount:   "2000000",
		OutAmount:  "1000000",
		RequestID:  testRequestID(h.ISO, "proposal-quote"),
	})
	treasury, err := h.Privy.EnsureTreasury(ctx, wallets.GroupID(group.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	wallets.SetTreasuryUSDCBalance(h.Privy, treasury.Address, 5_000_000)

	proposal, err := governance.CreateProposal(ctx, CreateProposalInput{
		GroupID:    group.GroupID,
		ProposerID: session.UserID,
		Symbol:     "AAPLx",
		UsdcMicros: 2_000_000,
	})
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	confirmed, _, err := h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           2_000_000,
		InputToken:        evm.USDCAddress,
		OutputToken:       "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash:      testTxHash(h.ISO, "already-confirmed"),
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
	proposalID := proposal.ID

	executeRequestID := testRequestID(h.ISO, "failed-with-proposal")
	pending, _, err := h.Store.InsertPendingTransaction(ctx, postgres.InsertPendingTransactionParams{
		GroupID:          group.GroupID,
		ProposalID:       proposalID,
		Action:           postgres.TransactionActionBuy,
		InputToken:        evm.USDCAddress,
		OutputToken:       "0xb200000000000000000000c2e324d24d7eecd1fb",
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
