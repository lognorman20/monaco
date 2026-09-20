package app

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/chainlink"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/marks"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

type integrationHarness struct {
	DB       *sql.DB
	Store    *postgres.Store
	Wallets  wallets.Client
	Privy    wallets.Client // legacy test name for Wallets
	Auth     auth.Verifier
	Marks    marks.Client
	Pyth     marks.Client // legacy test name for Marks
	Deposits *DepositService
	Groups   *GroupService
	Swap     *SwapService
	Redeem   *RedeemService
	Dex      dex.Client
	Jupiter  dex.Client // legacy test name for Dex
	Catalog  b20.Catalog
	XStocks  b20.Catalog // legacy test name for Catalog
	Chain    evm.Client
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

func testTxHash(iso *postgres.TestIsolation, label string) string {
	return fmt.Sprintf("0x%s%s", iso.Suffix(), label)
}

func seedTestTreasuryUSDC(t *testing.T, walletClient wallets.Client, treasuryAddress string, usdcMicros int64) {
	t.Helper()
	wallets.SetTreasuryUSDCBalance(walletClient, treasuryAddress, usdcMicros)
}

func openTestSession(t *testing.T, iso *postgres.TestIsolation, sessions *SessionService, verifier auth.Verifier, label, displayName string) SessionResult {
	t.Helper()
	token := auth.AccessToken(iso.UniqueToken(label))
	auth.RegisterToken(verifier, token, auth.Identity{
		DynamicUserID: iso.UniqueDynamicID(label),
		DisplayName:   displayName,
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
	walletClient := wallets.NewFakeClient()
	verifier := auth.NewFakeVerifier()
	marksClient := chainlink.NewFakeClient()
	dexClient := dex.NewFakeClient()
	catalog := b20.NewFakeCatalog()
	aapl, _ := b20.NewPinnedCatalog().ResolveTokenAddress(context.Background(), "AAPLc")
	b20.RegisterAsset(catalog, b20.Asset{Symbol: "AAPLc", Name: "Apple", TokenAddress: aapl, Decimals: 8})
	var chain evm.Client // nil: unit tests skip live receipt polling
	buy := NewBuyService(dexClient, catalog)
	symbols := NewSymbolResolver(catalog)
	swap := NewSwapService(store, buy, dexClient, walletClient, chain, symbols)

	return integrationHarness{
		DB:       db,
		Store:    store,
		Wallets:  walletClient,
		Privy:    walletClient,
		Auth:     verifier,
		Marks:    marksClient,
		Pyth:     marksClient,
		Deposits: NewDepositService(store, verifier, walletClient, marksClient, symbols),
		Groups:   NewGroupService(store, verifier, walletClient),
		Swap:     swap,
		Redeem:   NewRedeemService(store, walletClient, verifier, marksClient, dexClient, swap),
		Dex:      dexClient,
		Jupiter:  dexClient,
		Catalog:  catalog,
		XStocks:  catalog,
		Chain:    chain,
		Symbols:  symbols,
		ISO:      iso,
	}
}
