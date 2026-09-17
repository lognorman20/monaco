package worker

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func setupPoller(t *testing.T) (*SweepPoller, *workerTestApp, *fakeSolanaRPC) {
	t.Helper()
	testApp := integrationWorkerApp(t)
	rpc := NewFakeSolanaRPC()
	poller := NewSweepPoller(testApp.Store, testApp.Privy, rpc, testApp.Deposits, "relayer-key", NewStubClock(testApp.Now))
	return poller, testApp, rpc
}

func TestSweepPoller_memberBalanceCoversIntent_triggersSubmitSweep(t *testing.T) {
	// Arrange
	poller, testApp, _ := setupPoller(t)
	ctx := context.Background()
	_, memberAddress, _ := seedPendingDeposit(t, testApp)
	privy.SetMemberUSDCBalance(testApp.Privy, memberAddress, 2_000_000)

	// Act
	if err := poller.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Assert
	last, ok := privy.LastSweepRequest(testApp.Privy)
	if !ok {
		t.Fatal("expected SubmitSweep call")
	}
	if last.MemberAddress != memberAddress {
		t.Fatalf("member = %q, want %q", last.MemberAddress, memberAddress)
	}
	if last.Amount != 2_000_000 {
		t.Fatalf("amount = %d, want 2000000", last.Amount)
	}
}

func TestSweepPoller_afterBroadcast_observeSweepRunsWhenBalanceBelowIntent(t *testing.T) {
	// Arrange
	poller, testApp, rpc := setupPoller(t)
	ctx := context.Background()
	deposit, memberAddress, _ := seedPendingDeposit(t, testApp)
	privy.SetMemberUSDCBalance(testApp.Privy, memberAddress, 2_000_000)

	// Act — broadcast while balance covers intent; confirmation not ready yet
	if err := poller.Tick(ctx); err != nil {
		t.Fatalf("first Tick: %v", err)
	}

	updated, found, err := testApp.Store.GetDepositByID(ctx, deposit.ID)
	if err != nil || !found {
		t.Fatalf("GetDepositByID: found=%v err=%v", found, err)
	}
	if !updated.TxSignature.Valid || updated.TxSignature.String == "" {
		t.Fatal("expected broadcast tx_signature persisted on pending deposit")
	}
	if updated.Status != "pending" {
		t.Fatalf("status = %q, want pending after broadcast", updated.Status)
	}

	sig := updated.TxSignature.String
	privy.SetMemberUSDCBalance(testApp.Privy, memberAddress, 0)
	rpc.Confirm(sig)

	// Act — member balance below intent but stored sig should still confirm and credit
	if err := poller.Tick(ctx); err != nil {
		t.Fatalf("second Tick: %v", err)
	}

	// Assert
	confirmed, found, err := testApp.Store.GetDepositByID(ctx, deposit.ID)
	if err != nil || !found {
		t.Fatalf("GetDepositByID after confirm: found=%v err=%v", found, err)
	}
	if confirmed.Status != "confirmed" {
		t.Fatalf("status = %q, want confirmed", confirmed.Status)
	}
	position, hasPosition, err := testApp.Store.GetPosition(ctx, deposit.UserID, deposit.GroupID)
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}
	if !hasPosition || position.ShareUnits != deposit.Amount {
		t.Fatalf("share_units = %d, want %d", position.ShareUnits, deposit.Amount)
	}
}

func TestSweepPoller_memberBalanceBelowIntent_doesNotSweep(t *testing.T) {
	// Arrange
	poller, testApp, _ := setupPoller(t)
	ctx := context.Background()
	_, memberAddress, _ := seedPendingDeposit(t, testApp)
	privy.SetMemberUSDCBalance(testApp.Privy, memberAddress, 0)

	// Act
	if err := poller.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Assert
	if _, ok := privy.LastSweepRequest(testApp.Privy); ok {
		t.Fatal("expected no SubmitSweep call")
	}
}
