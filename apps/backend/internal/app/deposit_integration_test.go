package app

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func integrationApp(t *testing.T) (*DepositService, privy.Client, *sql.DB) {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "TRUNCATE users, member_wallets, groups, treasuries, deposits, positions, withdrawals RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("reset tables: %v", err)
	}
	store := postgres.NewStore(db)
	privyClient := privy.NewFakeClient()
	return NewDepositService(store, privyClient), privyClient, db
}

func TestObserveSweep_duplicateSignature_doesNotDoubleCredit(t *testing.T) {
	// Arrange
	deposits, privyClient, _ := integrationApp(t)
	ctx := context.Background()
	sweep, _ := seedFundedDeposit(t, deposits, privyClient)
	first, err := deposits.ObserveSweep(ctx, sweep)
	if err != nil {
		t.Fatalf("first ObserveSweep: %v", err)
	}

	// Act
	second, err := deposits.ObserveSweep(ctx, sweep)

	// Assert
	if err != nil {
		t.Fatalf("second ObserveSweep: %v", err)
	}
	if second.Credited {
		t.Fatal("expected no second credit")
	}
	if second.Position.ShareUnits != first.Position.ShareUnits {
		t.Fatalf("shareUnits changed from %d to %d", first.Position.ShareUnits, second.Position.ShareUnits)
	}
}

func TestObserveSweep_concurrentDuplicateSignature_creditsOnceOnly(t *testing.T) {
	// Arrange
	deposits, privyClient, _ := integrationApp(t)
	ctx := context.Background()
	sweep, _ := seedFundedDeposit(t, deposits, privyClient)

	// Act
	var wg sync.WaitGroup
	results := make([]ObserveSweepResult, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = deposits.ObserveSweep(ctx, sweep)
		}(i)
	}
	wg.Wait()

	// Assert
	credited := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("ObserveSweep[%d]: %v", i, err)
		}
		if results[i].Credited {
			credited++
		}
	}
	if credited != 1 {
		t.Fatalf("credited count = %d, want 1", credited)
	}
	if results[0].Position.ShareUnits != sweep.Amount {
		t.Fatalf("shareUnits = %d, want %d", results[0].Position.ShareUnits, sweep.Amount)
	}
}

func TestProperty_observeSweepIdempotent(t *testing.T) {
	// Arrange
	deposits, privyClient, _ := integrationApp(t)
	ctx := context.Background()
	sweep, _ := seedFundedDeposit(t, deposits, privyClient)
	first, err := deposits.ObserveSweep(ctx, sweep)
	if err != nil {
		t.Fatalf("first ObserveSweep: %v", err)
	}

	// Act
	for i := 0; i < 3; i++ {
		again, err := deposits.ObserveSweep(ctx, sweep)
		if err != nil {
			t.Fatalf("repeat ObserveSweep: %v", err)
		}
		if again.Credited {
			t.Fatal("expected idempotent no-op credit")
		}
		if again.Position.ShareUnits != first.Position.ShareUnits {
			t.Fatalf("shareUnits drifted to %d", again.Position.ShareUnits)
		}
	}
}

func TestObserveSweep_memberWalletOnlyBalance_doesNotCreditPosition(t *testing.T) {
	// Arrange
	deposits, privyClient, _ := integrationApp(t)
	sweep, _ := seedFundedDeposit(t, deposits, privyClient)
	sweep.ToAddress = sweep.FromAddress
	privy.SetMemberUSDCBalance(privyClient, sweep.FromAddress, sweep.Amount)

	// Act
	_, err := deposits.ObserveSweep(context.Background(), sweep)

	// Assert
	if err == nil {
		t.Fatal("expected invalid sweep target error")
	}
	pos, found, err := deposits.store.GetPosition(context.Background(), sweep.UserID, sweep.GroupID)
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}
	if found && pos.ShareUnits != 0 {
		t.Fatalf("shareUnits = %d, want 0", pos.ShareUnits)
	}
}
