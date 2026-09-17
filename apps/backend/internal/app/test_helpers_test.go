package app

import (
	"database/sql"
	"os"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

type integrationHarness struct {
	DB            *sql.DB
	Store         *postgres.Store
	Privy         privy.Client
	Pyth          pyth.Client
	Deposits      *DepositService
	Groups        *GroupService
	Swap          *SwapService
	Redeem        *RedeemService
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

	postgres.PrepareIntegrationDB(t, db)

	store := postgres.NewStore(db)
	privyClient := privy.NewFakeClient()
	pythClient := pyth.NewFakeClient()
	jupiterClient := jupiter.NewFakeClient()
	xstocksResolver := xstocks.NewFakeResolver()
	buy := NewBuyService(jupiterClient, xstocksResolver)

	signer := NewFakePrivyTreasurySigner()
	swap := NewSwapService(store, buy, jupiterClient, privyClient, signer)
	swap.SetPollConfigForTests(jupiter.TestPollConfig())

	return integrationHarness{
		DB:       db,
		Store:    store,
		Privy:    privyClient,
		Pyth:     pythClient,
		Deposits: NewDepositService(store, privyClient, pythClient),
		Groups:   NewGroupService(store, privyClient),
		Swap:     swap,
		Redeem:   NewRedeemService(store, privyClient, pythClient, jupiterClient, swap, signer),
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
