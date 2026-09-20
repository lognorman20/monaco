package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/apps/backend/internal/b20"
)

func integrationTransactionHandlers(t *testing.T) (*TransactionHandlers, *AuthHandlers, wallets.Client, dex.Client, *postgres.TestIsolation) {
	t.Helper()

	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	jupiterClient := dex.NewFakeClient()
	xstocksResolver := b20.NewFakeCatalog()
	buy := app.NewBuyService(jupiterClient, xstocksResolver)
	catalog := b20.NewFakeCatalog()
	b20.RegisterCatalogAsset(catalog, b20.Asset{
		Symbol:     "AAPLx",
		Name:       "Apple",
		TokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb",
	})
	symbols := app.NewSymbolResolver(catalog)
	signer := app.NewFakePrivyTreasurySigner()
	swap := app.NewSwapService(store, buy, jupiterClient, privyClient, signer, "", symbols)
	return &TransactionHandlers{
		Store:   store,
		Privy:   privyClient,
		XStocks: xstocksResolver,
		Swap:    swap,
		Symbols: symbols,
	}, authHandlers, privyClient, jupiterClient, iso
}

func TestRetryTransactionHandler_failedBuy_returnsConfirmed(t *testing.T) {
	handlers, authHandlers, privyClient, jupiterClient, iso := integrationTransactionHandlers(t)
	ctx := context.Background()
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "retry-http", "Retry HTTP")

	governance := app.NewGovernanceService(handlers.Store, handlers.Privy)
	created, err := governance.CreateGroupWithRules(ctx, string(token), "Retry Club "+iso.Suffix(), app.DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	trackCreatedGroup(iso, created.GroupID)

	const usdcAmount int64 = 2_000_000
	requestID := "req-retry-http-" + iso.Suffix()
	signature := "sig-retry-http-" + iso.Suffix()
	b20.RegisterTokenAddress(handlers.XStocks, "AAPLx", "0xb200000000000000000000c2e324d24d7eecd1fb")
	jupiter.RegisterQuoteBuy(jupiterClient, "0xb200000000000000000000c2e324d24d7eecd1fb", usdcAmount, jupiter.BuyQuote{
		Routable:   true,
		InputToken:  evm.USDCAddress,
		OutputToken: "0xb200000000000000000000c2e324d24d7eecd1fb",
		InAmount:   "2000000",
		OutAmount:  "1000000",
		RequestID:  requestID,
	})
	jupiter.RegisterBuyOrder(jupiterClient, requestID, jupiter.BuyOrder{
		RequestID:   requestID,
		Transaction: "unsigned-buy-tx",
		InAmount:    "2000000",
		OutAmount:   "1000000",
		InputToken:   evm.USDCAddress,
		OutputToken:  "0xb200000000000000000000c2e324d24d7eecd1fb",
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

	treasury, err := privyClient.EnsureTreasury(ctx, wallets.GroupID(created.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	handlers.Swap.SetTreasuryBalances(treasury.Address, app.TreasuryBalances{USDC: 5_000_000})
	wallets.SetTreasuryUSDCBalance(privyClient, treasury.Address, 5_000_000)

	failed, err := handlers.Store.InsertFailedTransaction(ctx, created.GroupID, postgres.TransactionActionBuy, evm.USDCAddress, "0xb200000000000000000000c2e324d24d7eecd1fb", usdcAmount, "req-failed-"+iso.Suffix())
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

func TestGetTransactionHandler_returnsFullDetail(t *testing.T) {
	handlers, authHandlers, privyClient, _, iso := integrationTransactionHandlers(t)
	ctx := context.Background()
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "get-tx", "Get Tx")

	governance := app.NewGovernanceService(handlers.Store, handlers.Privy)
	created, err := governance.CreateGroupWithRules(ctx, string(token), "Get Tx Club "+iso.Suffix(), app.DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	trackCreatedGroup(iso, created.GroupID)

	confirmed, _, err := handlers.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          created.GroupID,
		Amount:           2_000_000,
		InputToken:        evm.USDCAddress,
		OutputToken:       "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash:      "sig-get-tx-" + iso.Suffix(),
		ExecuteRequestID: "req-get-tx-" + iso.Suffix(),
		CostBasisPrice:   2_000_000,
		CostBasisAmount:  1_000_000,
	})
	if err != nil {
		t.Fatalf("confirm buy: %v", err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/transactions/"+confirmed.ID, nil)
	getReq.SetPathValue("id", confirmed.ID)
	getReq.Header.Set("Authorization", "Bearer "+string(token))
	getRec := httptest.NewRecorder()
	handlers.GetTransactionHandler(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", getRec.Code, getRec.Body.String())
	}

	var payload getTransactionResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.TransactionID != confirmed.ID {
		t.Fatalf("transactionId = %q, want %q", payload.TransactionID, confirmed.ID)
	}
	if payload.Action != postgres.TransactionActionBuy {
		t.Fatalf("action = %q, want buy", payload.Action)
	}
	if payload.Status != postgres.TransactionStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", payload.Status)
	}
	if payload.AmountMicros != 2_000_000 {
		t.Fatalf("amountMicros = %d, want 2000000", payload.AmountMicros)
	}
	if payload.TxHash == "" || payload.ExecuteRequestID == "" {
		t.Fatalf("expected txHash and executeRequestId, got sig=%q req=%q", payload.TxHash, payload.ExecuteRequestID)
	}
	if payload.CreatedAt == "" || payload.ConfirmedAt == "" {
		t.Fatalf("expected timestamps, got created=%q confirmed=%q", payload.CreatedAt, payload.ConfirmedAt)
	}
	if payload.InputSymbol != "USDC" || payload.OutputSymbol != "AAPLx" {
		t.Fatalf("symbols = %q / %q, want USDC / AAPLx", payload.InputSymbol, payload.OutputSymbol)
	}
}

func TestRetryTransactionHandler_confirmedBuy_returns409(t *testing.T) {
	handlers, authHandlers, privyClient, _, iso := integrationTransactionHandlers(t)
	ctx := context.Background()
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "retry-409", "Retry 409")

	governance := app.NewGovernanceService(handlers.Store, handlers.Privy)
	created, err := governance.CreateGroupWithRules(ctx, string(token), "Retry 409 Club "+iso.Suffix(), app.DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	trackCreatedGroup(iso, created.GroupID)

	confirmed, _, err := handlers.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          created.GroupID,
		Amount:           1_000_000,
		InputToken:        evm.USDCAddress,
		OutputToken:       "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash:      "sig-confirmed-" + iso.Suffix(),
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
