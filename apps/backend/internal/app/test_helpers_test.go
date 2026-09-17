package app

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

type integrationHarness struct {
	DB       *sql.DB
	Store    *postgres.Store
	Privy    privy.Client
	Pyth     pyth.Client
	Deposits *DepositService
	Groups   *GroupService
	Swap     *SwapService
	Redeem   *RedeemService
	Jupiter  jupiter.Client
	XStocks  xstocks.Resolver
	ISO      *postgres.TestIsolation
}

func integrationDB(t *testing.T) (*sql.DB, *postgres.TestIsolation) {
	t.Helper()
	db := postgres.OpenTestDB(t)
	return db, postgres.PrepareTestDB(t, db)
}

func testGroupName(iso *postgres.TestIsolation, label string) string {
	return fmt.Sprintf("%s-%s Fund", iso.Suffix(), label)
}

func testRequestID(iso *postgres.TestIsolation, label string) string {
	return fmt.Sprintf("req-%s-%s", iso.Suffix(), label)
}

func testTxSignature(iso *postgres.TestIsolation, label string) string {
	return fmt.Sprintf("sig-%s-%s", iso.Suffix(), label)
}

func openTestSession(t *testing.T, iso *postgres.TestIsolation, sessions *SessionService, privyClient privy.Client, label, displayName string) SessionResult {
	t.Helper()
	token := privy.AccessToken(iso.UniqueToken(label))
	privy.RegisterToken(privyClient, token, privy.Identity{
		PrivyUserID: iso.UniquePrivyID(label),
		DisplayName: displayName,
	})
	result, err := sessions.OpenSession(context.Background(), string(token))
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	iso.TrackUser(result.UserID)
	return result
}

func integrationApp(t *testing.T) integrationHarness {
	t.Helper()

	db, iso := integrationDB(t)
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
		ISO:      iso,
	}
}
