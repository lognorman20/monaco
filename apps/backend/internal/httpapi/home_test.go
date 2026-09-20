package httpapi

import (
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

func integrationHomeApp(t *testing.T) (*HomeHandlers, *AuthHandlers, *GroupHandlers, wallets.Client, *postgres.Store, *postgres.TestIsolation) {
	t.Helper()
	return integrationHomeAppWithPyth(t, nil)
}

func findHomeGroupRow(t *testing.T, groups []homeGroupBoardRowResponse, groupID string) homeGroupBoardRowResponse {
	t.Helper()
	for _, row := range groups {
		if row.GroupID == groupID {
			return row
		}
	}
	t.Fatalf("group %q not found in home response (%d rows)", groupID, len(groups))
	return homeGroupBoardRowResponse{}
}

func integrationHomeAppWithPyth(t *testing.T, pythClient marks.Client) (*HomeHandlers, *AuthHandlers, *GroupHandlers, wallets.Client, *postgres.Store, *postgres.TestIsolation) {
	t.Helper()

	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	groups := app.NewGroupService(store, privyClient)
	governance := app.NewGovernanceService(store, privyClient)
	symbols := app.NewSymbolResolver(nil)
	deposits := app.NewDepositService(store, privyClient, pythClient, symbols)
	home := app.NewHomeService(store, privyClient, pythClient, deposits, symbols)
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
	for _, row := range payload.Groups {
		if row.IsJoined {
			t.Fatalf("expected no joined groups for new user, got joined group %q", row.GroupID)
		}
	}
	if len(payload.People) != 0 {
		t.Fatalf("expected empty people board, got people=%d", len(payload.People))
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
	row := findHomeGroupRow(t, payload.Groups, created.GroupID)
	if row.Name != "Logan" {
		t.Fatalf("group name = %q, want Logan", row.Name)
	}
	if row.PotValueUsd != "0.00" {
		t.Fatalf("potValueUsd = %q, want 0.00", row.PotValueUsd)
	}
	if row.PercentReturn != nil {
		t.Fatalf("percentReturn = %v, want nil for unfunded group", row.PercentReturn)
	}
	if row.DollarPnL != "+0.00" {
		t.Fatalf("dollarPnl = %q, want +0.00", row.DollarPnL)
	}
	if !row.IsJoined {
		t.Fatal("expected isJoined=true for group creator")
	}
	if len(payload.People) != 1 {
		t.Fatalf("people len = %d, want 1 for unfunded creator on people board", len(payload.People))
	}
	if payload.People[0].PercentReturn != nil {
		t.Fatalf("percentReturn = %v, want nil for unfunded member", payload.People[0].PercentReturn)
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
	row := findHomeGroupRow(t, payload.Groups, created.GroupID)
	if row.Name != "Weekend investors" {
		t.Fatalf("group name = %q, want Weekend investors", row.Name)
	}
	if row.PotValueUsd != "100.00" {
		t.Fatalf("potValueUsd = %q, want 100.00", row.PotValueUsd)
	}
	if row.DollarPnL != "+0.00" {
		t.Fatalf("dollarPnl = %q, want +0.00 for flat funded group", row.DollarPnL)
	}
	if !row.IsJoined {
		t.Fatal("expected isJoined=true for funded group creator")
	}
	if len(payload.People) != 1 {
		t.Fatalf("people len = %d, want 1", len(payload.People))
	}
	if payload.People[0].DisplayName != "Alfred" {
		t.Fatalf("display name = %q, want Alfred", payload.People[0].DisplayName)
	}
}

func TestGET_home_nonMember_seesUnjoinedGroupRow(t *testing.T) {
	t.Parallel()
	// Arrange
	homeHandlers, authHandlers, groupHandlers, privyClient, _, iso := integrationHomeApp(t)
	_, creatorToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "all-groups-creator", "Creator")

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Discovery Club"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
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

	_, viewerToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "all-groups-viewer", "Viewer")
	req := httptest.NewRequest(http.MethodGet, "/v1/home", nil)
	req.Header.Set("Authorization", "Bearer "+string(viewerToken))
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
	row := findHomeGroupRow(t, payload.Groups, created.GroupID)
	if row.Name != "Discovery Club" {
		t.Fatalf("group name = %q, want Discovery Club", row.Name)
	}
	if row.IsJoined {
		t.Fatal("expected isJoined=false for non-member viewer")
	}
}

func TestGET_home_pythError_returns200(t *testing.T) {
	t.Parallel()

	pythClient := chainlink.NewFakeClient()
	homeHandlers, authHandlers, groupHandlers, privyClient, store, iso := integrationHomeAppWithPyth(t, pythClient)
	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "pyth-fail", "Alfred")

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Marked pot club"}`))
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
	const depositMicros = int64(2_000_000)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := store.IncrementPositionTx(ctx, tx, session.UserID, created.GroupID, depositMicros, depositMicros); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit position: %v", err)
	}

	const swappedUSDC = int64(1_000_000)
	const aaplAtomics = int64(500_000)
	_, _, err = store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          created.GroupID,
		Amount:           swappedUSDC,
		InputToken:        evm.USDCAddress,
		OutputToken:       "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash:      fmt.Sprintf("sig-%s-buy", iso.Suffix()),
		ExecuteRequestID: fmt.Sprintf("req-%s-buy", iso.Suffix()),
		CostBasisPrice:   swappedUSDC,
		CostBasisAmount:  aaplAtomics,
	})
	if err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}

	treasury, found, err := store.GetTreasuryByGroupID(ctx, created.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}
	wallets.SetTreasuryUSDCBalance(privyClient, treasury.Address, depositMicros-swappedUSDC)

	chainlink.RegisterMarkedPotError(pythClient, marks.TreasuryRef{
		GroupID: created.GroupID,
		Address: treasury.Address,
	}, fmt.Errorf("pyth latest price: status 403"))

	req := httptest.NewRequest(http.MethodGet, "/v1/home", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	homeHandlers.HomeHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload homeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	row := findHomeGroupRow(t, payload.Groups, created.GroupID)
	if row.PotValueUsd != "2.00" {
		t.Fatalf("potValueUsd = %q, want 2.00 (cost basis fallback)", row.PotValueUsd)
	}
}

func TestGET_home_treasurySurplus_reconcilesOnRead(t *testing.T) {
	t.Parallel()

	homeHandlers, authHandlers, groupHandlers, privyClient, store, iso := integrationHomeApp(t)
	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "home-surplus", "Surplus User")

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Surplus Club"}`))
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
	treasury, found, err := store.GetTreasuryByGroupID(ctx, created.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := store.IncrementPositionTx(ctx, tx, session.UserID, created.GroupID, 200_000, 200_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit position: %v", err)
	}

	wallets.SetTreasuryUSDCBalance(privyClient, treasury.Address, 400_000)

	req := httptest.NewRequest(http.MethodGet, "/v1/home", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	homeHandlers.HomeHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload homeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	row := findHomeGroupRow(t, payload.Groups, created.GroupID)
	if row.PotValueUsd != "0.40" {
		t.Fatalf("potValueUsd = %q, want 0.40", row.PotValueUsd)
	}
	if row.DollarPnL != "+0.00" {
		t.Fatalf("dollarPnl = %q, want +0.00", row.DollarPnL)
	}
}

func TestGET_userSharedGroups_returnsOnlySharedClubs(t *testing.T) {
	t.Parallel()

	homeHandlers, authHandlers, groupHandlers, privyClient, _, iso := integrationHomeApp(t)
	aliceSession, aliceToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "shared-alice", "Alice")
	_, bobToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "shared-bob", "Bob")
	_, carolToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "shared-carol", "Carol")

	createSharedClub := func(token auth.AccessToken, name string) string {
		t.Helper()
		createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"`+name+`"}`))
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
		return created.GroupID
	}

	joinClub := func(token auth.AccessToken, groupID string) {
		t.Helper()
		joinReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/join", strings.NewReader(`{}`))
		joinReq.SetPathValue("id", groupID)
		joinReq.Header.Set("Content-Type", "application/json")
		joinReq.Header.Set("Authorization", "Bearer "+string(token))
		joinRec := httptest.NewRecorder()
		groupHandlers.JoinGroupHandler(joinRec, joinReq)
		if joinRec.Code != http.StatusNoContent {
			t.Fatalf("join group status = %d, want 204; body = %s", joinRec.Code, joinRec.Body.String())
		}
	}

	sharedGroupID := createSharedClub(aliceToken, "Shared Club")
	joinClub(bobToken, sharedGroupID)
	privateGroupID := createSharedClub(bobToken, "Bob Only")
	_ = createSharedClub(carolToken, "Carol Only")

	req := httptest.NewRequest(http.MethodGet, "/v1/users/"+aliceSession.UserID+"/groups", nil)
	req.SetPathValue("id", aliceSession.UserID)
	req.Header.Set("Authorization", "Bearer "+string(bobToken))
	rec := httptest.NewRecorder()
	homeHandlers.UserSharedGroupsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload homeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Groups) != 1 {
		t.Fatalf("groups len = %d, want 1 shared club", len(payload.Groups))
	}
	if payload.Groups[0].GroupID != sharedGroupID {
		t.Fatalf("groupId = %q, want %q", payload.Groups[0].GroupID, sharedGroupID)
	}
	if payload.Groups[0].GroupID == privateGroupID {
		t.Fatal("private bob-only club must not appear in alice/bob shared list for bob viewer")
	}
}
