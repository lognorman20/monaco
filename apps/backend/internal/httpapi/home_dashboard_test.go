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
	wantExpiresAt := `"expiresAt":"` + proposal.ExpiresAt.UTC().Format(time.RFC3339) + `"`
	if !strings.Contains(rec.Body.String(), wantExpiresAt) {
		t.Fatalf("body = %s, want it to contain %s", rec.Body.String(), wantExpiresAt)
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

func TestMapHomePnLSeriesPoints_nonUTCSubSecondInput_emitsUTCRFC3339(t *testing.T) {
	t.Parallel()
	// Arrange: a New York wall-clock time with nanoseconds, as a store row could carry it.
	newYork := time.FixedZone("EDT", -4*60*60)
	points := []app.HomePnLSeriesPoint{{
		TS:        time.Date(2026, 9, 17, 18, 30, 5, 123456789, newYork),
		EquityUsd: "130.00",
		DollarPnL: "+0.00",
	}}

	// Act
	body, err := json.Marshal(homePnLSeriesResponse{Points: mapHomePnLSeriesPoints(points)})

	// Assert
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"points":[{"ts":"2026-09-17T22:30:05Z","equityUsd":"130.00","dollarPnl":"+0.00"}]}`
	if string(body) != want {
		t.Fatalf("body = %s, want %s", body, want)
	}
}

func TestMapHomeMissedProposals_nonUTCSubSecondInput_emitsUTCRFC3339(t *testing.T) {
	t.Parallel()
	// Arrange: the offset pushes both times onto the previous UTC date.
	tokyo := time.FixedZone("JST", 9*60*60)
	rows := []app.HomeMissedProposalRow{{
		GroupID:    "g1",
		GroupName:  "Vote cabal",
		ProposalID: "p1",
		Symbol:     "AAPL",
		Status:     "open",
		CreatedAt:  time.Date(2026, 9, 18, 5, 0, 0, 999999999, tokyo),
		ExpiresAt:  time.Date(2026, 9, 19, 5, 0, 0, 1, tokyo),
	}}

	// Act
	body, err := json.Marshal(homeMissedProposalsResponse{Proposals: mapHomeMissedProposals(rows)})

	// Assert
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Proposals []map[string]any `json:"proposals"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(decoded.Proposals) != 1 {
		t.Fatalf("proposals len = %d, want 1", len(decoded.Proposals))
	}
	if got := decoded.Proposals[0]["createdAt"]; got != "2026-09-17T20:00:00Z" {
		t.Fatalf("createdAt = %v, want 2026-09-17T20:00:00Z", got)
	}
	if got := decoded.Proposals[0]["expiresAt"]; got != "2026-09-18T20:00:00Z" {
		t.Fatalf("expiresAt = %v, want 2026-09-18T20:00:00Z", got)
	}
}
