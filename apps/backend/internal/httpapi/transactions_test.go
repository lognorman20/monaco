package httpapi

import (
	"context"
	"database/sql"
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

func integrationTransactionApp(t *testing.T) (*TransactionHandlers, *GroupHandlers, *AuthHandlers, *app.SwapService, jupiter.Client, privy.Client, *sql.DB) {
	t.Helper()

	db := integrationDB(t)
	postgres.PrepareIntegrationDB(t, db)

	store := postgres.NewStore(db)
	privyClient := privy.NewFakeClient()
	jupiterClient := jupiter.NewFakeClient()
	xstocksResolver := xstocks.NewFakeResolver()
	buy := app.NewBuyService(jupiterClient, xstocksResolver)
	swap := app.NewSwapService(store, buy, jupiterClient, privyClient, app.NewFakePrivyTreasurySigner())
	swap.SetPollConfigForTests(jupiter.TestPollConfig())
	groups := app.NewGroupService(store, privyClient)
	sessions := app.NewSessionService(store, privyClient)

	return &TransactionHandlers{
		Store:   store,
		Privy:   privyClient,
		XStocks: xstocksResolver,
	}, &GroupHandlers{Groups: groups, Governance: app.NewGovernanceService(store, privyClient)}, &AuthHandlers{Sessions: sessions}, swap, jupiterClient, privyClient, db
}

func seedSwapGroupHTTP(t *testing.T, groupHandlers *GroupHandlers, authHandlers *AuthHandlers, privyClient privy.Client, db *sql.DB) (groupID, userID string) {
	t.Helper()
	token := fixtureSessionToken()
	group := seedGroup(t, groupHandlers, authHandlers, privyClient, token, privy.Identity{
		PrivyUserID: "did:privy:swap-http",
		DisplayName: "Swap HTTP",
	}, "Swap HTTP Group")
	userID = mustUserID(t, db, "did:privy:swap-http")
	return group.GroupID, userID
}

func registerHappyBuyHTTP(jupiterClient jupiter.Client, resolver xstocks.Resolver, outputMint string, requestID string, signature string) {
	xstocks.RegisterSolanaMint(resolver, "AAPLx", outputMint)
	jupiter.RegisterQuoteBuy(jupiterClient, outputMint, 1_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: outputMint,
		InAmount:   "1000000",
		OutAmount:  "500000",
		RequestID:  requestID,
	})
	jupiter.RegisterBuyOrder(jupiterClient, requestID, jupiter.BuyOrder{
		RequestID:   requestID,
		Transaction: "unsigned-buy-tx",
		InAmount:    "1000000",
		OutAmount:   "500000",
		InputMint:   jupiter.USDCMint,
		OutputMint:  outputMint,
	})
	jupiter.RegisterExecutePoll(jupiterClient, requestID, []jupiter.ExecuteResult{
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

func TestGET_transactionStatus_returnsConfirmedFill(t *testing.T) {
	// Arrange
	transactionHandlers, groupHandlers, authHandlers, swap, jupiterClient, privyClient, db := integrationTransactionApp(t)
	ctx := context.Background()
	groupID, userID := seedSwapGroupHTTP(t, groupHandlers, authHandlers, privyClient, db)
	const outputMint = jupiter.AAPLxMint
	const requestID = "req-http-status"
	const signature = "http-status-sig"
	registerHappyBuyHTTP(jupiterClient, transactionHandlers.XStocks, outputMint, requestID, signature)

	buyResult, err := swap.DevExecuteBuy(ctx, app.DevExecuteBuyRequest{
		GroupID:    groupID,
		UserID:     userID,
		Symbol:     "AAPLx",
		USDCAmount: 1_000_000,
	})
	if err != nil {
		t.Fatalf("DevExecuteBuy: %v", err)
	}

	token := fixtureSessionToken()
	req := httptest.NewRequest(http.MethodGet, "/v1/transactions/"+buyResult.Transaction.ID, nil)
	req.SetPathValue("id", buyResult.Transaction.ID)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	transactionHandlers.GetTransactionHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload getTransactionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.Status != postgres.TransactionStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", payload.Status)
	}
	if payload.TxSignature != signature {
		t.Fatalf("txSignature = %q, want %q", payload.TxSignature, signature)
	}
	if payload.CostBasisPrice != 1_000_000 {
		t.Fatalf("costBasisPrice = %d, want 1000000", payload.CostBasisPrice)
	}
	if payload.CostBasisAmount != 500_000 {
		t.Fatalf("costBasisAmount = %d, want 500000", payload.CostBasisAmount)
	}
}

func TestGET_treasuryTokenBalances_reflectsPostBuyHoldings(t *testing.T) {
	// Arrange
	transactionHandlers, groupHandlers, authHandlers, swap, jupiterClient, privyClient, db := integrationTransactionApp(t)
	ctx := context.Background()
	groupID, userID := seedSwapGroupHTTP(t, groupHandlers, authHandlers, privyClient, db)
	const outputMint = jupiter.AAPLxMint
	registerHappyBuyHTTP(jupiterClient, transactionHandlers.XStocks, outputMint, "req-http-tokens", "http-tokens-sig")

	_, err := swap.DevExecuteBuy(ctx, app.DevExecuteBuyRequest{
		GroupID:    groupID,
		UserID:     userID,
		Symbol:     "AAPLx",
		USDCAmount: 1_000_000,
	})
	if err != nil {
		t.Fatalf("DevExecuteBuy: %v", err)
	}

	token := fixtureSessionToken()
	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+groupID+"/treasury/tokens", nil)
	req.SetPathValue("id", groupID)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	transactionHandlers.GetTreasuryTokenBalancesHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload treasuryTokenBalancesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Tokens) != 1 {
		t.Fatalf("tokens len = %d, want 1", len(payload.Tokens))
	}
	if payload.Tokens[0].Symbol != "AAPLx" {
		t.Fatalf("symbol = %q, want AAPLx", payload.Tokens[0].Symbol)
	}
	if payload.Tokens[0].Amount != 500_000 {
		t.Fatalf("amount = %d, want 500000", payload.Tokens[0].Amount)
	}
}

func TestGET_costBasisBySymbol_returnsFillDerivedBasis(t *testing.T) {
	// Arrange
	transactionHandlers, groupHandlers, authHandlers, swap, jupiterClient, privyClient, db := integrationTransactionApp(t)
	ctx := context.Background()
	groupID, userID := seedSwapGroupHTTP(t, groupHandlers, authHandlers, privyClient, db)
	const outputMint = jupiter.AAPLxMint
	registerHappyBuyHTTP(jupiterClient, transactionHandlers.XStocks, outputMint, "req-http-basis", "http-basis-sig")

	_, err := swap.DevExecuteBuy(ctx, app.DevExecuteBuyRequest{
		GroupID:    groupID,
		UserID:     userID,
		Symbol:     "AAPLx",
		USDCAmount: 1_000_000,
	})
	if err != nil {
		t.Fatalf("DevExecuteBuy: %v", err)
	}

	token := fixtureSessionToken()
	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+groupID+"/cost-basis/AAPLx", nil)
	req.SetPathValue("id", groupID)
	req.SetPathValue("symbol", "AAPLx")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	transactionHandlers.GetCostBasisBySymbolHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload costBasisBySymbolResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.Symbol != "AAPLx" {
		t.Fatalf("symbol = %q, want AAPLx", payload.Symbol)
	}
	if payload.CostBasisPrice != 1_000_000 {
		t.Fatalf("costBasisPrice = %d, want 1000000", payload.CostBasisPrice)
	}
	if payload.CostBasisAmount != 500_000 {
		t.Fatalf("costBasisAmount = %d, want 500000", payload.CostBasisAmount)
	}
}
