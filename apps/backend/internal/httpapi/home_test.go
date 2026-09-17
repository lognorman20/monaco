package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func integrationHomeApp(t *testing.T) (*HomeHandlers, *AuthHandlers, *GroupHandlers, privy.Client, *postgres.Store, *postgres.TestIsolation) {
	t.Helper()

	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	groups := app.NewGroupService(store, privyClient)
	governance := app.NewGovernanceService(store, privyClient)
	home := app.NewHomeService(store, privyClient)
	return &HomeHandlers{Home: home}, authHandlers, &GroupHandlers{Groups: groups, Governance: governance}, privyClient, store, iso
}

func TestGET_home_missingAuth_returns401(t *testing.T) {
	t.Parallel()
	// Arrange
	homeHandlers, _, _, _, _, _ := integrationHomeApp(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/home", nil)
	rec := httptest.NewRecorder()

	// Act
	homeHandlers.HomeHandler(rec, req)

	// Assert
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGET_home_authenticated_returnsEmptyBoards(t *testing.T) {
	t.Parallel()
	// Arrange
	homeHandlers, authHandlers, _, privyClient, _, iso := integrationHomeApp(t)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "viewer", "Alfred")
	req := httptest.NewRequest(http.MethodGet, "/v1/home", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	homeHandlers.HomeHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload homeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.Groups == nil {
		t.Fatal("expected groups array in response")
	}
	if payload.People == nil {
		t.Fatal("expected people array in response")
	}
	if len(payload.Groups) != 0 || len(payload.People) != 0 {
		t.Fatalf("expected empty boards, got groups=%d people=%d", len(payload.Groups), len(payload.People))
	}
}

func TestGET_home_authenticated_returnsUnfundedGroupRow(t *testing.T) {
	t.Parallel()
	// Arrange
	homeHandlers, authHandlers, groupHandlers, privyClient, _, iso := integrationHomeApp(t)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "creator", "Alfred")

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Logan"}`))
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

	req := httptest.NewRequest(http.MethodGet, "/v1/home", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	homeHandlers.HomeHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload homeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Groups) != 1 {
		t.Fatalf("groups len = %d, want 1", len(payload.Groups))
	}
	if payload.Groups[0].Name != "Logan" {
		t.Fatalf("group name = %q, want Logan", payload.Groups[0].Name)
	}
	if payload.Groups[0].GroupID != created.GroupID {
		t.Fatalf("group id = %q, want %q", payload.Groups[0].GroupID, created.GroupID)
	}
	if payload.Groups[0].PercentReturn != nil {
		t.Fatalf("percentReturn = %v, want nil for unfunded group", payload.Groups[0].PercentReturn)
	}
	if payload.Groups[0].DollarPnL != "+0.00" {
		t.Fatalf("dollarPnl = %q, want +0.00", payload.Groups[0].DollarPnL)
	}
	if len(payload.People) != 0 {
		t.Fatalf("people len = %d, want 0 for unfunded group", len(payload.People))
	}
}

func TestGET_home_authenticated_returnsFundedGroupAndPeopleRows(t *testing.T) {
	t.Parallel()
	// Arrange
	homeHandlers, authHandlers, groupHandlers, privyClient, store, iso := integrationHomeApp(t)
	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "creator", "Alfred")

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Weekend investors"}`))
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

	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := store.IncrementPositionTx(ctx, tx, session.UserID, created.GroupID, 100_000_000, 100_000_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit position: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/home", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	homeHandlers.HomeHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload homeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Groups) != 1 {
		t.Fatalf("groups len = %d, want 1", len(payload.Groups))
	}
	if payload.Groups[0].Name != "Weekend investors" {
		t.Fatalf("group name = %q, want Weekend investors", payload.Groups[0].Name)
	}
	if payload.Groups[0].GroupID != created.GroupID {
		t.Fatalf("group id = %q, want %q", payload.Groups[0].GroupID, created.GroupID)
	}
	if len(payload.People) != 1 {
		t.Fatalf("people len = %d, want 1", len(payload.People))
	}
	if payload.People[0].DisplayName != "Alfred" {
		t.Fatalf("display name = %q, want Alfred", payload.People[0].DisplayName)
	}
}
