package app

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func integrationDepositService(t *testing.T) (*DepositService, privy.Client, *sql.DB) {
	h := integrationApp(t)
	return h.Deposits, h.Privy, h.DB
}

func seedFundedDeposit(t *testing.T, deposits *DepositService, privyClient privy.Client) (ObservedSweep, string) {
	t.Helper()
	ctx := context.Background()
	token := privy.AccessToken("deposit-test-" + t.Name())
	privy.RegisterToken(privyClient, token, privy.Identity{PrivyUserID: "did:privy:" + t.Name()})
	store := deposits.store
	sessions := NewSessionService(store, privyClient)
	if _, err := sessions.OpenSession(ctx, string(token)); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	groups := NewGroupService(store, privyClient)
	group, err := groups.CreateGroup(ctx, string(token), "Deposit Fund")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	created, err := deposits.CreateDeposit(ctx, string(token), group.GroupID, 4_000_000)
	if err != nil {
		t.Fatalf("CreateDeposit: %v", err)
	}
	treasury, _ := privyClient.EnsureTreasury(ctx, privy.GroupID(group.GroupID))
	user, _, _ := store.GetUserByPrivyUserID(ctx, "did:privy:"+t.Name())
	sweep := ObservedSweep{
		TxSignature: "SWEEP-unit-test",
		FromAddress: created.Deposit.FromAddress,
		ToAddress:   treasury.SolanaAddress,
		Amount:      4_000_000,
		DepositID:   created.Deposit.ID,
		UserID:      user.ID,
		GroupID:     group.GroupID,
	}
	return sweep, string(token)
}

func TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually(t *testing.T) {
	// Arrange
	deposits, privyClient, _ := integrationDepositService(t)
	sweep, _ := seedFundedDeposit(t, deposits, privyClient)

	// Act
	result, err := deposits.ObserveSweep(context.Background(), sweep)

	// Assert
	if err != nil {
		t.Fatalf("ObserveSweep: %v", err)
	}
	if !result.Credited {
		t.Fatal("expected credit")
	}
	if result.Position.ShareUnits != 4_000_000 {
		t.Fatalf("shareUnits = %d, want 4000000", result.Position.ShareUnits)
	}
	if result.Position.AmountDeposited != 4_000_000 {
		t.Fatalf("amountDeposited = %d, want 4000000", result.Position.AmountDeposited)
	}
}

func TestObserveSweep_confirmedTreasuryArrival_setsDepositStatusConfirmed(t *testing.T) {
	// Arrange
	deposits, privyClient, _ := integrationDepositService(t)
	sweep, _ := seedFundedDeposit(t, deposits, privyClient)

	// Act
	result, err := deposits.ObserveSweep(context.Background(), sweep)

	// Assert
	if err != nil {
		t.Fatalf("ObserveSweep: %v", err)
	}
	if result.Deposit.Status != DepositStatusConfirmed {
		t.Fatalf("status = %q, want confirmed", result.Deposit.Status)
	}
	if result.Deposit.TxSignature != sweep.TxSignature {
		t.Fatalf("txSignature = %q, want %q", result.Deposit.TxSignature, sweep.TxSignature)
	}
}

func TestObserveSweep_multipleDeposits_sumsShareUnitsAndAmountDeposited(t *testing.T) {
	// Arrange
	deposits, privyClient, _ := integrationDepositService(t)
	ctx := context.Background()
	sweep1, token := seedFundedDeposit(t, deposits, privyClient)
	if _, err := deposits.ObserveSweep(ctx, sweep1); err != nil {
		t.Fatalf("first ObserveSweep: %v", err)
	}

	groups := NewGroupService(deposits.store, privyClient)
	groupID := sweep1.GroupID
	created2, err := deposits.CreateDeposit(ctx, token, groupID, 1_000_000)
	if err != nil {
		t.Fatalf("CreateDeposit: %v", err)
	}
	sweep2 := sweep1
	sweep2.TxSignature = "SWEEP-second"
	sweep2.Amount = 1_000_000
	sweep2.DepositID = created2.Deposit.ID

	// Act
	result, err := deposits.ObserveSweep(ctx, sweep2)

	// Assert
	if err != nil {
		t.Fatalf("ObserveSweep: %v", err)
	}
	if result.Position.ShareUnits != 5_000_000 {
		t.Fatalf("shareUnits = %d, want 5000000", result.Position.ShareUnits)
	}
	if result.Position.AmountDeposited != 5_000_000 {
		t.Fatalf("amountDeposited = %d, want 5000000", result.Position.AmountDeposited)
	}
	_ = groups
}

func TestProperty_sweptUsdcEqualsShareUnitsAndAmountDeposited(t *testing.T) {
	// Arrange
	amounts := []int64{1, 50_000, 2_500_000}
	deposits, privyClient, _ := integrationDepositService(t)
	ctx := context.Background()
	token := privy.AccessToken("property-test-" + t.Name())
	privy.RegisterToken(privyClient, token, privy.Identity{PrivyUserID: "did:privy:property-" + t.Name()})
	sessions := NewSessionService(deposits.store, privyClient)
	if _, err := sessions.OpenSession(ctx, string(token)); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	groups := NewGroupService(deposits.store, privyClient)
	group, err := groups.CreateGroup(ctx, string(token), "Property Fund")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	treasury, _ := privyClient.EnsureTreasury(ctx, privy.GroupID(group.GroupID))
	user, _, _ := deposits.store.GetUserByPrivyUserID(ctx, "did:privy:property-"+t.Name())
	for i, amount := range amounts {
		created, err := deposits.CreateDeposit(ctx, string(token), group.GroupID, amount)
		if err != nil {
			t.Fatalf("CreateDeposit: %v", err)
		}
		sweep := ObservedSweep{
			TxSignature: fmt.Sprintf("SWEEP-property-%d", i),
			FromAddress: created.Deposit.FromAddress,
			ToAddress:   treasury.SolanaAddress,
			Amount:      amount,
			DepositID:   created.Deposit.ID,
			UserID:      user.ID,
			GroupID:     group.GroupID,
		}

		// Act
		result, err := deposits.ObserveSweep(ctx, sweep)

		// Assert
		if err != nil {
			t.Fatalf("ObserveSweep amount=%d: %v", amount, err)
		}
		if result.Position.ShareUnits < amount {
			t.Fatalf("amount=%d shareUnits=%d", amount, result.Position.ShareUnits)
		}
	}
}

func TestProperty_multipleDepositsPreserveOneToOneInvariant(t *testing.T) {
	// Arrange
	deposits, privyClient, _ := integrationDepositService(t)
	ctx := context.Background()
	sweep1, token := seedFundedDeposit(t, deposits, privyClient)
	if _, err := deposits.ObserveSweep(ctx, sweep1); err != nil {
		t.Fatalf("first ObserveSweep: %v", err)
	}
	created2, err := deposits.CreateDeposit(ctx, token, sweep1.GroupID, 2_000_000)
	if err != nil {
		t.Fatalf("CreateDeposit: %v", err)
	}
	sweep2 := sweep1
	sweep2.TxSignature = "SWEEP-prop-2"
	sweep2.DepositID = created2.Deposit.ID
	sweep2.Amount = 2_000_000

	// Act
	result, err := deposits.ObserveSweep(ctx, sweep2)

	// Assert
	if err != nil {
		t.Fatalf("ObserveSweep: %v", err)
	}
	if result.Position.ShareUnits != result.Position.AmountDeposited {
		t.Fatalf("shareUnits %d != amountDeposited %d", result.Position.ShareUnits, result.Position.AmountDeposited)
	}
}
