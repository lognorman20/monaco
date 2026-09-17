package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func integrationTransactionHandlers(t *testing.T) (*TransactionHandlers, *AuthHandlers, privy.Client, jupiter.Client, *postgres.TestIsolation) {
	t.Helper()

	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	jupiterClient := jupiter.NewFakeClient()
	xstocksResolver := xstocks.NewFakeResolver()
	buy := app.NewBuyService(jupiterClient, xstocksResolver)
	signer := app.NewFakePrivyTreasurySigner()
	swap := app.NewSwapService(store, buy, jupiterClient, privyClient, signer, "")
	return &TransactionHandlers{
		Store:   store,
		Privy:   privyClient,
		XStocks: xstocksResolver,
		Swap:    swap,
	}, authHandlers, privyClient, jupiterClient, iso
}

func TestRetryTransactionHandler_failedBuy_returnsConfirmed(t *testing.T) {
	handlers, authHandlers, privyClient, jupiterClient, iso := integrationTransactionHandlers(t)
	ctx := context.Background()
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "retry-http", "Retry HTTP")

	governance := app.NewGovernanceService(handlers.Store, handlers.Privy)
	created, err := governance.CreateGroupWithRules(ctx, string(token), "Retry Club "+iso.Suffix(), app.DefaultGroupRules(), "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	trackCreatedGroup(iso, created.GroupID)

	const usdcAmount int64 = 2_000_000
	requestID := "req-retry-http-" + iso.Suffix()
	signature := "sig-retry-http-" + iso.Suffix()
	xstocks.RegisterSolanaMint(handlers.XStocks, "AAPLx", jupiter.AAPLxMint)
	jupiter.RegisterQuoteBuy(jupiterClient, jupiter.AAPLxMint, usdcAmount, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "2000000",
		OutAmount:  "1000000",
		RequestID:  requestID,
	})
	jupiter.RegisterBuyOrder(jupiterClient, requestID, jupiter.BuyOrder{
		RequestID:   requestID,
		Transaction: "unsigned-buy-tx",
		InAmount:    "2000000",
		OutAmount:   "1000000",
		InputMint:   jupiter.USDCMint,
		OutputMint:  jupiter.AAPLxMint,
	})
	jupiter.RegisterExecutePoll(jupiterClient, requestID, []jupiter.ExecuteResult{
		{Status: jupiter.ExecuteStatusPending, Code: -1},
		{
			Status:             jupiter.ExecuteStatusSuccess,
			Code:               0,
			Signature:          signature,
			InputAmountResult:  "2000000",
			OutputAmountResult: "1000000",
		},
	})

	treasury, err := privyClient.EnsureTreasury(ctx, privy.GroupID(created.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	handlers.Swap.SetTreasuryBalances(treasury.SolanaAddress, app.TreasuryBalances{USDC: 5_000_000})
	privy.SetTreasuryUSDCBalance(privyClient, treasury.SolanaAddress, 5_000_000)

	failed, err := handlers.Store.InsertFailedTransaction(ctx, created.GroupID, postgres.TransactionActionBuy, jupiter.USDCMint, jupiter.AAPLxMint, usdcAmount, "req-failed-"+iso.Suffix())
	if err != nil {
		t.Fatalf("insert failed buy: %v", err)
	}

	retryReq := httptest.NewRequest(http.MethodPost, "/v1/transactions/"+failed.ID+"/retry", nil)
	retryReq.SetPathValue("id", failed.ID)
	retryReq.Header.Set("Authorization", "Bearer "+string(token))
	retryRec := httptest.NewRecorder()
	handlers.RetryTransactionHandler(retryRec, retryReq)

	if retryRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", retryRec.Code, retryRec.Body.String())
	}

	var payload getTransactionResponse
	if err := json.Unmarshal(retryRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.Status != postgres.TransactionStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", payload.Status)
	}
	if payload.TransactionID == failed.ID {
		t.Fatal("expected new transaction id after retry")
	}
}

func TestRetryTransactionHandler_confirmedBuy_returns409(t *testing.T) {
	handlers, authHandlers, privyClient, _, iso := integrationTransactionHandlers(t)
	ctx := context.Background()
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "retry-409", "Retry 409")

	governance := app.NewGovernanceService(handlers.Store, handlers.Privy)
	created, err := governance.CreateGroupWithRules(ctx, string(token), "Retry 409 Club "+iso.Suffix(), app.DefaultGroupRules(), "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	trackCreatedGroup(iso, created.GroupID)

	confirmed, _, err := handlers.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          created.GroupID,
		Amount:           1_000_000,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      "sig-confirmed-" + iso.Suffix(),
		ExecuteRequestID: "req-confirmed-" + iso.Suffix(),
		CostBasisPrice:   1_000_000,
		CostBasisAmount:  500_000,
	})
	if err != nil {
		t.Fatalf("confirm buy: %v", err)
	}

	retryReq := httptest.NewRequest(http.MethodPost, "/v1/transactions/"+confirmed.ID+"/retry", nil)
	retryReq.SetPathValue("id", confirmed.ID)
	retryReq.Header.Set("Authorization", "Bearer "+string(token))
	retryRec := httptest.NewRecorder()
	handlers.RetryTransactionHandler(retryRec, retryReq)

	if retryRec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", retryRec.Code, retryRec.Body.String())
	}
}
