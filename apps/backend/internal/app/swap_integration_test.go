package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func seedSwapGroup(t *testing.T, h integrationHarness) (groupID, userID, treasuryAddress string) {
	t.Helper()
	ctx := context.Background()

	token := privy.AccessToken("swap-test-token")
	privy.RegisterToken(h.Privy, token, privy.Identity{
		PrivyUserID: "did:privy:swap-user",
		DisplayName: "Swap Tester",
	})

	user, err := h.Store.UpsertUser(ctx, "did:privy:swap-user", "Swap Tester")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	group, err := h.Groups.CreateGroup(ctx, string(token), "Swap Group")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	treasury, err := h.Privy.EnsureTreasury(ctx, privy.GroupID(group.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}

	return group.GroupID, user.ID, treasury.SolanaAddress
}

func registerHappyBuy(client jupiter.Client, resolver xstocks.Resolver, outputMint string, usdcAmount int64, requestID string, signature string) {
	xstocks.RegisterSolanaMint(resolver, "AAPLx", outputMint)
	jupiter.RegisterQuoteBuy(client, outputMint, usdcAmount, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: outputMint,
		InAmount:   "1000000",
		OutAmount:  "500000",
		RequestID:  requestID,
	})
	jupiter.RegisterBuyOrder(client, requestID, jupiter.BuyOrder{
		RequestID:   requestID,
		Transaction: "unsigned-buy-tx",
		InAmount:    "1000000",
		OutAmount:   "500000",
		InputMint:   jupiter.USDCMint,
		OutputMint:  outputMint,
	})
	jupiter.RegisterExecutePoll(client, requestID, []jupiter.ExecuteResult{
		{Status: jupiter.ExecuteStatusPending, Code: -1},
		{
			Status:             jupiter.ExecuteStatusSuccess,
			Code:               0,
			Signature:          signature,
			InputAmountResult:  "1000000",
			OutputAmountResult: "500000",
		},
	})
}

func TestJupiterExecute_terminalFailure_doesNotPersistConfirmedRow(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	ctx := context.Background()
	groupID, userID, _ := seedSwapGroup(t, h)
	const outputMint = jupiter.AAPLxMint
	const requestID = "req-terminal-buy"
	xstocks.RegisterSolanaMint(h.XStocks, "AAPLx", outputMint)
	registerHappyBuy(h.Jupiter, h.XStocks, outputMint, 1_000_000, requestID, "should-not-persist")
	jupiter.RegisterExecutePoll(h.Jupiter, requestID, []jupiter.ExecuteResult{
		{Status: jupiter.ExecuteStatusFailed, Code: -1000, Signature: "failed-buy-sig", Error: "failed to land"},
	})

	// Act
	_, err := h.Swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{
		GroupID:    groupID,
		UserID:     userID,
		Symbol:     "AAPLx",
		USDCAmount: 1_000_000,
	})

	// Assert
	if err == nil {
		t.Fatal("expected terminal failure")
	}
	count, err := h.Store.CountConfirmedTransactionsBySignature(ctx, "failed-buy-sig")
	if err != nil {
		t.Fatalf("CountConfirmedTransactionsBySignature: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 confirmed rows, got %d", count)
	}
}

func TestDevExecuteBuy_happyPath_insertsOneTransactionRowWithCostBasis(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	ctx := context.Background()
	groupID, userID, _ := seedSwapGroup(t, h)
	const outputMint = jupiter.AAPLxMint
	const requestID = "req-happy-buy"
	const signature = "happy-buy-sig"
	registerHappyBuy(h.Jupiter, h.XStocks, outputMint, 1_000_000, requestID, signature)

	// Act
	result, err := h.Swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{
		GroupID:    groupID,
		UserID:     userID,
		Symbol:     "AAPLx",
		USDCAmount: 1_000_000,
	})

	// Assert
	if err != nil {
		t.Fatalf("DevExecuteBuy: %v", err)
	}
	if !result.Created {
		t.Fatal("expected created transaction")
	}
	if result.Transaction.Action != postgres.TransactionActionBuy {
		t.Fatalf("action = %q, want buy", result.Transaction.Action)
	}
	if !result.Transaction.CostBasisAmount.Valid || result.Transaction.CostBasisAmount.Int64 != 500_000 {
		t.Fatalf("cost_basis_amount = %v, want 500000", result.Transaction.CostBasisAmount)
	}
	if !result.Transaction.CostBasisPrice.Valid || result.Transaction.CostBasisPrice.Int64 != 1_000_000 {
		t.Fatalf("cost_basis_price = %v, want 1000000", result.Transaction.CostBasisPrice)
	}
	count, err := h.Store.CountConfirmedTransactionsBySignature(ctx, signature)
	if err != nil {
		t.Fatalf("CountConfirmedTransactionsBySignature: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 confirmed row, got %d", count)
	}
}

func TestDevExecuteBuy_duplicateTxSignature_doesNotDoubleRecordOrCostBasis(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	ctx := context.Background()
	groupID, userID, _ := seedSwapGroup(t, h)
	const outputMint = jupiter.AAPLxMint
	const requestID = "req-dup-sig"
	const signature = "dup-buy-sig"
	registerHappyBuy(h.Jupiter, h.XStocks, outputMint, 1_000_000, requestID, signature)

	first, err := h.Swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{
		GroupID:    groupID,
		UserID:     userID,
		Symbol:     "AAPLx",
		USDCAmount: 1_000_000,
	})
	if err != nil {
		t.Fatalf("first DevExecuteBuy: %v", err)
	}

	// Act
	second, err := h.Swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{
		GroupID:    groupID,
		UserID:     userID,
		Symbol:     "AAPLx",
		USDCAmount: 1_000_000,
	})

	// Assert
	if err != nil {
		t.Fatalf("second DevExecuteBuy: %v", err)
	}
	if second.Created {
		t.Fatal("expected no second insert")
	}
	if second.Transaction.ID != first.Transaction.ID {
		t.Fatalf("expected same transaction id %s, got %s", first.Transaction.ID, second.Transaction.ID)
	}
	count, err := h.Store.CountConfirmedTransactionsBySignature(ctx, signature)
	if err != nil {
		t.Fatalf("CountConfirmedTransactionsBySignature: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 confirmed row, got %d", count)
	}
}

func TestDevExecuteBuy_duplicateExecuteRequestId_isIdempotent(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	ctx := context.Background()
	groupID, userID, _ := seedSwapGroup(t, h)
	const outputMint = jupiter.AAPLxMint
	const requestID = "req-dup-execute-id"
	const signature = "dup-execute-sig"
	registerHappyBuy(h.Jupiter, h.XStocks, outputMint, 1_000_000, requestID, signature)

	first, err := h.Swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{
		GroupID:    groupID,
		UserID:     userID,
		Symbol:     "AAPLx",
		USDCAmount: 1_000_000,
	})
	if err != nil {
		t.Fatalf("first DevExecuteBuy: %v", err)
	}

	// Act
	byRequestID, found, err := h.Store.GetConfirmedTransactionByExecuteRequestID(ctx, requestID)

	// Assert
	if err != nil {
		t.Fatalf("GetConfirmedTransactionByExecuteRequestID: %v", err)
	}
	if !found {
		t.Fatal("expected transaction by execute_request_id")
	}
	if byRequestID.ID != first.Transaction.ID {
		t.Fatalf("expected same transaction id")
	}
}

func TestProperty_confirmedBuyExactlyOneTransactionRowPerSignature(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	ctx := context.Background()
	groupID, userID, _ := seedSwapGroup(t, h)
	const outputMint = jupiter.AAPLxMint
	signatures := []string{"prop-sig-a", "prop-sig-b", "prop-sig-c"}

	for i, signature := range signatures {
		requestID := "req-prop-" + signature
		registerHappyBuy(h.Jupiter, h.XStocks, outputMint, int64(1_000_000+i), requestID, signature)

		_, err := h.Swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{
			GroupID:    groupID,
			UserID:     userID,
			Symbol:     "AAPLx",
			USDCAmount: int64(1_000_000 + i),
		})
		if err != nil {
			t.Fatalf("DevExecuteBuy %s: %v", signature, err)
		}

		// Act
		count, err := h.Store.CountConfirmedTransactionsBySignature(ctx, signature)

		// Assert
		if err != nil {
			t.Fatalf("CountConfirmedTransactionsBySignature: %v", err)
		}
		if count != 1 {
			t.Fatalf("signature %s: expected 1 row, got %d", signature, count)
		}
	}
}

func TestProperty_costBasisAmountMatchesFillOutput(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	ctx := context.Background()
	groupID, userID, _ := seedSwapGroup(t, h)
	const outputMint = jupiter.AAPLxMint
	const requestID = "req-cost-basis"
	const signature = "cost-basis-sig"
	const fillOutput = "750000"
	xstocks.RegisterSolanaMint(h.XStocks, "AAPLx", outputMint)
	jupiter.RegisterQuoteBuy(h.Jupiter, outputMint, 2_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: outputMint,
		InAmount:   "2000000",
		OutAmount:  fillOutput,
		RequestID:  requestID,
	})
	jupiter.RegisterBuyOrder(h.Jupiter, requestID, jupiter.BuyOrder{
		RequestID:   requestID,
		Transaction: "unsigned-buy-tx",
		InAmount:    "2000000",
		OutAmount:   fillOutput,
		InputMint:   jupiter.USDCMint,
		OutputMint:  outputMint,
	})
	jupiter.RegisterExecutePoll(h.Jupiter, requestID, []jupiter.ExecuteResult{
		{Status: jupiter.ExecuteStatusPending, Code: -1},
		{
			Status:             jupiter.ExecuteStatusSuccess,
			Code:               0,
			Signature:          signature,
			InputAmountResult:  "2000000",
			OutputAmountResult: fillOutput,
		},
	})

	// Act
	result, err := h.Swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{
		GroupID:    groupID,
		UserID:     userID,
		Symbol:     "AAPLx",
		USDCAmount: 2_000_000,
	})

	// Assert
	if err != nil {
		t.Fatalf("DevExecuteBuy: %v", err)
	}
	if !result.Transaction.CostBasisAmount.Valid || result.Transaction.CostBasisAmount.Int64 != 750_000 {
		t.Fatalf("cost_basis_amount = %v, want 750000", result.Transaction.CostBasisAmount)
	}
}

func TestSellToUSDC_happyPath_reducesXStockAndIncreasesTreasuryUsdc(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	ctx := context.Background()
	groupID, userID, treasuryAddress := seedSwapGroup(t, h)
	const inputMint = jupiter.AAPLxMint
	const requestID = "req-sell-happy"
	const signature = "sell-happy-sig"
	h.Swap.SetTreasuryBalances(treasuryAddress, TreasuryBalances{USDC: 1_000_000, XStock: 2_000_000})
	jupiter.RegisterSellQuote(h.Jupiter, inputMint, 500_000, jupiter.SellQuote{
		Routable:    true,
		InputMint:   inputMint,
		OutputMint:  jupiter.USDCMint,
		InAmount:    "500000",
		OutAmount:   "450000",
		RequestID:   requestID,
		Transaction: "unsigned-sell-tx",
	})
	jupiter.RegisterExecutePoll(h.Jupiter, requestID, []jupiter.ExecuteResult{
		{Status: jupiter.ExecuteStatusPending, Code: -1},
		{
			Status:             jupiter.ExecuteStatusSuccess,
			Code:               0,
			Signature:          signature,
			InputAmountResult:  "500000",
			OutputAmountResult: "450000",
		},
	})

	// Act
	result, err := h.Swap.SellToUSDC(ctx, SellToUSDCRequest{
		GroupID:   groupID,
		UserID:    userID,
		Symbol:    "AAPLx",
		InputMint: inputMint,
		Amount:    500_000,
	})

	// Assert
	if err != nil {
		t.Fatalf("SellToUSDC: %v", err)
	}
	if !result.Created {
		t.Fatal("expected created sell transaction")
	}
	if result.Transaction.Action != postgres.TransactionActionSell {
		t.Fatalf("action = %q, want sell", result.Transaction.Action)
	}
	balances := h.Swap.TreasuryBalancesFor(treasuryAddress)
	if balances.XStock != 1_500_000 {
		t.Fatalf("xStock balance = %d, want 1500000", balances.XStock)
	}
	if balances.USDC != 1_450_000 {
		t.Fatalf("USDC balance = %d, want 1450000", balances.USDC)
	}
}

func TestSellToUSDC_duplicateSignature_doesNotDoubleApply(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	ctx := context.Background()
	groupID, userID, treasuryAddress := seedSwapGroup(t, h)
	const inputMint = jupiter.AAPLxMint
	const requestID = "req-sell-dup"
	const signature = "sell-dup-sig"
	h.Swap.SetTreasuryBalances(treasuryAddress, TreasuryBalances{USDC: 1_000_000, XStock: 2_000_000})
	jupiter.RegisterSellQuote(h.Jupiter, inputMint, 500_000, jupiter.SellQuote{
		Routable:    true,
		InputMint:   inputMint,
		OutputMint:  jupiter.USDCMint,
		InAmount:    "500000",
		OutAmount:   "450000",
		RequestID:   requestID,
		Transaction: "unsigned-sell-tx",
	})
	jupiter.RegisterExecutePoll(h.Jupiter, requestID, []jupiter.ExecuteResult{
		{Status: jupiter.ExecuteStatusPending, Code: -1},
		{
			Status:             jupiter.ExecuteStatusSuccess,
			Code:               0,
			Signature:          signature,
			InputAmountResult:  "500000",
			OutputAmountResult: "450000",
		},
	})

	first, err := h.Swap.SellToUSDC(ctx, SellToUSDCRequest{
		GroupID:   groupID,
		UserID:    userID,
		Symbol:    "AAPLx",
		InputMint: inputMint,
		Amount:    500_000,
	})
	if err != nil {
		t.Fatalf("first SellToUSDC: %v", err)
	}

	// Act
	second, err := h.Swap.SellToUSDC(ctx, SellToUSDCRequest{
		GroupID:   groupID,
		UserID:    userID,
		Symbol:    "AAPLx",
		InputMint: inputMint,
		Amount:    500_000,
	})

	// Assert
	if err != nil {
		t.Fatalf("second SellToUSDC: %v", err)
	}
	if second.Created {
		t.Fatal("expected no second insert")
	}
	if second.Transaction.ID != first.Transaction.ID {
		t.Fatalf("expected same transaction id")
	}
	balances := h.Swap.TreasuryBalancesFor(treasuryAddress)
	if balances.XStock != 1_500_000 {
		t.Fatalf("xStock balance = %d, want 1500000", balances.XStock)
	}
	if balances.USDC != 1_450_000 {
		t.Fatalf("USDC balance = %d, want 1450000", balances.USDC)
	}
}
