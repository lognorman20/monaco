package postgres

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
)

func TestNavSnapshot_writtenOnDepositConfirmTransactionConfirmWithdrawalPayout(t *testing.T) {
	// Arrange
	db := integrationDB(t)
	resetTables(t, db)
	store := NewStore(db)
	ctx := context.Background()

	user, err := store.UpsertUser(ctx, "did:privy:nav-snapshot", "Nav Snapshot Tester")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	group, err := store.InsertGroup(ctx, "Nav Snapshot Group", user.ID)
	if err != nil {
		t.Fatalf("InsertGroup: %v", err)
	}
	if _, err := store.InsertTreasury(ctx, group.ID, "privy-treasury-wallet", "FAKEtreasury-nav"); err != nil {
		t.Fatalf("InsertTreasury: %v", err)
	}

	const depositAmount = int64(4_000_000)
	deposit, err := store.InsertDeposit(ctx, user.ID, group.ID, depositAmount, "FAKEmember-nav")
	if err != nil {
		t.Fatalf("InsertDeposit: %v", err)
	}

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, newlyConfirmed, err := store.ConfirmDepositTx(ctx, tx, deposit.ID, "SWEEP-nav-deposit"); err != nil {
		t.Fatalf("ConfirmDepositTx: %v", err)
	} else if !newlyConfirmed {
		t.Fatal("expected newly confirmed deposit")
	}
	if _, err := store.IncrementPositionTx(ctx, tx, user.ID, group.ID, depositAmount, depositAmount); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := store.WriteNavSnapshotOnDepositConfirmTx(ctx, tx, group.ID, depositAmount); err != nil {
		t.Fatalf("WriteNavSnapshotOnDepositConfirmTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit deposit tx: %v", err)
	}
	committed = true

	const buyUSDC = int64(1_000_000)
	const buyOutput = int64(500_000)
	if _, created, err := store.ConfirmBuyTransaction(ctx, ConfirmBuyTransactionParams{
		GroupID:          group.ID,
		Amount:           buyUSDC,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      "BUY-nav-snapshot",
		ExecuteRequestID: "req-nav-snapshot-buy",
		CostBasisPrice:   buyUSDC,
		CostBasisAmount:  buyOutput,
	}); err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	} else if !created {
		t.Fatal("expected created buy transaction")
	}
	const treasuryAfterBuy = depositAmount - buyUSDC
	if err := store.WriteNavSnapshotOnTransactionConfirm(ctx, group.ID, treasuryAfterBuy); err != nil {
		t.Fatalf("WriteNavSnapshotOnTransactionConfirm: %v", err)
	}

	withdrawal, err := store.InsertWithdrawal(ctx, user.ID, group.ID, 500_000, "FAKEpayout-nav")
	if err != nil {
		t.Fatalf("InsertWithdrawal: %v", err)
	}

	payoutTx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx payout: %v", err)
	}
	payoutCommitted := false
	defer func() {
		if !payoutCommitted {
			_ = payoutTx.Rollback()
		}
	}()

	// Act
	if _, newlyPaid, err := store.ConfirmWithdrawalPayoutTx(ctx, payoutTx, withdrawal.ID, "PAYOUT-nav-snapshot", treasuryAfterBuy); err != nil {
		t.Fatalf("ConfirmWithdrawalPayoutTx: %v", err)
	} else if !newlyPaid {
		t.Fatal("expected newly paid withdrawal")
	}
	if err := payoutTx.Commit(); err != nil {
		t.Fatalf("commit payout tx: %v", err)
	}
	payoutCommitted = true

	// Assert
	depositCount, err := store.CountNavSnapshotsByGroupAndReason(ctx, group.ID, NavSnapshotReasonDeposit)
	if err != nil {
		t.Fatalf("CountNavSnapshotsByGroupAndReason deposit: %v", err)
	}
	if depositCount != 1 {
		t.Fatalf("deposit snapshot count = %d, want 1", depositCount)
	}

	txCount, err := store.CountNavSnapshotsByGroupAndReason(ctx, group.ID, NavSnapshotReasonTransactionConfirm)
	if err != nil {
		t.Fatalf("CountNavSnapshotsByGroupAndReason transaction_confirm: %v", err)
	}
	if txCount != 1 {
		t.Fatalf("transaction_confirm snapshot count = %d, want 1", txCount)
	}

	withdrawalCount, err := store.CountNavSnapshotsByGroupAndReason(ctx, group.ID, NavSnapshotReasonWithdrawalPayout)
	if err != nil {
		t.Fatalf("CountNavSnapshotsByGroupAndReason withdrawal_payout: %v", err)
	}
	if withdrawalCount != 1 {
		t.Fatalf("withdrawal_payout snapshot count = %d, want 1", withdrawalCount)
	}

	snapshots, err := store.ListNavSnapshotsByGroup(ctx, group.ID)
	if err != nil {
		t.Fatalf("ListNavSnapshotsByGroup: %v", err)
	}
	if len(snapshots) != 3 {
		t.Fatalf("snapshot rows = %d, want 3", len(snapshots))
	}
	for _, snapshot := range snapshots {
		if snapshot.PotNavMicros <= 0 {
			t.Fatalf("snapshot %s pot_nav_micros must be positive, got %d", snapshot.Reason, snapshot.PotNavMicros)
		}
		if snapshot.NavPerShareMicros <= 0 {
			t.Fatalf("snapshot %s nav_per_share_micros must be positive, got %d", snapshot.Reason, snapshot.NavPerShareMicros)
		}
		if snapshot.TotalShares <= 0 {
			t.Fatalf("snapshot %s total_shares must be positive, got %d", snapshot.Reason, snapshot.TotalShares)
		}
	}
}
