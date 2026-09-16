package app

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

type integrationHarness struct {
	DB            *sql.DB
	Store         *postgres.Store
	Privy         privy.Client
	Deposits      *DepositService
	Groups        *GroupService
	Swap          *SwapService
	Jupiter       jupiter.Client
	XStocks       xstocks.Resolver
}

func integrationApp(t *testing.T) integrationHarness {
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
	_, err = db.ExecContext(ctx, "TRUNCATE users, member_wallets, groups, treasuries, deposits, positions, withdrawals, transactions RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("reset tables: %v", err)
	}

	store := postgres.NewStore(db)
	privyClient := privy.NewFakeClient()
	jupiterClient := jupiter.NewFakeClient()
	xstocksResolver := xstocks.NewFakeResolver()
	buy := NewBuyService(jupiterClient, xstocksResolver)

	return integrationHarness{
		DB:       db,
		Store:    store,
		Privy:    privyClient,
		Deposits: NewDepositService(store, privyClient),
		Groups:   NewGroupService(store, privyClient),
		Swap:     NewSwapService(store, buy, jupiterClient, privyClient, NewFakePrivyTreasurySigner()),
		Jupiter:  jupiterClient,
		XStocks:  xstocksResolver,
	}
}

func buildObservedSweep(overrides map[string]any) ObservedSweep {
	sweep := ObservedSweep{
		TxSignature: "SWEEP-test-signature",
		FromAddress: "FAKEmember",
		ToAddress:   "FAKEtreasury",
		Amount:      1_000_000,
		DepositID:   "00000000-0000-0000-0000-000000000001",
		UserID:      "00000000-0000-0000-0000-000000000002",
		GroupID:     "00000000-0000-0000-0000-000000000003",
	}
	if v, ok := overrides["TxSignature"].(string); ok {
		sweep.TxSignature = v
	}
	if v, ok := overrides["FromAddress"].(string); ok {
		sweep.FromAddress = v
	}
	if v, ok := overrides["ToAddress"].(string); ok {
		sweep.ToAddress = v
	}
	if v, ok := overrides["Amount"].(int64); ok {
		sweep.Amount = v
	}
	if v, ok := overrides["DepositID"].(string); ok {
		sweep.DepositID = v
	}
	if v, ok := overrides["UserID"].(string); ok {
		sweep.UserID = v
	}
	if v, ok := overrides["GroupID"].(string); ok {
		sweep.GroupID = v
	}
	return sweep
}

func depositRowToDeposit(row postgres.DepositRow) Deposit {
	return depositFromRow(row)
}
