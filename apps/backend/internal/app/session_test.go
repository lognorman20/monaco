package app

import (
	"context"
	"database/sql"
	"os"
	"testing"

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
	postgres.PrepareIntegrationDB(t, db)
}

func TestEnsureMemberWallet_repeatSession_reusesSameWallet(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	resetTables(t, db)
	store := postgres.NewStore(db)
	privyClient := privy.NewFakeClient()
	session := NewSessionService(store, privyClient)

	user, err := store.UpsertUser(ctx, "did:privy:test-user-789", "Bartholomez")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	// Act
	first, err := session.EnsureMemberWallet(ctx, user.PrivyUserID, user.ID)
	if err != nil {
		t.Fatalf("first EnsureMemberWallet: %v", err)
	}
	second, err := session.EnsureMemberWallet(ctx, user.PrivyUserID, user.ID)
	if err != nil {
		t.Fatalf("second EnsureMemberWallet: %v", err)
	}

	// Assert
	if first.ID != second.ID {
		t.Fatalf("expected same wallet id, got first=%s second=%s", first.ID, second.ID)
	}
	if first.SolanaAddress != second.SolanaAddress {
		t.Fatalf("expected same solana address, got first=%s second=%s", first.SolanaAddress, second.SolanaAddress)
	}

	var rowCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM member_wallets WHERE user_id = $1", user.ID).Scan(&rowCount); err != nil {
		t.Fatalf("count member_wallets: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected 1 member_wallets row, got %d", rowCount)
	}
}
