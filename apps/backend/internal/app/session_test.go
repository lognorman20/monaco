package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

func TestEnsureMemberWallet_existingPrivyWallet_reusesWithoutCreate(t *testing.T) {
	ctx := context.Background()
	db, iso := integrationDB(t)
	store := postgres.NewStore(db)
	walletClient := wallets.NewFakeClient()
	verifier := auth.NewFakeVerifier()
	session := NewSessionService(store, verifier, walletClient)

	dynamicID := iso.UniqueDynamicID("existing-wallet")
	user, err := store.UpsertUser(ctx, dynamicID, "Existing Wallet User")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)

	existing := wallets.WalletRef{
		UserID:   wallets.UserID(user.ID),
		WalletID: "wallet-prefixed-existing",
		Address:  "0xPrefixedExisting1111111111111111111111",
	}
	wallets.RegisterDynamicMemberWallet(walletClient, dynamicID, existing)

	wallet, err := session.EnsureMemberWallet(ctx, dynamicID, user.ID)
	if err != nil {
		t.Fatalf("EnsureMemberWallet: %v", err)
	}

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
	ctx := context.Background()
	db, iso := integrationDB(t)
	store := postgres.NewStore(db)
	walletClient := wallets.NewFakeClient()
	verifier := auth.NewFakeVerifier()
	session := NewSessionService(store, verifier, walletClient)

	dynamicID := iso.UniqueDynamicID("wallet")
	user, err := store.UpsertUser(ctx, dynamicID, "Bartholomez")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)

	first, err := session.EnsureMemberWallet(ctx, dynamicID, user.ID)
	if err != nil {
		t.Fatalf("first EnsureMemberWallet: %v", err)
	}
	second, err := session.EnsureMemberWallet(ctx, dynamicID, user.ID)
	if err != nil {
		t.Fatalf("second EnsureMemberWallet: %v", err)
	}
	if first.WalletID != second.WalletID || first.Address != second.Address {
		t.Fatalf("wallet changed between calls: %+v vs %+v", first, second)
	}
}
