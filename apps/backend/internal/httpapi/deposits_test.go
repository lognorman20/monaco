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
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

func integrationDepositApp(t *testing.T) (*DepositHandlers, *GroupHandlers, *AuthHandlers, privy.Client, *sql.DB) {
	t.Helper()

	authHandlers, privyClient, db := integrationApp(t)
	store := postgres.NewStore(db)
	groups := app.NewGroupService(store, privyClient)
	governance := app.NewGovernanceService(store, privyClient)
	deposits := app.NewDepositService(store, privyClient, pyth.NewFakeClient())
	return &DepositHandlers{Deposits: deposits}, &GroupHandlers{Groups: groups, Governance: governance}, authHandlers, privyClient, db
}

func seedGroup(t *testing.T, groupHandlers *GroupHandlers, authHandlers *AuthHandlers, privyClient privy.Client, token privy.AccessToken, identity privy.Identity, name string) createGroupResponse {
	t.Helper()
	seedAuthenticatedUser(t, authHandlers, privyClient, token, identity)
	req := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"`+name+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	groupHandlers.CreateGroupHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create group status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload createGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode group json: %v", err)
	}
	return payload
}

func TestPOST_deposit_missingAuth_returns401(t *testing.T) {
	// Arrange
	depositHandlers, _, _, _, _ := integrationDepositApp(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/groups/g1/deposits", strings.NewReader(`{"amount":1000000}`))
	req.SetPathValue("id", "g1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// Act
	depositHandlers.CreateDepositHandler(rec, req)

	// Assert
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestPOST_deposit_zeroOrNegativeAmount_returns400(t *testing.T) {
	// Arrange
	depositHandlers, groupHandlers, authHandlers, privyClient, _ := integrationDepositApp(t)
	token := fixtureSessionToken()
	group := seedGroup(t, groupHandlers, authHandlers, privyClient, token, privy.Identity{PrivyUserID: "did:privy:alfred"}, "Fund")
	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+group.GroupID+"/deposits", strings.NewReader(`{"amount":0}`))
	req.SetPathValue("id", group.GroupID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	depositHandlers.CreateDepositHandler(rec, req)

	// Assert
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPOST_deposit_nonMember_returns403(t *testing.T) {
	// Arrange
	depositHandlers, groupHandlers, authHandlers, privyClient, _ := integrationDepositApp(t)
	creatorToken := fixtureSessionToken()
	group := seedGroup(t, groupHandlers, authHandlers, privyClient, creatorToken, privy.Identity{PrivyUserID: "did:privy:alfred"}, "Fund")
	otherToken := privy.AccessToken("other-token")
	seedAuthenticatedUser(t, authHandlers, privyClient, otherToken, privy.Identity{PrivyUserID: "did:privy:bartholomez"})
	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+group.GroupID+"/deposits", strings.NewReader(`{"amount":1000000}`))
	req.SetPathValue("id", group.GroupID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(otherToken))
	rec := httptest.NewRecorder()

	// Act
	depositHandlers.CreateDepositHandler(rec, req)

	// Assert
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestPOST_deposit_createsPendingDepositRow(t *testing.T) {
	// Arrange
	depositHandlers, groupHandlers, authHandlers, privyClient, db := integrationDepositApp(t)
	token := fixtureSessionToken()
	group := seedGroup(t, groupHandlers, authHandlers, privyClient, token, privy.Identity{PrivyUserID: "did:privy:alfred"}, "Fund")
	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+group.GroupID+"/deposits", strings.NewReader(`{"amount":2500000}`))
	req.SetPathValue("id", group.GroupID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	depositHandlers.CreateDepositHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload createDepositResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.Status != "pending" {
		t.Fatalf("status = %q, want pending", payload.Status)
	}

	ctx := context.Background()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM deposits WHERE id = $1 AND status = 'pending'", payload.DepositID).Scan(&count); err != nil {
		t.Fatalf("count deposits: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 pending deposit row, got %d", count)
	}
}

func TestGET_depositStatus_returnsCreditedShareUnitsWhenConfirmed(t *testing.T) {
	// Arrange
	depositHandlers, groupHandlers, authHandlers, privyClient, db := integrationDepositApp(t)
	token := fixtureSessionToken()
	group := seedGroup(t, groupHandlers, authHandlers, privyClient, token, privy.Identity{PrivyUserID: "did:privy:alfred"}, "Fund")
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+group.GroupID+"/deposits", strings.NewReader(`{"amount":5000000}`))
	createReq.SetPathValue("id", group.GroupID)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(token))
	createRec := httptest.NewRecorder()
	depositHandlers.CreateDepositHandler(createRec, createReq)
	var created createDepositResponse
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)

	treasury, _ := privyClient.EnsureTreasury(context.Background(), privy.GroupID(group.GroupID))
	sweep := app.ObservedSweep{
		TxSignature: "SWEEP-get-test",
		FromAddress: created.FromAddress,
		ToAddress:   treasury.SolanaAddress,
		Amount:      5000000,
		DepositID:   created.DepositID,
		UserID:      mustUserID(t, db, "did:privy:alfred"),
		GroupID:     group.GroupID,
	}
	if _, err := depositHandlers.Deposits.ObserveSweep(context.Background(), sweep); err != nil {
		t.Fatalf("ObserveSweep: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/deposits/"+created.DepositID, nil)
	req.SetPathValue("id", created.DepositID)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	depositHandlers.GetDepositHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload getDepositResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.Status != "confirmed" {
		t.Fatalf("status = %q, want confirmed", payload.Status)
	}
	if payload.ShareUnits != 5000000 {
		t.Fatalf("shareUnits = %d, want 5000000", payload.ShareUnits)
	}
}

func TestGET_memberShareUnits_reflectsPositionAfterSweep(t *testing.T) {
	// Arrange
	depositHandlers, groupHandlers, authHandlers, privyClient, db := integrationDepositApp(t)
	token := fixtureSessionToken()
	group := seedGroup(t, groupHandlers, authHandlers, privyClient, token, privy.Identity{PrivyUserID: "did:privy:alfred"}, "Fund")
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+group.GroupID+"/deposits", strings.NewReader(`{"amount":3000000}`))
	createReq.SetPathValue("id", group.GroupID)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(token))
	createRec := httptest.NewRecorder()
	depositHandlers.CreateDepositHandler(createRec, createReq)
	var created createDepositResponse
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	treasury, _ := privyClient.EnsureTreasury(context.Background(), privy.GroupID(group.GroupID))
	_, err := depositHandlers.Deposits.ObserveSweep(context.Background(), app.ObservedSweep{
		TxSignature: "SWEEP-share-units",
		FromAddress: created.FromAddress,
		ToAddress:   treasury.SolanaAddress,
		Amount:      3000000,
		DepositID:   created.DepositID,
		UserID:      mustUserID(t, db, "did:privy:alfred"),
		GroupID:     group.GroupID,
	})
	if err != nil {
		t.Fatalf("ObserveSweep: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+group.GroupID+"/share-units", nil)
	req.SetPathValue("id", group.GroupID)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	depositHandlers.GetMemberShareUnitsHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload memberShareUnitsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.ShareUnits != 3000000 {
		t.Fatalf("shareUnits = %d, want 3000000", payload.ShareUnits)
	}
}

func TestGET_treasuryUsdcBalance_returnsPostSweepAmount(t *testing.T) {
	// Arrange
	depositHandlers, groupHandlers, authHandlers, privyClient, _ := integrationDepositApp(t)
	token := fixtureSessionToken()
	group := seedGroup(t, groupHandlers, authHandlers, privyClient, token, privy.Identity{PrivyUserID: "did:privy:alfred"}, "Fund")
	privy.SetTreasuryUSDCBalance(privyClient, group.TreasuryAddress, 9000000)

	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+group.GroupID+"/treasury/usdc", nil)
	req.SetPathValue("id", group.GroupID)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	depositHandlers.GetTreasuryUsdcBalanceHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload treasuryUsdcBalanceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.UsdcBalance != 9000000 {
		t.Fatalf("usdcBalance = %d, want 9000000", payload.UsdcBalance)
	}
}

func mustUserID(t *testing.T, db *sql.DB, privyUserID string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(context.Background(), "SELECT id FROM users WHERE privy_user_id = $1", privyUserID).Scan(&id); err != nil {
		t.Fatalf("select user id: %v", err)
	}
	return id
}
