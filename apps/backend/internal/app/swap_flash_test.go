package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/flash"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// useFlashProvider swaps the harness onto a fake Flash venue and returns its client.
func useFlashProvider(t *testing.T, h executeOnPassHarness) flash.Client {
	t.Helper()
	signer, ok := NewFakePrivyTreasurySigner().(flash.TreasurySigner)
	if !ok {
		t.Fatal("fake treasury signer must sign messages for Flash")
	}
	client := flash.NewFakeClient()
	h.App.Swap.SetSwapProvider(flash.NewSwapProvider(client, signer, nil, flash.ProviderConfig{}))
	h.App.Swap.SetPollConfigForTests(flash.TestPollConfig())
	return client
}

func flashBuyQuote(quoteID string, usdcMicros int64) flash.Quote {
	return flash.Quote{
		QuoteID:      quoteID,
		OrderMessage: fmt.Sprintf("DFS|m=%s|t=%d|h=test", jupiter.USDCMint, usdcMicros),
		Nonce:        "1",
		Deadline:     "4102444800",
	}
}

func TestExecuteOnPass_flashProvider_confirmsBuyFromFlashFill(t *testing.T) {
	// Arrange
	h := integrationExecuteOnPassApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h, "flash-buy")
	client := useFlashProvider(t, h)
	quoteID := testRequestID(h.App.ISO, "flash-buy-quote")
	signature := testTxSignature(h.App.ISO, "flash-buy-fill")
	flash.RegisterQuotes(client, "buy", jupiter.AAPLxMint, "2", flashBuyQuote(quoteID, 2_000_000))
	flash.RegisterOrderPoll(client, flash.FakeOrderID(quoteID),
		flash.Order{Status: flash.OrderStatusAccepted},
		flash.Order{Status: flash.OrderStatusFilled, TransactionID: signature, FilledTargetAmount: "0.00594433", FilledContraAmount: "2"},
	)

	// Act
	result, err := h.ExecutePass.ExecuteOnPass(ctx, passed)

	// Assert
	if err != nil {
		t.Fatalf("ExecuteOnPass: %v", err)
	}
	if h.App.Swap.SwapProviderName() != swapprovider.NameFlash {
		t.Fatalf("provider = %q, want flash", h.App.Swap.SwapProviderName())
	}
	row := result.Transaction
	if row.Status != "confirmed" || row.TxSignature.String != signature {
		t.Fatalf("unexpected transaction %+v", row)
	}
	if row.ExecuteRequestID.String != flash.FakeOrderID(quoteID) {
		t.Fatalf("execute_request_id = %q, want the Flash order id", row.ExecuteRequestID.String)
	}
	if row.CostBasisPrice.Int64 != 2_000_000 || row.CostBasisAmount.Int64 != 594_433 {
		t.Fatalf("cost basis = %d/%d, want 2000000/594433", row.CostBasisPrice.Int64, row.CostBasisAmount.Int64)
	}
	if got := len(flash.Submissions(client)); got != 1 {
		t.Fatalf("flash submissions = %d, want 1", got)
	}
}

func TestSwapService_flashProvider_rejectedOrder_marksTransactionFailed(t *testing.T) {
	// Arrange
	h := integrationExecuteOnPassApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h, "flash-rejected")
	client := useFlashProvider(t, h)
	quoteID := testRequestID(h.App.ISO, "flash-rejected-quote")
	orderID := flash.FakeOrderID(quoteID)
	flash.RegisterQuotes(client, "buy", jupiter.AAPLxMint, "2", flashBuyQuote(quoteID, 2_000_000))
	flash.RegisterOrderPoll(client, orderID, flash.Order{Status: flash.OrderStatusRejected, CloseReason: "REASON_UNSPECIFIED"})

	// Act
	_, err := h.App.Swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{
		GroupID:    passed.GroupID,
		UserID:     passed.ProposerID,
		Symbol:     "AAPLx",
		USDCAmount: 2_000_000,
	})

	// Assert
	if !errors.Is(err, flash.ErrOrderRejected) {
		t.Fatalf("DevExecuteBuy error = %v, want ErrOrderRejected", err)
	}
	row, found, err := h.App.Store.GetTransactionByExecuteRequestID(ctx, orderID)
	if err != nil || !found {
		t.Fatalf("GetTransactionByExecuteRequestID: found=%v err=%v", found, err)
	}
	if row.Status != "failed" || row.Action != postgres.TransactionActionBuy {
		t.Fatalf("transaction = %+v, want failed buy", row)
	}
}

func TestSwapService_flashProvider_unknownMint_returnsQuoteNotRoutable(t *testing.T) {
	// Arrange
	h := integrationExecuteOnPassApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h, "flash-noroute")
	client := useFlashProvider(t, h)
	xstocks.RegisterSolanaMint(h.App.XStocks, "AAPLx", jupiter.AAPLxMint)
	flash.RegisterQuoteError(client, "sell", jupiter.AAPLxMint, "1", fmt.Errorf("%w: asset not found", flash.ErrNoRoute))

	// Act
	_, err := h.App.Swap.SellToUSDC(ctx, SellToUSDCRequest{
		GroupID:   passed.GroupID,
		UserID:    passed.ProposerID,
		Symbol:    "AAPLx",
		InputMint: jupiter.AAPLxMint,
		Amount:    100_000_000,
	})

	// Assert
	if !errors.Is(err, ErrQuoteNotRoutable) {
		t.Fatalf("SellToUSDC error = %v, want ErrQuoteNotRoutable", err)
	}
}
