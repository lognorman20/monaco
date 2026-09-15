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
	return &GroupHandlers{Groups: groups}, authHandlers, privyClient, db
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
