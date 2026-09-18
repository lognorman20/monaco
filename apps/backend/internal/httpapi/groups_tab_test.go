package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func integrationGroupsTabApp(t *testing.T) (*GroupsTabHandlers, *AuthHandlers, *GroupHandlers, privy.Client, *postgres.Store, *postgres.TestIsolation) {
	t.Helper()
	homeHandlers, authHandlers, groupHandlers, privyClient, store, iso := integrationHomeApp(t)
	return &GroupsTabHandlers{Home: homeHandlers.Home}, authHandlers, groupHandlers, privyClient, store, iso
}

func TestGET_groups_search_missingAuth_returns401(t *testing.T) {
	t.Parallel()
	// Arrange
	handlers, _, _, _, _, _ := integrationGroupsTabApp(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/groups/search?q=alpha", nil)
	rec := httptest.NewRecorder()

	// Act
	handlers.SearchGroupsHandler(rec, req)

	// Assert
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGET_groups_search_returnsMatchingClub(t *testing.T) {
	t.Parallel()
	// Arrange
	handlers, authHandlers, groupHandlers, privyClient, _, iso := integrationGroupsTabApp(t)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "searcher", "Search User")

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Alpha Investors"}`))
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

	req := httptest.NewRequest(http.MethodGet, "/v1/groups/search?q=alpha", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	handlers.SearchGroupsHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Groups []groupDiscoveryRowResponse `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Groups) != 1 {
		t.Fatalf("groups len = %d, want 1", len(payload.Groups))
	}
	if payload.Groups[0].GroupID != created.GroupID {
		t.Fatalf("groupId = %q, want %q", payload.Groups[0].GroupID, created.GroupID)
	}
	if payload.Groups[0].Name != "Alpha Investors" {
		t.Fatalf("name = %q, want Alpha Investors", payload.Groups[0].Name)
	}
	if !payload.Groups[0].IsJoined {
		t.Fatal("expected isJoined=true for creator")
	}
	if payload.Groups[0].JoinMode != "open" {
		t.Fatalf("joinMode = %q, want open", payload.Groups[0].JoinMode)
	}
}

func TestGET_groups_leaderboard_returnsRankedFundedClub(t *testing.T) {
	t.Parallel()
	// Arrange
	handlers, authHandlers, groupHandlers, privyClient, store, iso := integrationGroupsTabApp(t)
	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "leader", "Leader")

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Leaderboard Club"}`))
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

	req := httptest.NewRequest(http.MethodGet, "/v1/groups/leaderboard", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	handlers.GroupLeaderboardHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Groups []groupLeaderboardRowResponse `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	var row *groupLeaderboardRowResponse
	for i := range payload.Groups {
		if payload.Groups[i].GroupID == created.GroupID {
			row = &payload.Groups[i]
			break
		}
	}
	if row == nil {
		t.Fatalf("expected funded club %q on leaderboard, got %d rows", created.GroupID, len(payload.Groups))
	}
	if row.Rank < 1 {
		t.Fatalf("rank = %d, want >= 1", row.Rank)
	}
	if row.PotValueUsd != "100.00" {
		t.Fatalf("potValueUsd = %q, want 100.00", row.PotValueUsd)
	}
}

func TestGET_groups_pnlHistory_returnsSnapshotSeries(t *testing.T) {
	t.Parallel()
	// Arrange
	handlers, authHandlers, groupHandlers, privyClient, store, iso := integrationGroupsTabApp(t)
	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "history", "History User")

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Chart Club"}`))
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
	if err := store.WriteNavSnapshotOnDepositConfirmTx(ctx, tx, created.GroupID, 100_000_000); err != nil {
		t.Fatalf("WriteNavSnapshotOnDepositConfirmTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit deposit: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+created.GroupID+"/pnl-history", nil)
	req.SetPathValue("id", created.GroupID)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	handlers.GroupPnLHistoryHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload groupPnLHistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.GroupID != created.GroupID {
		t.Fatalf("groupId = %q, want %q", payload.GroupID, created.GroupID)
	}
	if payload.Name != "Chart Club" {
		t.Fatalf("name = %q, want Chart Club", payload.Name)
	}
	if len(payload.Points) != 1 {
		t.Fatalf("points len = %d, want 1", len(payload.Points))
	}
	if payload.Points[0].PotValueUsd != "100.00" {
		t.Fatalf("potValueUsd = %q, want 100.00", payload.Points[0].PotValueUsd)
	}
	if payload.Points[0].DollarPnL != "+0.00" {
		t.Fatalf("dollarPnl = %q, want +0.00", payload.Points[0].DollarPnL)
	}
}
