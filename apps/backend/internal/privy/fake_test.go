package privy

import (
	"context"
	"testing"
)

func TestFakeClient_EnsureMemberWallet_reusesRegisteredPrivyWallet(t *testing.T) {
	// Arrange
	client := NewFakeClient()
	ctx := context.Background()
	privyUserID := "did:privy:existing-user"
	userID := UserID("11111111-1111-1111-1111-111111111111")
	RegisterPrivyMemberWallet(client, privyUserID, WalletRef{
		PrivyWalletID: "wallet-registered",
		SolanaAddress: "SoRegistered1111111111111111111111111111111",
	})

	// Act
	ref, err := client.EnsureMemberWallet(ctx, privyUserID, userID)

	// Assert
	if err != nil {
		t.Fatalf("EnsureMemberWallet: %v", err)
	}
	if ref.PrivyWalletID != "wallet-registered" {
		t.Fatalf("PrivyWalletID = %q", ref.PrivyWalletID)
	}
	if ref.SolanaAddress != "SoRegistered1111111111111111111111111111111" {
		t.Fatalf("SolanaAddress = %q", ref.SolanaAddress)
	}
}

func TestFakeClient_EnsureMemberWallet_returnsDeterministicAddress(t *testing.T) {
	// Arrange
	client := NewFakeClient()
	ctx := context.Background()
	userID := UserID("11111111-1111-1111-1111-111111111111")

	// Act
	first, err := client.EnsureMemberWallet(ctx, "did:privy:user-a", userID)
	if err != nil {
		t.Fatalf("first EnsureMemberWallet: %v", err)
	}
	second, err := client.EnsureMemberWallet(ctx, "did:privy:user-a", userID)
	if err != nil {
		t.Fatalf("second EnsureMemberWallet: %v", err)
	}
	other, err := client.EnsureMemberWallet(ctx, "did:privy:user-b", UserID("22222222-2222-2222-2222-222222222222"))
	if err != nil {
		t.Fatalf("other EnsureMemberWallet: %v", err)
	}

	// Assert
	if first.SolanaAddress == "" || first.PrivyWalletID == "" {
		t.Fatalf("first wallet ref incomplete: %#v", first)
	}
	if first != second {
		t.Fatalf("repeat call changed wallet: first=%#v second=%#v", first, second)
	}
	if other.SolanaAddress == first.SolanaAddress {
		t.Fatalf("expected distinct addresses for different users")
	}
}

func TestFakeClient_EnsureTreasury_returnsDeterministicAddress(t *testing.T) {
	// Arrange
	client := NewFakeClient()
	groupID := GroupID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")

	// Act
	first, err := client.EnsureTreasury(context.Background(), groupID)
	if err != nil {
		t.Fatalf("first EnsureTreasury: %v", err)
	}
	second, err := client.EnsureTreasury(context.Background(), groupID)
	if err != nil {
		t.Fatalf("second EnsureTreasury: %v", err)
	}

	// Assert
	if first.SolanaAddress == "" || first.PrivyWalletID == "" {
		t.Fatalf("treasury ref incomplete: %#v", first)
	}
	if first != second {
		t.Fatalf("repeat call changed treasury: first=%#v second=%#v", first, second)
	}
}

func TestFakeClient_VerifySession_validAndInvalidTokens(t *testing.T) {
	// Arrange
	client := NewFakeClient()
	token := AccessToken("fixture-session-token")
	RegisterToken(client, token, Identity{
		PrivyUserID: "did:privy:fixture",
		SessionID:   "session-1",
		DisplayName: "Fixture User",
	})

	// Act
	identity, err := client.VerifySession(context.Background(), token)
	_, invalidErr := client.VerifySession(context.Background(), AccessToken("bad-token"))

	// Assert
	if err != nil {
		t.Fatalf("VerifySession valid token: %v", err)
	}
	if identity.PrivyUserID != "did:privy:fixture" {
		t.Fatalf("PrivyUserID = %q", identity.PrivyUserID)
	}
	if invalidErr != ErrInvalidToken {
		t.Fatalf("invalid token err = %v, want %v", invalidErr, ErrInvalidToken)
	}
}
