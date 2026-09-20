package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

func TestCreditUncreditedTreasuryUSDC_creditsSharesAndDepositedTogether(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := integrationApp(t)

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "reconcile", "Reconcile User")
	token := string(auth.AccessToken(h.ISO.UniqueToken("reconcile")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "reconcile"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	if err := insertGroupMember(t, ctx, h.Store, group.GroupID, session.UserID); err != nil {
		t.Fatalf("insertGroupMember: %v", err)
	}

	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, group.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}

	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, 200_000, 200_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	wallets.SetTreasuryUSDCBalance(h.Privy, treasury.Address, 400_000)

	credited, err := h.Deposits.CreditUncreditedTreasuryUSDC(ctx, group.GroupID)
	if err != nil {
		t.Fatalf("CreditUncreditedTreasuryUSDC: %v", err)
	}
	if !credited {
		t.Fatal("expected credited=true")
	}

	position, hasPosition, err := h.Store.GetPosition(ctx, session.UserID, group.GroupID)
	if err != nil || !hasPosition {
		t.Fatalf("GetPosition: found=%v err=%v", hasPosition, err)
	}
	if position.ShareUnits != 400_000 {
		t.Fatalf("share_units = %d, want 400000", position.ShareUnits)
	}
	if position.AmountDeposited != 400_000 {
		t.Fatalf("amount_deposited = %d, want 400000", position.AmountDeposited)
	}
}

func TestGetGroupView_afterSecondDeposit_showsZeroPnL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "view-pnl", "View PnL")
	token := string(auth.AccessToken(h.ISO.UniqueToken("view-pnl")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "view-pnl"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	if err := insertGroupMember(t, ctx, h.Store, group.GroupID, session.UserID); err != nil {
		t.Fatalf("insertGroupMember: %v", err)
	}

	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, group.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}

	confirmDeposit := func(amount int64) {
		t.Helper()
		tx, err := h.Store.BeginTx(ctx)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, amount, amount); err != nil {
			t.Fatalf("IncrementPositionTx: %v", err)
		}
		if err := h.Store.WriteNavSnapshotOnDepositConfirmTx(ctx, tx, group.GroupID, amount); err != nil {
			t.Fatalf("WriteNavSnapshotOnDepositConfirmTx: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
	}

	confirmDeposit(200_000)
	wallets.SetTreasuryUSDCBalance(h.Privy, treasury.Address, 200_000)

	confirmDeposit(200_000)
	wallets.SetTreasuryUSDCBalance(h.Privy, treasury.Address, 400_000)

	view, err := home.GetGroupView(ctx, token, group.GroupID)
	if err != nil {
		t.Fatalf("GetGroupView: %v", err)
	}
	if view.You.EquityUsd != "0.40" {
		t.Fatalf("equityUsd = %q, want 0.40", view.You.EquityUsd)
	}
	if view.You.DollarPnL != "+0.00" {
		t.Fatalf("dollarPnL = %q, want +0.00", view.You.DollarPnL)
	}
	if view.You.PercentReturn == nil || *view.You.PercentReturn != "0" {
		t.Fatalf("percentReturn = %v, want 0", view.You.PercentReturn)
	}
}

func TestGetGroupView_treasurySurplusWithoutShareCredit_reconcilesOnRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "surplus", "Surplus User")
	token := string(auth.AccessToken(h.ISO.UniqueToken("surplus")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "surplus"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	if err := insertGroupMember(t, ctx, h.Store, group.GroupID, session.UserID); err != nil {
		t.Fatalf("insertGroupMember: %v", err)
	}

	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, group.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}

	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, 200_000, 200_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	wallets.SetTreasuryUSDCBalance(h.Privy, treasury.Address, 400_000)

	view, err := home.GetGroupView(ctx, token, group.GroupID)
	if err != nil {
		t.Fatalf("GetGroupView: %v", err)
	}
	if view.You.EquityUsd != "0.40" {
		t.Fatalf("equityUsd = %q, want 0.40", view.You.EquityUsd)
	}
	if view.You.DollarPnL != "+0.00" {
		t.Fatalf("dollarPnL = %q, want +0.00", view.You.DollarPnL)
	}
}

func insertGroupMember(t *testing.T, ctx context.Context, store *postgres.Store, groupID, userID string) error {
	t.Helper()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		return err
	}
	if err := store.InsertGroupMemberTx(ctx, tx, groupID, userID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
