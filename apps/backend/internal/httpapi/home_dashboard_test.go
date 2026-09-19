package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

func TestGET_homeDashboard_missingAuth_returns401(t *testing.T) {
	t.Parallel()
	homeHandlers, _, _, _, _, _ := integrationHomeApp(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/home/dashboard", nil)
	rec := httptest.NewRecorder()
	homeHandlers.HomeDashboardHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGET_homeDashboard_authenticated_returnsEmptyDashboard(t *testing.T) {
	t.Parallel()
	// Arrange
	homeHandlers, authHandlers, _, privyClient, _, iso := integrationHomeApp(t)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "dashboard-viewer", "Alfred")
	req := httptest.NewRequest(http.MethodGet, "/v1/home/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	homeHandlers.HomeDashboardHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload homeDashboardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.NetWorthUsd != "0.00" {
		t.Fatalf("netWorthUsd = %q, want 0.00", payload.NetWorthUsd)
	}
	if payload.MyGroups == nil {
		t.Fatal("expected myGroups array")
	}
	if payload.PnlSeries1H == nil {
		t.Fatal("expected pnlSeries1H array")
	}
	if payload.MissedProposals == nil {
		t.Fatal("expected missedProposals array")
	}
	if payload.Leaderboard.Range != "ALL" {
		t.Fatalf("leaderboard range = %q, want ALL", payload.Leaderboard.Range)
	}
}

func TestGET_homeDashboard_fundedGroup_computesPotNavOncePerJoinedGroup(t *testing.T) {
	t.Parallel()
	homeHandlers, authHandlers, groupHandlers, privyClient, store, iso := integrationHomeApp(t)
	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "dashboard-dedup", "Alfred")

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Dedup cabal"}`))
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
	privy.SetTreasuryUSDCBalance(privyClient, created.TreasuryAddress, 100_000_000)

	ctx = app.HomeContextWithPotNavCache(ctx)
	req := httptest.NewRequest(http.MethodGet, "/v1/home/dashboard", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	homeHandlers.HomeDashboardHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if got := app.HomePotNavComputeCount(ctx); got != 1 {
		t.Fatalf("groupPotNavAndShares computes = %d, want 1 per joined group on dashboard", got)
	}
}

func TestGET_homeDashboard_fundedGroup_returnsMyGroupRow(t *testing.T) {
	t.Parallel()
	// Arrange
	homeHandlers, authHandlers, groupHandlers, privyClient, store, iso := integrationHomeApp(t)
	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "dashboard-funded", "Alfred")

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
	privy.SetTreasuryUSDCBalance(privyClient, created.TreasuryAddress, 100_000_000)

	req := httptest.NewRequest(http.MethodGet, "/v1/home/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	homeHandlers.HomeDashboardHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload homeDashboardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.NetWorthUsd != "100.00" {
		t.Fatalf("netWorthUsd = %q, want 100.00", payload.NetWorthUsd)
	}
	if len(payload.MyGroups) != 1 {
		t.Fatalf("myGroups len = %d, want 1", len(payload.MyGroups))
	}
	if payload.MyGroups[0].Name != "Weekend investors" {
		t.Fatalf("group name = %q, want Weekend investors", payload.MyGroups[0].Name)
	}
	if payload.MyGroups[0].EquityUsd != "100.00" {
		t.Fatalf("equityUsd = %q, want 100.00", payload.MyGroups[0].EquityUsd)
	}
	if len(payload.PnlSeries1H) != 0 {
		t.Fatalf("pnlSeries1H len = %d, want 0 without in-window nav snapshots", len(payload.PnlSeries1H))
	}
	if len(payload.Leaderboard.People) != 1 {
		t.Fatalf("leaderboard people len = %d, want 1", len(payload.Leaderboard.People))
	}
}

func TestGET_homePnlSeries_invalidRange_returns400(t *testing.T) {
	t.Parallel()
	homeHandlers, authHandlers, _, privyClient, _, iso := integrationHomeApp(t)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "pnl-range", "Alfred")
	req := httptest.NewRequest(http.MethodGet, "/v1/home/pnl-series?range=bad", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	homeHandlers.HomePnLSeriesHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGET_homeDashboard_openProposal_listsUntilVoted(t *testing.T) {
	t.Parallel()
	homeHandlers, authHandlers, groupHandlers, privyClient, store, iso := integrationHomeApp(t)
	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "dashboard-vote", "Alfred")

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Vote cabal"}`))
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
	proposal, err := store.InsertProposalTx(ctx, tx, postgres.InsertProposalParams{
		GroupID:    created.GroupID,
		ProposerID: session.UserID,
		Symbol:     "AAPL",
		Kind:       domain.ProposalKindBuy,
		UsdcMicros: 1_000_000,
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("InsertProposalTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/home/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	homeHandlers.HomeDashboardHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload homeDashboardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.MissedProposals) != 1 || payload.MissedProposals[0].ProposalID != proposal.ID {
		t.Fatalf("missed = %+v, want proposal %s", payload.MissedProposals, proposal.ID)
	}

	tx, err = store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx vote: %v", err)
	}
	if _, _, err := store.InsertVoteTx(ctx, tx, proposal.ID, session.UserID, domain.VoteYes); err != nil {
		t.Fatalf("InsertVoteTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit vote: %v", err)
	}

	rec = httptest.NewRecorder()
	homeHandlers.HomeDashboardHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status after vote = %d, want 200", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json after vote: %v", err)
	}
	if len(payload.MissedProposals) != 0 {
		t.Fatalf("missed after vote = %d, want 0", len(payload.MissedProposals))
	}
}
