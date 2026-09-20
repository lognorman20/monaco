package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

func TestEnsureMemberWallet_existingPrivyWallet_reusesWithoutCreate(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db, iso := integrationDB(t)
	store := postgres.NewStore(db)
	privyClient := wallets.NewFakeClient()
	session := NewSessionService(store, privyClient)

	user, err := store.UpsertUser(ctx, iso.UniqueDynamicID("existing-wallet"), "Existing Wallet User")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)

	existing := wallets.WalletRef{
		UserID:        wallets.UserID(user.ID),
		WalletID: "wallet-prefixed-existing",
		Address: "SoPrefixedExisting111111111111111111111111111",
	}
	privy.RegisterPrivyMemberWallet(privyClient, user.PrivyUserID, existing)

	// Act
	wallet, err := session.EnsureMemberWallet(ctx, user.PrivyUserID, user.ID)
	if err != nil {
		t.Fatalf("EnsureMemberWallet: %v", err)
	}

	// Assert
	if wallet.WalletID != existing.WalletID {
		t.Fatalf("WalletID = %q, want %q", wallet.WalletID, existing.WalletID)
	}
	if wallet.Address != existing.Address {
		t.Fatalf("Address = %q, want %q", wallet.Address, existing.Address)
	}

	var rowCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM member_wallets WHERE user_id = $1", user.ID).Scan(&rowCount); err != nil {
		t.Fatalf("count member_wallets: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected 1 member_wallets row, got %d", rowCount)
	}
}

func TestEnsureMemberWallet_repeatSession_reusesSameWallet(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db, iso := integrationDB(t)
	store := postgres.NewStore(db)
	privyClient := wallets.NewFakeClient()
	session := NewSessionService(store, privyClient)

	user, err := store.UpsertUser(ctx, iso.UniqueDynamicID("wallet"), "Bartholomez")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)

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
	if first.Address != second.Address {
		t.Fatalf("expected same solana address, got first=%s second=%s", first.Address, second.Address)
	}

	var rowCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM member_wallets WHERE user_id = $1", user.ID).Scan(&rowCount); err != nil {
		t.Fatalf("count member_wallets: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected 1 member_wallets row, got %d", rowCount)
	}
}
