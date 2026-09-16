package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func integrationCatalogApp(t *testing.T) (*CatalogHandlers, *GroupHandlers, *AuthHandlers, privy.Client) {
	t.Helper()

	authHandlers, privyClient, db := integrationApp(t)
	store := postgres.NewStore(db)
	catalog := xstocks.NewFakeCatalogSearcher()
	catalogHandlers := &CatalogHandlers{
		Store:   store,
		Privy:   privyClient,
		Catalog: catalog,
	}
	groupHandlers := &GroupHandlers{
		Groups:     app.NewGroupService(store, privyClient),
		Governance: app.NewGovernanceService(store, privyClient),
	}
	return catalogHandlers, groupHandlers, authHandlers, privyClient
}

func TestGET_assets_search_returnsBackendResolvedCatalog(t *testing.T) {
	// Arrange
	catalogHandlers, groupHandlers, authHandlers, privyClient := integrationCatalogApp(t)
	token := fixtureSessionToken()
	seedAuthenticatedUser(t, authHandlers, privyClient, token, privy.Identity{
		PrivyUserID: "did:privy:catalog",
		DisplayName: "Catalog User",
	})

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Catalog Fund"}`))
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

	xstocks.RegisterCatalogAsset(catalogHandlers.Catalog, xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple xStock",
		SolanaMint: "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+created.GroupID+"/assets?query=AAPL", nil)
	req.SetPathValue("id", created.GroupID)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	catalogHandlers.SearchAssetsHandler(rec, req)

	// Assert
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
	if payload.Assets[0].Symbol != "AAPLx" {
		t.Fatalf("symbol = %q, want AAPLx", payload.Assets[0].Symbol)
	}
	if payload.Assets[0].Name != "Apple xStock" {
		t.Fatalf("name = %q, want Apple xStock", payload.Assets[0].Name)
	}
	if payload.Assets[0].SolanaMint != "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp" {
		t.Fatalf("solanaMint = %q, want backend-resolved mint", payload.Assets[0].SolanaMint)
	}
}
