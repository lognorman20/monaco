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
	Catalog  xstocks.CatalogSearcher
	Symbols  *SymbolResolver
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

func seedTestTreasuryUSDC(t *testing.T, privyClient privy.Client, treasuryAddress string, usdcMicros int64) {
	t.Helper()
	privy.SetTreasuryUSDCBalance(privyClient, treasuryAddress, usdcMicros)
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
	catalog := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(catalog, xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple",
		SolanaMint: jupiter.AAPLxMint,
	})
	xstocks.RegisterCatalogAsset(catalog, xstocks.CatalogAsset{
		Symbol:     "TSLAx",
		Name:       "Tesla",
		SolanaMint: jupiter.TSLAxMint,
	})
	symbols := NewSymbolResolver(catalog)

	signer := NewFakePrivyTreasurySigner()
	swap := NewSwapService(store, buy, jupiterClient, privyClient, signer, "", symbols)
	swap.SetPollConfigForTests(jupiter.TestPollConfig())

	return integrationHarness{
		DB:       db,
		Store:    store,
		Privy:    privyClient,
		Pyth:     pythClient,
		Deposits: NewDepositService(store, privyClient, pythClient, symbols),
		Groups:   NewGroupService(store, privyClient),
		Swap:     swap,
		Redeem:   NewRedeemService(store, privyClient, pythClient, jupiterClient, swap, signer),
		Jupiter:  jupiterClient,
		XStocks:  xstocksResolver,
		Catalog:  catalog,
		Symbols:  symbols,
		ISO:      iso,
	}
}

// registerLiveAAPLxMark gives the group's AAPLx holding a live Pyth mark. Deposit credit and
// redeem refuse to price a holding at cost basis, so any test that moves money against a pot
// holding stock registers the mark it expects.
func registerLiveAAPLxMark(h integrationHarness, groupID string, markMicros int64) {
	pyth.RegisterMarkedPot(h.Pyth, pyth.TreasuryRef{GroupID: groupID}, pyth.NavInput{
		Holdings: []pyth.MarkedHolding{{
			Symbol:   "AAPLx",
			Mint:     jupiter.AAPLxMint,
			MarkUsdc: markMicros,
			Source:   pyth.MarkSourcePyth,
		}},
	})
}
