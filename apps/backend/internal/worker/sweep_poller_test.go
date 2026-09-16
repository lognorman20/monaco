package worker

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func setupPoller(t *testing.T) (*SweepPoller, privy.Client, *fakeSolanaRPC, *app.DepositService, *postgres.Store) {
	t.Helper()
	testApp := integrationWorkerApp(t)
	rpc := NewFakeSolanaRPC()
	poller := NewSweepPoller(testApp.Store, testApp.Privy, rpc, testApp.Deposits, "relayer-key", NewStubClock(testApp.Now))
	return poller, testApp.Privy, rpc, testApp.Deposits, testApp.Store
}

func TestSweepPoller_memberBalanceCoversIntent_triggersSubmitSweep(t *testing.T) {
	// Arrange
	poller, privyClient, _, _, store := setupPoller(t)
	ctx := context.Background()
	_, memberAddress, _ := seedPendingDeposit(t, store, privyClient)
	privy.SetMemberUSDCBalance(privyClient, memberAddress, 2_000_000)

	// Act
	if err := poller.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Assert
	last, ok := privy.LastSweepRequest(privyClient)
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

func TestSweepPoller_memberBalanceBelowIntent_doesNotSweep(t *testing.T) {
	// Arrange
	poller, privyClient, _, _, store := setupPoller(t)
	ctx := context.Background()
	_, memberAddress, _ := seedPendingDeposit(t, store, privyClient)
	privy.SetMemberUSDCBalance(privyClient, memberAddress, 0)

	// Act
	if err := poller.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Assert
	if _, ok := privy.LastSweepRequest(privyClient); ok {
		t.Fatal("expected no SubmitSweep call")
	}
}
