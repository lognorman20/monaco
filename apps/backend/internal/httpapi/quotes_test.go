package httpapi

import (
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/apps/backend/internal/b20"
)

func integrationQuotesApp(t *testing.T) (*QuoteHandlers, *GroupHandlers, *AuthHandlers, wallets.Client, dex.Client, b20.Catalog, *postgres.TestIsolation) {
	t.Helper()

	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	jupiterClient := dex.NewFakeClient()
	xstocksResolver := b20.NewFakeCatalog()
	buy := app.NewBuyService(jupiterClient, xstocksResolver)
	quoteHandlers := &QuoteHandlers{
		Store: store,
		Privy: privyClient,
		Buy:   buy,
	}
	deposits := app.NewDepositService(store, privyClient, nil, app.NewSymbolResolver(b20.NewFakeCatalog()))
	home := app.NewHomeService(store, privyClient, nil, deposits, app.NewSymbolResolver(b20.NewFakeCatalog()))
	governance := app.NewGovernanceService(store, privyClient)
	governance.SetBuyService(buy)
	governance.SetHomeService(home)
	groupHandlers := &GroupHandlers{
		Groups:     app.NewGroupService(store, privyClient),
		Governance: governance,
		Home:       home,
	}
	return quoteHandlers, groupHandlers, authHandlers, privyClient, jupiterClient, xstocksResolver, iso
}

func createGroupForQuotes(t *testing.T, iso *postgres.TestIsolation, groupHandlers *GroupHandlers, authHandlers *AuthHandlers, privyClient wallets.Client) (auth.AccessToken, string, string) {
	t.Helper()

	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "quotes-user", "Quotes User")

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Quotes Fund"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(token))
	createRec := httptest.NewRecorder()
	groupHandlers.CreateGroupHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create group status = %d, want 200; body = %s", createRec.Code, createRec.Body.String())
	}

	var created createGroupResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create group json: %v", err)
	}
	trackCreatedGroup(iso, created.GroupID)
	wallets.SetTreasuryUSDCBalance(privyClient, created.TreasuryAddress, 100_000_000)

	return token, created.GroupID, session.UserID
}

func TestPOST_quotes_noRoute_returnsRoutableFalse(t *testing.T) {
	t.Parallel()
	// Arrange
	quoteHandlers, groupHandlers, authHandlers, privyClient, _, resolver, iso := integrationQuotesApp(t)
	token, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, privyClient)
	b20.RegisterTokenAddress(resolver, "AAPLx", "0xb200000000000000000000c2e324d24d7eecd1fb")

	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/quotes", strings.NewReader(`{"symbol":"AAPLx","usdc":1000000}`))
	req.SetPathValue("id", groupID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	quoteHandlers.QuoteHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload quoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.Routable {
		t.Fatal("expected routable=false")
	}
	if payload.Symbol != "AAPLx" {
		t.Fatalf("symbol = %q, want AAPLx", payload.Symbol)
	}
	if payload.USDCMicros != "1000000" {
		t.Fatalf("usdcMicros = %q, want 1000000", payload.USDCMicros)
	}
}

func TestPOST_proposals_noRoute_refusesBeforeInsert(t *testing.T) {
	t.Parallel()
	// Arrange
	quoteHandlers, groupHandlers, authHandlers, privyClient, _, resolver, iso := integrationQuotesApp(t)
	token, groupID, userID := createGroupForQuotes(t, iso, groupHandlers, authHandlers, privyClient)
	b20.RegisterTokenAddress(resolver, "AAPLx", "0xb200000000000000000000c2e324d24d7eecd1fb")

	// Act
	ok, err := ProposalQuoteOK(context.Background(), quoteHandlers.Buy, ProposalQuoteInput{
		GroupID:    groupID,
		UserID:     userID,
		Symbol:     "AAPLx",
		USDCAmount: 1_000_000,
	})

	// Assert
	if err != nil {
		t.Fatalf("ProposalQuoteOK: %v", err)
	}
	if ok {
		t.Fatal("expected proposal create to be refused before insert when quote is not routable")
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/quotes", strings.NewReader(`{"symbol":"AAPLx","usdc":1000000}`))
	req.SetPathValue("id", groupID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	quoteHandlers.QuoteHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("quote status = %d, want 200", rec.Code)
	}
	var payload quoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode quote json: %v", err)
	}
	if payload.Routable {
		t.Fatal("expected routable=false from quotes endpoint")
	}
}
