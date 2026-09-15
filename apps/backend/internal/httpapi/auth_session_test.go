package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is not set")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatalf("ping db: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func resetTables(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx := context.Background()
	_, err := db.ExecContext(ctx, "TRUNCATE users, member_wallets, groups, treasuries RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("reset tables: %v", err)
	}
}

func fixtureSessionToken() privy.AccessToken {
	return privy.AccessToken("fixture-session-token")
}

func integrationApp(t *testing.T) (*AuthHandlers, privy.Client, *sql.DB) {
	t.Helper()

	db := integrationDB(t)
	resetTables(t, db)
	store := postgres.NewStore(db)
	privyClient := privy.NewFakeClient()
	sessions := app.NewSessionService(store, privyClient)
	return &AuthHandlers{Sessions: sessions}, privyClient, db
}

func TestPOST_auth_session_happyPath_returns200AndSetsSession(t *testing.T) {
	// Arrange
	handlers, privyClient, db := integrationApp(t)
	token := fixtureSessionToken()
	privy.RegisterToken(privyClient, token, privy.Identity{
		PrivyUserID: "did:privy:alfred",
		DisplayName: "Alfred",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/session", strings.NewReader(`{"accessToken":"fixture-session-token"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// Act
	handlers.SessionHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload authSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.UserID == "" {
		t.Fatal("expected userId in response")
	}
	if payload.DisplayName != "Alfred" {
		t.Fatalf("displayName = %q, want Alfred", payload.DisplayName)
	}
	if payload.MemberWalletAddress == "" {
		t.Fatal("expected memberWalletAddress in response")
	}

	ctx := context.Background()
	var userCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE privy_user_id = $1", "did:privy:alfred").Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("expected 1 user row, got %d", userCount)
	}

	var walletCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM member_wallets WHERE user_id = $1", payload.UserID).Scan(&walletCount); err != nil {
		t.Fatalf("count member_wallets: %v", err)
	}
	if walletCount != 1 {
		t.Fatalf("expected 1 member_wallets row, got %d", walletCount)
	}
}
