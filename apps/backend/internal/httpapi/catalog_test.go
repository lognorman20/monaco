package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func integrationCatalogApp(t *testing.T) (*CatalogHandlers, *GroupHandlers, *AuthHandlers, privy.Client, *postgres.TestIsolation) {
	t.Helper()

	quoteHandlers, groupHandlers, authHandlers, privyClient, _, _, iso := integrationQuotesApp(t)
	catalog := xstocks.NewFakeCatalogSearcher()
	return &CatalogHandlers{
		Store:   quoteHandlers.Store,
		Privy:   quoteHandlers.Privy,
		Catalog: catalog,
	}, groupHandlers, authHandlers, privyClient, iso
}

func TestGET_assets_paginatesCatalogResults(t *testing.T) {
	t.Parallel()

	catalogHandlers, groupHandlers, authHandlers, _, iso := integrationCatalogApp(t)
	token, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, catalogHandlers.Privy)
	for i := 0; i < 3; i++ {
		xstocks.RegisterCatalogAsset(catalogHandlers.Catalog, xstocks.CatalogAsset{
			Symbol:     "SYM" + string(rune('A'+i)) + "x",
			Name:       "Stock " + string(rune('A'+i)),
			SolanaMint: "Mint" + string(rune('A'+i)),
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+groupID+"/assets?limit=2&offset=0", nil)
	req.SetPathValue("id", groupID)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	catalogHandlers.SearchAssetsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload searchAssetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Assets) != 2 {
		t.Fatalf("assets len = %d, want 2", len(payload.Assets))
	}
	if !payload.HasMore {
		t.Fatal("expected hasMore=true")
	}
}

func TestGET_assets_allowsNonCreatorMember(t *testing.T) {
	t.Parallel()

	catalogHandlers, groupHandlers, authHandlers, privyClient, iso := integrationCatalogApp(t)
	_, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, catalogHandlers.Privy)
	_, joinerToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "catalog-joiner", "Catalog Joiner")
	joinReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/join", strings.NewReader(`{}`))
	joinReq.SetPathValue("id", groupID)
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+string(joinerToken))
	joinRec := httptest.NewRecorder()
	groupHandlers.JoinGroupHandler(joinRec, joinReq)
	if joinRec.Code != http.StatusNoContent {
		t.Fatalf("join status = %d, want 204; body = %s", joinRec.Code, joinRec.Body.String())
	}

	xstocks.RegisterCatalogAsset(catalogHandlers.Catalog, xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple",
		SolanaMint: "MintAAPL",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+groupID+"/assets?query=AAPL&limit=5", nil)
	req.SetPathValue("id", groupID)
	req.Header.Set("Authorization", "Bearer "+string(joinerToken))
	rec := httptest.NewRecorder()
	catalogHandlers.SearchAssetsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
}

func TestGET_assets_includesRoutableField(t *testing.T) {
	t.Parallel()

	catalogHandlers, groupHandlers, authHandlers, _, iso := integrationCatalogApp(t)
	token, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, catalogHandlers.Privy)
	xstocks.RegisterCatalogAsset(catalogHandlers.Catalog, xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple",
		SolanaMint: "MintAAPL",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+groupID+"/assets?limit=5", nil)
	req.SetPathValue("id", groupID)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	catalogHandlers.SearchAssetsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload searchAssetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Assets) != 1 {
		t.Fatalf("assets len = %d, want 1", len(payload.Assets))
	}
	if !payload.Assets[0].Routable {
		t.Fatal("expected routable=true when no prober is configured on fake catalog")
	}
}
