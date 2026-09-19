package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func ensureGroupMember(t *testing.T, store *postgres.Store, groupID, userID string) {
	t.Helper()
	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := store.InsertGroupMemberTx(ctx, tx, groupID, userID); err != nil {
		t.Fatalf("InsertGroupMemberTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func TestFundGroup_rejectsOverBalance(t *testing.T) {
	h := integrationApp(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	session := openTestSession(t, h.ISO, sessions, h.Privy, "fund-over", "Fund Over")
	token := privy.AccessToken(h.ISO.UniqueToken("fund-over"))

	group, err := h.Groups.CreateGroup(ctx, string(token), testGroupName(h.ISO, "fund-over"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	ensureGroupMember(t, h.Store, group.GroupID, session.UserID)

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	privy.SetMemberUSDCBalance(h.Privy, wallet.SolanaAddress, 500_000)

	_, err = h.Deposits.FundGroup(ctx, string(token), group.GroupID, 750_000)
	if !errors.Is(err, ErrInsufficientPlatformBalance) {
		t.Fatalf("FundGroup err = %v, want ErrInsufficientPlatformBalance", err)
	}
}

func TestFundGroup_rejectsNonMember(t *testing.T) {
	h := integrationApp(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	ownerToken := privy.AccessToken(h.ISO.UniqueToken("fund-owner"))
	openTestSession(t, h.ISO, sessions, h.Privy, "fund-owner", "Owner")
	group, err := h.Groups.CreateGroup(ctx, string(ownerToken), testGroupName(h.ISO, "fund-owner"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	outsiderToken := privy.AccessToken(h.ISO.UniqueToken("fund-outsider"))
	outsider := openTestSession(t, h.ISO, sessions, h.Privy, "fund-outsider", "Outsider")
	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, outsider.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	privy.SetMemberUSDCBalance(h.Privy, wallet.SolanaAddress, 1_000_000)

	_, err = h.Deposits.FundGroup(ctx, string(outsiderToken), group.GroupID, 100_000)
	if !errors.Is(err, ErrNotGroupMember) {
		t.Fatalf("FundGroup err = %v, want ErrNotGroupMember", err)
	}
}

func TestFundGroup_creditsPositionAfterConfirmedSweep(t *testing.T) {
	h := integrationApp(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	token := privy.AccessToken(h.ISO.UniqueToken("fund-credit"))
	session := openTestSession(t, h.ISO, sessions, h.Privy, "fund-credit", "Creditor")
	group, err := h.Groups.CreateGroup(ctx, string(token), testGroupName(h.ISO, "fund-credit"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	ensureGroupMember(t, h.Store, group.GroupID, session.UserID)

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, group.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}
	const amount = int64(1_500_000)
	privy.SetMemberUSDCBalance(h.Privy, wallet.SolanaAddress, amount)
	seedTestTreasuryUSDC(t, h.Privy, treasury.SolanaAddress, 0)

	result, err := h.Deposits.FundGroup(ctx, string(token), group.GroupID, amount)
	if err != nil {
		t.Fatalf("FundGroup: %v", err)
	}

	sig := testTxSignature(h.ISO, "fund-credit")
	observe, err := h.Deposits.ObserveSweep(ctx, ObservedSweep{
		TxSignature: sig,
		FromAddress: wallet.SolanaAddress,
		ToAddress:   treasury.SolanaAddress,
		Amount:      amount,
		DepositID:   result.Deposit.ID,
		UserID:      session.UserID,
		GroupID:     group.GroupID,
	})
	if err != nil {
		t.Fatalf("ObserveSweep: %v", err)
	}
	if !observe.Credited {
		t.Fatal("expected newly credited position")
	}
	if observe.Position.ShareUnits != amount {
		t.Fatalf("share_units = %d, want %d", observe.Position.ShareUnits, amount)
	}
}

func TestGetPlatformBalance_subtractsPendingAllocations(t *testing.T) {
	h := integrationApp(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	token := privy.AccessToken(h.ISO.UniqueToken("balance"))
	session := openTestSession(t, h.ISO, sessions, h.Privy, "balance", "Balancer")
	group, err := h.Groups.CreateGroup(ctx, string(token), testGroupName(h.ISO, "balance"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	ensureGroupMember(t, h.Store, group.GroupID, session.UserID)

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	privy.SetMemberUSDCBalance(h.Privy, wallet.SolanaAddress, 2_000_000)

	if _, err := h.Deposits.FundGroup(ctx, string(token), group.GroupID, 600_000); err != nil {
		t.Fatalf("FundGroup: %v", err)
	}

	balance, err := h.Deposits.GetPlatformBalance(ctx, string(token))
	if err != nil {
		t.Fatalf("GetPlatformBalance: %v", err)
	}
	if balance.AvailableUsdcMicros != 1_400_000 {
		t.Fatalf("available = %d, want 1400000", balance.AvailableUsdcMicros)
	}
	if balance.PendingAllocationMicros != 600_000 {
		t.Fatalf("pending = %d, want 600000", balance.PendingAllocationMicros)
	}
}
