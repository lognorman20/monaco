package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func TestEnsureMemberWallet_existingPrivyWallet_reusesWithoutCreate(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db, iso := integrationDB(t)
	store := postgres.NewStore(db)
	privyClient := privy.NewFakeClient()
	session := NewSessionService(store, privyClient)

	user, err := store.UpsertUser(ctx, iso.UniquePrivyID("existing-wallet"), "Existing Wallet User")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)

	existing := privy.WalletRef{
		UserID:        privy.UserID(user.ID),
		PrivyWalletID: "wallet-prefixed-existing",
		SolanaAddress: "SoPrefixedExisting111111111111111111111111111",
	}
	privy.RegisterPrivyMemberWallet(privyClient, user.PrivyUserID, existing)

	// Act
	wallet, err := session.EnsureMemberWallet(ctx, user.PrivyUserID, user.ID)
	if err != nil {
		t.Fatalf("EnsureMemberWallet: %v", err)
	}

	// Assert
	if wallet.PrivyWalletID != existing.PrivyWalletID {
		t.Fatalf("PrivyWalletID = %q, want %q", wallet.PrivyWalletID, existing.PrivyWalletID)
	}
	if wallet.SolanaAddress != existing.SolanaAddress {
		t.Fatalf("SolanaAddress = %q, want %q", wallet.SolanaAddress, existing.SolanaAddress)
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
	privyClient := privy.NewFakeClient()
	session := NewSessionService(store, privyClient)

	user, err := store.UpsertUser(ctx, iso.UniquePrivyID("wallet"), "Bartholomez")
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
