package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

type testApp struct {
	DB       *sql.DB
	Store    *postgres.Store
	Privy    privy.Client
	Sessions *app.SessionService
	Groups   *app.GroupService
}

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

func integrationApp(t *testing.T) *testApp {
	t.Helper()

	db := integrationDB(t)
	resetTables(t, db)
	store := postgres.NewStore(db)
	privyClient := privy.NewFakeClient()

	return &testApp{
		DB:       db,
		Store:    store,
		Privy:    privyClient,
		Sessions: app.NewSessionService(store, privyClient),
		Groups:   app.NewGroupService(store, privyClient),
	}
}

func fixtureSessionToken() privy.AccessToken {
	return privy.AccessToken("fixture-session-token")
}

func TestEnsureMemberWallet_firstSession_createsMemberWalletRow(t *testing.T) {
	// Arrange
	ctx := context.Background()
	testApp := integrationApp(t)

	user, err := testApp.Store.UpsertUser(ctx, "did:privy:test-user-456", "Cayman")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	// Act
	wallet, err := testApp.Sessions.EnsureMemberWallet(ctx, user.PrivyUserID, user.ID)
	if err != nil {
		t.Fatalf("EnsureMemberWallet: %v", err)
	}

	// Assert
	if wallet.UserID != user.ID {
		t.Fatalf("expected user_id %s, got %s", user.ID, wallet.UserID)
	}
	if wallet.PrivyWalletID == "" {
		t.Fatal("expected privy_wallet_id to be set")
	}
	if wallet.SolanaAddress == "" {
		t.Fatal("expected solana_address to be set")
	}

	var rowCount int
	if err := testApp.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM member_wallets WHERE user_id = $1", user.ID).Scan(&rowCount); err != nil {
		t.Fatalf("count member_wallets: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected 1 member_wallets row, got %d", rowCount)
	}
}

func TestCreateGroup_insertsGroupAndTreasuryRows(t *testing.T) {
	// Arrange
	ctx := context.Background()
	testApp := integrationApp(t)
	token := fixtureSessionToken()
	privy.RegisterToken(testApp.Privy, token, privy.Identity{
		PrivyUserID: "did:privy:create-group-user",
		DisplayName: "Group Creator",
	})

	user, err := testApp.Store.UpsertUser(ctx, "did:privy:create-group-user", "Group Creator")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	// Act
	result, err := testApp.Groups.CreateGroup(ctx, string(token), "Alpha Fund")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	// Assert
	if result.GroupID == "" {
		t.Fatal("expected group id")
	}
	if result.Name != "Alpha Fund" {
		t.Fatalf("name = %q, want Alpha Fund", result.Name)
	}
	if result.TreasuryAddress == "" {
		t.Fatal("expected treasury address")
	}

	var groupCount int
	if err := testApp.DB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM groups WHERE id = $1 AND creator_user_id = $2",
		result.GroupID, user.ID,
	).Scan(&groupCount); err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if groupCount != 1 {
		t.Fatalf("expected 1 groups row, got %d", groupCount)
	}

	var treasuryCount int
	if err := testApp.DB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM treasuries WHERE group_id = $1",
		result.GroupID,
	).Scan(&treasuryCount); err != nil {
		t.Fatalf("count treasuries: %v", err)
	}
	if treasuryCount != 1 {
		t.Fatalf("expected 1 treasuries row, got %d", treasuryCount)
	}
}
