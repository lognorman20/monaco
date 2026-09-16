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

func integrationGroupApp(t *testing.T) (*GroupHandlers, *AuthHandlers, privy.Client, *sql.DB) {
	t.Helper()

	authHandlers, privyClient, db := integrationApp(t)
	store := postgres.NewStore(db)
	groups := app.NewGroupService(store, privyClient)
	governance := app.NewGovernanceService(store, privyClient)
	return &GroupHandlers{Groups: groups, Governance: governance}, authHandlers, privyClient, db
}

func TestPOST_groups_missingAuth_returns401(t *testing.T) {
	// Arrange
	groupHandlers, _, _, _ := integrationGroupApp(t)
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
	// Arrange
	groupHandlers, authHandlers, privyClient, db := integrationGroupApp(t)
	token := fixtureSessionToken()
	seedAuthenticatedUser(t, authHandlers, privyClient, token, privy.Identity{
		PrivyUserID: "did:privy:alfred",
		DisplayName: "Alfred",
	})
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
	// Arrange
	groupHandlers, authHandlers, privyClient, db := integrationGroupApp(t)
	token := fixtureSessionToken()
	seedAuthenticatedUser(t, authHandlers, privyClient, token, privy.Identity{
		PrivyUserID: "did:privy:bartholomez",
		DisplayName: "Bartholomez",
	})
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
	// Arrange
	groupHandlers, authHandlers, privyClient, _ := integrationGroupApp(t)
	token := fixtureSessionToken()
	seedAuthenticatedUser(t, authHandlers, privyClient, token, privy.Identity{
		PrivyUserID: "did:privy:alfred",
		DisplayName: "Alfred",
	})
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

func TestGET_group_nonMemberOrUnknown_returns404(t *testing.T) {
	// Arrange
	groupHandlers, authHandlers, privyClient, _ := integrationGroupApp(t)
	creatorToken := fixtureSessionToken()
	seedAuthenticatedUser(t, authHandlers, privyClient, creatorToken, privy.Identity{
		PrivyUserID: "did:privy:alfred",
		DisplayName: "Alfred",
	})
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

	otherToken := privy.AccessToken("other-user-session-token")
	seedAuthenticatedUser(t, authHandlers, privyClient, otherToken, privy.Identity{
		PrivyUserID: "did:privy:bartholomez",
		DisplayName: "Bartholomez",
	})
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
	// Arrange
	groupHandlers, authHandlers, privyClient, db := integrationGroupApp(t)
	token := fixtureSessionToken()
	user := seedAuthenticatedUser(t, authHandlers, privyClient, token, privy.Identity{PrivyUserID: "did:privy:rules-creator", DisplayName: "Creator"})
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
	// Arrange
	groupHandlers, authHandlers, privyClient, db := integrationGroupApp(t)
	creatorToken := fixtureSessionToken()
	seedAuthenticatedUser(t, authHandlers, privyClient, creatorToken, privy.Identity{PrivyUserID: "did:privy:open-creator", DisplayName: "Creator"})
	createRec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Open Club"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	groupHandlers.CreateGroupHandler(createRec, createReq)
	var created createGroupResponse
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	joinerToken := privy.AccessToken("open-joiner-token")
	seedAuthenticatedUser(t, authHandlers, privyClient, joinerToken, privy.Identity{PrivyUserID: "did:privy:open-joiner", DisplayName: "Joiner"})
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
	// Arrange
	groupHandlers, authHandlers, privyClient, db := integrationGroupApp(t)
	creatorToken := fixtureSessionToken()
	seedAuthenticatedUser(t, authHandlers, privyClient, creatorToken, privy.Identity{PrivyUserID: "did:privy:pw-creator", DisplayName: "Creator"})
	createRec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Secret Club","joinPolicy":{"mode":"password","password":"potluck"}}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	groupHandlers.CreateGroupHandler(createRec, createReq)
	var created createGroupResponse
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	joinerToken := privy.AccessToken("pw-joiner-token")
	seedAuthenticatedUser(t, authHandlers, privyClient, joinerToken, privy.Identity{PrivyUserID: "did:privy:pw-joiner", DisplayName: "Joiner"})
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
	// Arrange
	groupHandlers, authHandlers, privyClient, _ := integrationGroupApp(t)
	creatorToken := fixtureSessionToken()
	seedAuthenticatedUser(t, authHandlers, privyClient, creatorToken, privy.Identity{PrivyUserID: "did:privy:badpw-creator", DisplayName: "Creator"})
	createRec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Locked Club","joinPolicy":{"mode":"password","password":"potluck"}}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(creatorToken))
	groupHandlers.CreateGroupHandler(createRec, createReq)
	var created createGroupResponse
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	joinerToken := privy.AccessToken("badpw-joiner-token")
	seedAuthenticatedUser(t, authHandlers, privyClient, joinerToken, privy.Identity{PrivyUserID: "did:privy:badpw-joiner", DisplayName: "Joiner"})
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

func TestGET_group_missingAuth_returns401(t *testing.T) {
	// Arrange
	groupHandlers, _, _, _ := integrationGroupApp(t)
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
