package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func integrationGroupApp(t *testing.T) (*GroupHandlers, *AuthHandlers, privy.Client, *sql.DB, *postgres.TestIsolation) {
	t.Helper()

	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	groups := app.NewGroupService(store, privyClient)
	governance := app.NewGovernanceService(store, privyClient)
	symbols := app.NewSymbolResolver(nil)
	deposits := app.NewDepositService(store, privyClient, nil, symbols)
	home := app.NewHomeService(store, privyClient, nil, deposits, symbols)
	return &GroupHandlers{Groups: groups, Governance: governance, Home: home}, authHandlers, privyClient, db, iso
}

func TestPOST_groups_missingAuth_returns401(t *testing.T) {
	t.Parallel()
	// Arrange
	groupHandlers, _, _, _, _ := integrationGroupApp(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Alpha Fund"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// Act
	groupHandlers.CreateGroupHandler(rec, req)

	// Assert
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCreateGroup_insertsGroupAndTreasuryRows(t *testing.T) {
	t.Parallel()
	// Arrange
	groupHandlers, authHandlers, privyClient, db, iso := integrationGroupApp(t)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "creator", "Alfred")
	req := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Alpha Fund"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	groupHandlers.CreateGroupHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload createGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	trackCreatedGroup(iso, payload.GroupID)
	if payload.GroupID == "" {
		t.Fatal("expected groupId in response")
	}
	if payload.Name != "Alpha Fund" {
		t.Fatalf("name = %q, want Alpha Fund", payload.Name)
	}
	if payload.TreasuryAddress == "" {
		t.Fatal("expected treasuryAddress in response")
	}

	ctx := context.Background()
	var groupCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM groups WHERE id = $1", payload.GroupID).Scan(&groupCount); err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if groupCount != 1 {
		t.Fatalf("expected 1 groups row, got %d", groupCount)
	}

	var treasuryCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM treasuries WHERE group_id = $1", payload.GroupID).Scan(&treasuryCount); err != nil {
		t.Fatalf("count treasuries: %v", err)
	}
	if treasuryCount != 1 {
		t.Fatalf("expected 1 treasuries row, got %d", treasuryCount)
	}
}

func TestCreateGroup_provisionsTreasuryViaPrivyClient(t *testing.T) {
	t.Parallel()
	// Arrange
	groupHandlers, authHandlers, privyClient, db, iso := integrationGroupApp(t)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "creator", "Bartholomez")
	req := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Beta Club"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	groupHandlers.CreateGroupHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload createGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	trackCreatedGroup(iso, payload.GroupID)

	expectedTreasury, err := privyClient.EnsureTreasury(context.Background(), privy.GroupID(payload.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	if payload.TreasuryAddress != expectedTreasury.SolanaAddress {
		t.Fatalf("treasuryAddress = %q, want %q", payload.TreasuryAddress, expectedTreasury.SolanaAddress)
	}

	ctx := context.Background()
	var privyWalletID string
	if err := db.QueryRowContext(ctx, "SELECT privy_wallet_id FROM treasuries WHERE group_id = $1", payload.GroupID).Scan(&privyWalletID); err != nil {
		t.Fatalf("select treasury privy_wallet_id: %v", err)
	}
	if privyWalletID != expectedTreasury.PrivyWalletID {
		t.Fatalf("privy_wallet_id = %q, want %q", privyWalletID, expectedTreasury.PrivyWalletID)
	}
}

func TestGET_group_byId_returnsNameAndTreasuryAddress(t *testing.T) {
	t.Parallel()
	// Arrange
	groupHandlers, authHandlers, privyClient, _, iso := integrationGroupApp(t)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "creator", "Alfred")
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Alpha Fund"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(token))
	createRec := httptest.NewRecorder()
	groupHandlers.CreateGroupHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create group status = %d, want 200; body = %s", createRec.Code, createRec.Body.String())
	}

	var created createGroupResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create json: %v", err)
	}
	trackCreatedGroup(iso, created.GroupID)

	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+created.GroupID, nil)
	req.SetPathValue("id", created.GroupID)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	groupHandlers.GetGroupHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload getGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.Name != "Alpha Fund" {
		t.Fatalf("name = %q, want Alpha Fund", payload.Name)
	}
	if payload.TreasuryAddress != created.TreasuryAddress {
		t.Fatalf("treasuryAddress = %q, want %q", payload.TreasuryAddress, created.TreasuryAddress)
	}
}

func TestGET_groupView_byId_returnsPotYouAndMembers(t *testing.T) {
	t.Parallel()
	// Arrange
	groupHandlers, authHandlers, privyClient, _, iso := integrationGroupApp(t)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "creator", "Alfred")
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
		t.Fatalf("decode create json: %v", err)
	}
	trackCreatedGroup(iso, created.GroupID)

	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+created.GroupID+"/view", nil)
	req.SetPathValue("id", created.GroupID)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	groupHandlers.GetGroupViewHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload groupViewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.ID != created.GroupID {
		t.Fatalf("id = %q, want %q", payload.ID, created.GroupID)
	}
	if payload.Name != "Weekend investors" {
		t.Fatalf("name = %q, want Weekend investors", payload.Name)
	}
	if payload.TreasuryAddress != created.TreasuryAddress {
		t.Fatalf("treasuryAddress = %q, want %q", payload.TreasuryAddress, created.TreasuryAddress)
	}
	if len(payload.Pot) == 0 || payload.Pot[0].Symbol != "USDC" {
		t.Fatalf("pot = %#v, want USDC row", payload.Pot)
	}
	if payload.Pot[0].DollarPnL != "+0.00" {
		t.Fatalf("pot[0].dollarPnl = %q, want +0.00", payload.Pot[0].DollarPnL)
	}
	if payload.You.ShareUnits != "0" {
		t.Fatalf("you.shareUnits = %q, want 0", payload.You.ShareUnits)
	}
	if len(payload.Proposals) != 0 {
		t.Fatalf("proposals = %#v, want empty array", payload.Proposals)
	}
	if len(payload.Members) != 1 {
		t.Fatalf("members len = %d, want 1", len(payload.Members))
	}
}

func TestGET_groupView_listsAllMembersWithoutDeposits(t *testing.T) {
	t.Parallel()
	// Arrange
	groupHandlers, authHandlers, privyClient, _, iso := integrationGroupApp(t)
	_, creatorToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "view-creator", "Creator")
	createRec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Everyone Club"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	groupHandlers.CreateGroupHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create group status = %d, want 200; body = %s", createRec.Code, createRec.Body.String())
	}

	var created createGroupResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create json: %v", err)
	}
	trackCreatedGroup(iso, created.GroupID)

	_, joinerToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "view-joiner", "Joiner")
	joinRec := httptest.NewRecorder()
	joinReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+created.GroupID+"/join", strings.NewReader(`{}`))
	joinReq.SetPathValue("id", created.GroupID)
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+string(joinerToken))
	groupHandlers.JoinGroupHandler(joinRec, joinReq)
	if joinRec.Code != http.StatusNoContent {
		t.Fatalf("join status = %d, want 204; body = %s", joinRec.Code, joinRec.Body.String())
	}

	viewReq := httptest.NewRequest(http.MethodGet, "/v1/groups/"+created.GroupID+"/view", nil)
	viewReq.SetPathValue("id", created.GroupID)
	viewReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	viewRec := httptest.NewRecorder()

	// Act
	groupHandlers.GetGroupViewHandler(viewRec, viewReq)

	// Assert
	if viewRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", viewRec.Code, viewRec.Body.String())
	}

	var payload groupViewResponse
	if err := json.Unmarshal(viewRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Members) != 2 {
		t.Fatalf("members len = %d, want 2", len(payload.Members))
	}
	for _, member := range payload.Members {
		if member.DisplayName == "" {
			t.Fatalf("member %q missing display name", member.UserID)
		}
		if member.DollarPnL == "" {
			t.Fatalf("member %q missing dollarPnl", member.UserID)
		}
	}
}

func TestGET_group_nonMemberOrUnknown_returns404(t *testing.T) {
	t.Parallel()
	// Arrange
	groupHandlers, authHandlers, privyClient, _, iso := integrationGroupApp(t)
	_, creatorToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "creator", "Alfred")
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Alpha Fund"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	createRec := httptest.NewRecorder()
	groupHandlers.CreateGroupHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create group status = %d, want 200; body = %s", createRec.Code, createRec.Body.String())
	}

	var created createGroupResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create json: %v", err)
	}
	trackCreatedGroup(iso, created.GroupID)

	unknownReq := httptest.NewRequest(http.MethodGet, "/v1/groups/00000000-0000-0000-0000-000000000000", nil)
	unknownReq.SetPathValue("id", "00000000-0000-0000-0000-000000000000")
	unknownReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	unknownRec := httptest.NewRecorder()

	// Act
	groupHandlers.GetGroupHandler(unknownRec, unknownReq)

	// Assert
	if unknownRec.Code != http.StatusNotFound {
		t.Fatalf("unknown group status = %d, want 404; body = %s", unknownRec.Code, unknownRec.Body.String())
	}

	_, otherToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "other", "Bartholomez")
	nonMemberReq := httptest.NewRequest(http.MethodGet, "/v1/groups/"+created.GroupID, nil)
	nonMemberReq.SetPathValue("id", created.GroupID)
	nonMemberReq.Header.Set("Authorization", "Bearer "+string(otherToken))
	nonMemberRec := httptest.NewRecorder()

	// Act
	groupHandlers.GetGroupHandler(nonMemberRec, nonMemberReq)

	// Assert
	if nonMemberRec.Code != http.StatusNotFound {
		t.Fatalf("non-member status = %d, want 404; body = %s", nonMemberRec.Code, nonMemberRec.Body.String())
	}
}

func TestPOST_groups_persistsJoinPolicyVoterSetThresholdExpiry(t *testing.T) {
	t.Parallel()
	// Arrange
	groupHandlers, authHandlers, privyClient, db, iso := integrationGroupApp(t)
	user, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "rules-creator", "Creator")
	body := `{"name":"Rules Fund","joinPolicy":{"mode":"password","password":"potluck"},"voterSet":{"mode":"named_subset","memberIds":["` + user.UserID + `"]},"threshold":"unanimous","voteExpirySeconds":3600}`
	req := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	// Act
	groupHandlers.CreateGroupHandler(rec, req)
	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload createGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	trackCreatedGroup(iso, payload.GroupID)
	var joinMode, voterSetMode, threshold string
	var voteExpirySeconds int64
	var passwordHash string
	if err := db.QueryRowContext(context.Background(), `SELECT join_mode, voter_set_mode, threshold, vote_expiry_seconds, COALESCE(join_password_hash, '') FROM groups WHERE id = $1`, payload.GroupID).Scan(&joinMode, &voterSetMode, &threshold, &voteExpirySeconds, &passwordHash); err != nil {
		t.Fatalf("select group rules: %v", err)
	}
	if joinMode != "password" || voterSetMode != "named_subset" || threshold != "unanimous" || voteExpirySeconds != 3600 {
		t.Fatalf("unexpected persisted rules")
	}
	if passwordHash == "" || passwordHash == "potluck" {
		t.Fatal("expected hashed join password")
	}
}

func TestPOST_join_openGroup_addsMemberWithoutPassword(t *testing.T) {
	t.Parallel()
	// Arrange
	groupHandlers, authHandlers, privyClient, db, iso := integrationGroupApp(t)
	_, creatorToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "open-creator", "Creator")
	createRec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Open Club"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	groupHandlers.CreateGroupHandler(createRec, createReq)
	var created createGroupResponse
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	trackCreatedGroup(iso, created.GroupID)
	_, joinerToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "open-joiner", "Joiner")
	joinRec := httptest.NewRecorder()
	joinReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+created.GroupID+"/join", strings.NewReader(`{}`))
	joinReq.SetPathValue("id", created.GroupID)
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+string(joinerToken))
	// Act
	groupHandlers.JoinGroupHandler(joinRec, joinReq)
	// Assert
	if joinRec.Code != http.StatusNoContent {
		t.Fatalf("join status = %d, want 204", joinRec.Code)
	}
	var memberCount int
	_ = db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM group_members WHERE group_id = $1", created.GroupID).Scan(&memberCount)
	if memberCount != 2 {
		t.Fatalf("expected 2 members, got %d", memberCount)
	}
}

func TestPOST_join_passwordGroup_requiresCorrectPassword(t *testing.T) {
	t.Parallel()
	// Arrange
	groupHandlers, authHandlers, privyClient, db, iso := integrationGroupApp(t)
	_, creatorToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "pw-creator", "Creator")
	createRec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Secret Club","joinPolicy":{"mode":"password","password":"potluck"}}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	groupHandlers.CreateGroupHandler(createRec, createReq)
	var created createGroupResponse
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	trackCreatedGroup(iso, created.GroupID)
	_, joinerToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "pw-joiner", "Joiner")
	joinRec := httptest.NewRecorder()
	joinReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+created.GroupID+"/join", strings.NewReader(`{"password":"potluck"}`))
	joinReq.SetPathValue("id", created.GroupID)
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+string(joinerToken))
	// Act
	groupHandlers.JoinGroupHandler(joinRec, joinReq)
	// Assert
	if joinRec.Code != http.StatusNoContent {
		t.Fatalf("join status = %d, want 204", joinRec.Code)
	}
	var memberCount int
	_ = db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM group_members WHERE group_id = $1", created.GroupID).Scan(&memberCount)
	if memberCount != 2 {
		t.Fatalf("expected 2 members, got %d", memberCount)
	}
}

func TestPOST_join_wrongPassword_returns403(t *testing.T) {
	t.Parallel()
	// Arrange
	groupHandlers, authHandlers, privyClient, _, iso := integrationGroupApp(t)
	_, creatorToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "badpw-creator", "Creator")
	createRec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Locked Club","joinPolicy":{"mode":"password","password":"potluck"}}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	groupHandlers.CreateGroupHandler(createRec, createReq)
	var created createGroupResponse
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	trackCreatedGroup(iso, created.GroupID)
	_, joinerToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "badpw-joiner", "Joiner")
	joinRec := httptest.NewRecorder()
	joinReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+created.GroupID+"/join", strings.NewReader(`{"password":"wrong"}`))
	joinReq.SetPathValue("id", created.GroupID)
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+string(joinerToken))
	// Act
	groupHandlers.JoinGroupHandler(joinRec, joinReq)
	// Assert
	if joinRec.Code != http.StatusForbidden {
		t.Fatalf("join status = %d, want 403", joinRec.Code)
	}
}

func TestGET_groupActivity_returnsMixedStatuses(t *testing.T) {
	t.Parallel()

	groupHandlers, authHandlers, privyClient, db, iso := integrationGroupApp(t)
	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "activity-http", "Activity User")

	createRec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Activity Club"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(token))
	groupHandlers.CreateGroupHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create group status = %d, want 200; body = %s", createRec.Code, createRec.Body.String())
	}

	var created createGroupResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create json: %v", err)
	}
	trackCreatedGroup(iso, created.GroupID)

	store := postgres.NewStore(db)
	ctx := context.Background()
	if _, err := store.InsertDeposit(ctx, session.UserID, created.GroupID, 1_000_000, "from-wallet"); err != nil {
		t.Fatalf("insert deposit: %v", err)
	}
	if _, err := store.InsertFailedTransaction(ctx, created.GroupID, postgres.TransactionActionBuy, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp", 2_000_000, "req-failed-"+iso.Suffix()); err != nil {
		t.Fatalf("insert failed buy: %v", err)
	}
	if _, _, err := store.InsertPendingTransaction(ctx, postgres.InsertPendingTransactionParams{
		GroupID:          created.GroupID,
		Action:           postgres.TransactionActionSell,
		InputMint:        "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp",
		OutputMint:       "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		Amount:           500_000,
		ExecuteRequestID: "req-pending-" + iso.Suffix(),
	}); err != nil {
		t.Fatalf("insert pending sell: %v", err)
	}

	activityReq := httptest.NewRequest(http.MethodGet, "/v1/groups/"+created.GroupID+"/activity", nil)
	activityReq.SetPathValue("id", created.GroupID)
	activityReq.Header.Set("Authorization", "Bearer "+string(token))
	activityRec := httptest.NewRecorder()
	groupHandlers.ListGroupActivityHandler(activityRec, activityReq)

	if activityRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", activityRec.Code, activityRec.Body.String())
	}

	var payload groupActivityResponse
	if err := json.Unmarshal(activityRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Items) < 3 {
		t.Fatalf("items = %d, want at least 3", len(payload.Items))
	}

	statuses := map[string]int{}
	kinds := map[string]int{}
	for _, item := range payload.Items {
		statuses[item.Status]++
		kinds[item.Kind]++
	}
	if statuses["pending"] == 0 || statuses["failed"] == 0 {
		t.Fatalf("expected pending and failed statuses, got %#v", statuses)
	}
	if kinds["deposit"] == 0 || kinds["sell"] == 0 || kinds["buy"] == 0 {
		t.Fatalf("expected deposit, buy, and sell kinds, got %#v", kinds)
	}
}

func createOpenGroupWithJoiner(t *testing.T, groupHandlers *GroupHandlers, authHandlers *AuthHandlers, privyClient privy.Client, iso *postgres.TestIsolation, creatorLabel, joinerLabel string) (createGroupResponse, authSessionResponse, privy.AccessToken, privy.AccessToken) {
	t.Helper()
	_, creatorToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, creatorLabel, "Creator")
	createRec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Leave Test Club"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	groupHandlers.CreateGroupHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create group status = %d, want 200; body = %s", createRec.Code, createRec.Body.String())
	}
	var created createGroupResponse
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	trackCreatedGroup(iso, created.GroupID)
	joinerSession, joinerToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, joinerLabel, "Joiner")
	joinRec := httptest.NewRecorder()
	joinReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+created.GroupID+"/join", strings.NewReader(`{}`))
	joinReq.SetPathValue("id", created.GroupID)
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+string(joinerToken))
	groupHandlers.JoinGroupHandler(joinRec, joinReq)
	if joinRec.Code != http.StatusNoContent {
		t.Fatalf("join status = %d, want 204; body = %s", joinRec.Code, joinRec.Body.String())
	}
	return created, joinerSession, creatorToken, joinerToken
}

func TestPOST_leave_joinedMember_removesMembership(t *testing.T) {
	t.Parallel()
	groupHandlers, authHandlers, privyClient, db, iso := integrationGroupApp(t)
	created, _, creatorToken, joinerToken := createOpenGroupWithJoiner(t, groupHandlers, authHandlers, privyClient, iso, "leave-happy-creator", "leave-happy-joiner")
	leaveRec := httptest.NewRecorder()
	leaveReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+created.GroupID+"/leave", nil)
	leaveReq.SetPathValue("id", created.GroupID)
	leaveReq.Header.Set("Authorization", "Bearer "+string(joinerToken))
	groupHandlers.LeaveGroupHandler(leaveRec, leaveReq)
	if leaveRec.Code != http.StatusNoContent {
		t.Fatalf("leave status = %d, want 204; body = %s", leaveRec.Code, leaveRec.Body.String())
	}
	var memberCount int
	_ = db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM group_members WHERE group_id = $1", created.GroupID).Scan(&memberCount)
	if memberCount != 1 {
		t.Fatalf("member count = %d, want 1", memberCount)
	}
	viewRec := httptest.NewRecorder()
	viewReq := httptest.NewRequest(http.MethodGet, "/v1/groups/"+created.GroupID+"/view", nil)
	viewReq.SetPathValue("id", created.GroupID)
	viewReq.Header.Set("Authorization", "Bearer "+string(joinerToken))
	groupHandlers.GetGroupViewHandler(viewRec, viewReq)
	if viewRec.Code != http.StatusNotFound {
		t.Fatalf("leaver view status = %d, want 404", viewRec.Code)
	}
	creatorViewRec := httptest.NewRecorder()
	creatorViewReq := httptest.NewRequest(http.MethodGet, "/v1/groups/"+created.GroupID+"/view", nil)
	creatorViewReq.SetPathValue("id", created.GroupID)
	creatorViewReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	groupHandlers.GetGroupViewHandler(creatorViewRec, creatorViewReq)
	if creatorViewRec.Code != http.StatusOK {
		t.Fatalf("creator view status = %d, want 200", creatorViewRec.Code)
	}
}

func TestPOST_leave_withShareUnits_returns409(t *testing.T) {
	t.Parallel()
	groupHandlers, authHandlers, privyClient, db, iso := integrationGroupApp(t)
	created, joinerSession, _, joinerToken := createOpenGroupWithJoiner(t, groupHandlers, authHandlers, privyClient, iso, "leave-shares-creator", "leave-shares-joiner")
	store := postgres.NewStore(db)
	tx, _ := store.BeginTx(context.Background())
	_, _ = store.IncrementPositionTx(context.Background(), tx, joinerSession.UserID, created.GroupID, 500_000, 500_000)
	_ = tx.Commit()
	leaveRec := httptest.NewRecorder()
	leaveReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+created.GroupID+"/leave", nil)
	leaveReq.SetPathValue("id", created.GroupID)
	leaveReq.Header.Set("Authorization", "Bearer "+string(joinerToken))
	groupHandlers.LeaveGroupHandler(leaveRec, leaveReq)
	if leaveRec.Code != http.StatusConflict {
		t.Fatalf("leave status = %d, want 409", leaveRec.Code)
	}
}

func TestPOST_leave_lastMemberWithTreasury_returns409(t *testing.T) {
	t.Parallel()
	groupHandlers, authHandlers, privyClient, db, iso := integrationGroupApp(t)
	creatorSession, creatorToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "leave-last-creator", "Creator")
	createRec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Solo Treasury Club"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	groupHandlers.CreateGroupHandler(createRec, createReq)
	var created createGroupResponse
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	trackCreatedGroup(iso, created.GroupID)
	_, _ = db.ExecContext(context.Background(), `INSERT INTO positions (user_id, group_id, share_units, amount_deposited, amount_withdrawn) VALUES ($1,$2,0,1000000,0) ON CONFLICT (user_id, group_id) DO UPDATE SET amount_deposited = EXCLUDED.amount_deposited`, creatorSession.UserID, created.GroupID)
	leaveRec := httptest.NewRecorder()
	leaveReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+created.GroupID+"/leave", nil)
	leaveReq.SetPathValue("id", created.GroupID)
	leaveReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	groupHandlers.LeaveGroupHandler(leaveRec, leaveReq)
	if leaveRec.Code != http.StatusConflict {
		t.Fatalf("leave status = %d, want 409", leaveRec.Code)
	}
}

func TestGET_group_missingAuth_returns401(t *testing.T) {
	t.Parallel()
	// Arrange
	groupHandlers, _, _, _, _ := integrationGroupApp(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/groups/00000000-0000-0000-0000-000000000000", nil)
	req.SetPathValue("id", "00000000-0000-0000-0000-000000000000")
	rec := httptest.NewRecorder()

	// Act
	groupHandlers.GetGroupHandler(rec, req)

	// Assert
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
