package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

// portfolioFixture is Ada in two cabals that both own Apple.
//
//	Sunday Investors: Ada put in $600, Ben $400, so Ada's slice is 60%. The cabal bought
//	  4 AAPLx for $800 and keeps $200 cash. Apple is marked at $250, so the pot is $1,200
//	  and Ada's slice is $720: $600 of Apple (2.4 shares, cost $480) and $120 cash.
//	Semis or bust: Ada alone put in $500. The cabal bought 1 AAPLx for $200 and 2 TSLAx
//	  for $200, keeping $100. Apple at $250 and Tesla at $90 make the pot $530, all Ada's.
//
// Across both: $1,250 in cabals against $1,100 put in; $850 of Apple, $180 of Tesla, $220
// cash.
type portfolioFixture struct {
	h         integrationHarness
	home      *HomeService
	portfolio *PortfolioService
	token     string
	adaID     string
	benID     string
	sunday    string
	semis     string
}

const (
	portfolioAAPLMark = 250_000_000
	portfolioTSLAMark = 90_000_000
)

func newPortfolioFixture(t *testing.T) portfolioFixture {
	t.Helper()
	h := integrationApp(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)
	ada := openTestSession(t, h.ISO, sessions, h.Privy, "ada", "Ada")
	ben := openTestSession(t, h.ISO, sessions, h.Privy, "ben", "Ben")
	token := h.ISO.UniqueToken("ada")

	fx := portfolioFixture{h: h, token: token, adaID: ada.UserID, benID: ben.UserID}
	fx.home = NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)
	fx.portfolio = NewPortfolioService(fx.home)

	sunday, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "sunday"))
	if err != nil {
		t.Fatalf("CreateGroup(sunday): %v", err)
	}
	h.ISO.TrackGroup(sunday.GroupID)
	semis, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "semis"))
	if err != nil {
		t.Fatalf("CreateGroup(semis): %v", err)
	}
	h.ISO.TrackGroup(semis.GroupID)
	fx.sunday, fx.semis = sunday.GroupID, semis.GroupID

	execSQL(t, h.DB, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, fx.sunday, fx.benID)
	fx.fund(t, fx.sunday, fx.adaID, 600_000_000, "ada-sunday", time.Now().Add(-72*time.Hour))
	fx.fund(t, fx.sunday, fx.benID, 400_000_000, "ben-sunday", time.Now().Add(-73*time.Hour))
	fx.fund(t, fx.semis, fx.adaID, 500_000_000, "ada-semis", time.Now().Add(-72*time.Hour))

	fx.buy(t, fx.sunday, jupiter.AAPLxMint, 800_000_000, 400_000_000, "sunday-aapl")
	fx.buy(t, fx.semis, jupiter.AAPLxMint, 200_000_000, 100_000_000, "semis-aapl")
	fx.buy(t, fx.semis, jupiter.TSLAxMint, 200_000_000, 200_000_000, "semis-tsla")

	privy.SetTreasuryUSDCBalance(h.Privy, sunday.TreasuryAddress, 200_000_000)
	privy.SetTreasuryUSDCBalance(h.Privy, semis.TreasuryAddress, 100_000_000)
	pyth.RegisterMarkedPot(h.Pyth, pyth.TreasuryRef{GroupID: fx.sunday}, pyth.NavInput{Holdings: []pyth.MarkedHolding{
		{Symbol: "AAPLx", Mint: jupiter.AAPLxMint, MarkUsdc: portfolioAAPLMark, Source: pyth.MarkSourcePyth},
	}})
	pyth.RegisterMarkedPot(h.Pyth, pyth.TreasuryRef{GroupID: fx.semis}, pyth.NavInput{Holdings: []pyth.MarkedHolding{
		{Symbol: "AAPLx", Mint: jupiter.AAPLxMint, MarkUsdc: portfolioAAPLMark, Source: pyth.MarkSourcePyth},
		{Symbol: "TSLAx", Mint: jupiter.TSLAxMint, MarkUsdc: portfolioTSLAMark, Source: pyth.MarkSourcePyth},
	}})

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, fx.adaID)
	if err != nil || !found {
		t.Fatalf("member wallet for Ada: found=%v err=%v", found, err)
	}
	privy.SetMemberUSDCBalance(h.Privy, wallet.SolanaAddress, 42_500_000)
	return fx
}

// fund gives a member a confirmed deposit and the share units it bought at $1.
func (fx portfolioFixture) fund(t *testing.T, groupID, userID string, micros int64, label string, at time.Time) {
	t.Helper()
	execSQL(t, fx.h.DB,
		`INSERT INTO positions (user_id, group_id, share_units, amount_deposited) VALUES ($1, $2, $3, $3)`,
		userID, groupID, micros)
	execSQL(t, fx.h.DB,
		`INSERT INTO deposits (user_id, group_id, amount, from_address, status, tx_signature, created_at)
		 VALUES ($1, $2, $3, 'wallet-'||$4, 'confirmed', $5, $6)`,
		userID, groupID, micros, label, testTxSignature(fx.h.ISO, "dep-"+label), at)
}

// buy records a confirmed cabal buy of atomics for usdc.
func (fx portfolioFixture) buy(t *testing.T, groupID, mint string, usdc, atomics int64, label string) {
	t.Helper()
	execSQL(t, fx.h.DB,
		`INSERT INTO transactions (group_id, amount, action, input_mint, output_mint, status,
		   tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, confirmed_at)
		 VALUES ($1, $2, 'buy', $3, $4, 'confirmed', $5, $6, $2, $7, now())`,
		groupID, usdc, jupiter.USDCMint, mint, testTxSignature(fx.h.ISO, label), testRequestID(fx.h.ISO, label), atomics)
}

func (fx portfolioFixture) get(t *testing.T) PortfolioResult {
	t.Helper()
	result, err := fx.portfolio.GetPortfolio(context.Background(), fx.token)
	if err != nil {
		t.Fatalf("GetPortfolio: %v", err)
	}
	return result
}

func findHolding(t *testing.T, result PortfolioResult, symbol string) PortfolioHolding {
	t.Helper()
	for _, holding := range result.Holdings {
		if holding.Symbol == symbol {
			return holding
		}
	}
	t.Fatalf("no %s holding in %+v", symbol, result.Holdings)
	return PortfolioHolding{}
}

func TestGetPortfolio_mergesOneStockHeldInTwoCabals(t *testing.T) {
	// Arrange
	fx := newPortfolioFixture(t)

	// Act
	apple := findHolding(t, fx.get(t), "AAPLx")

	// Assert: $600 of Sunday's Apple (60% of $1,000) and $250 of Semis' (all of $250).
	if apple.ValueMicros != 850_000_000 {
		t.Fatalf("Apple value = %d, want 850000000", apple.ValueMicros)
	}
	if apple.Name != "Apple" || apple.Kind != "stock" {
		t.Errorf("Apple name/kind = %q/%q, want Apple/stock", apple.Name, apple.Kind)
	}
	if len(apple.Cabals) != 2 {
		t.Fatalf("Apple cabals = %d, want 2", len(apple.Cabals))
	}
	first, second := apple.Cabals[0], apple.Cabals[1]
	if first.GroupID != fx.sunday || first.ValueMicros != 600_000_000 || first.Quantity != "2.4" {
		t.Errorf("first cabal = %s %d %q, want Sunday 600000000 2.4", first.GroupID, first.ValueMicros, first.Quantity)
	}
	if second.GroupID != fx.semis || second.ValueMicros != 250_000_000 || second.Quantity != "1" {
		t.Errorf("second cabal = %s %d %q, want Semis 250000000 1", second.GroupID, second.ValueMicros, second.Quantity)
	}
}

func TestGetPortfolio_holdingPnLIsTheSliceOfEachCabalsCostBasis(t *testing.T) {
	// Arrange
	fx := newPortfolioFixture(t)

	// Act
	result := fx.get(t)
	apple := findHolding(t, result, "AAPLx")
	tesla := findHolding(t, result, "TSLAx")

	// Assert: Apple cost Ada $480 + $200 and is worth $850; Tesla cost $200, worth $180.
	if apple.DollarPnLMicros != 170_000_000 || apple.PercentReturn == nil || *apple.PercentReturn != "0.25" {
		t.Errorf("Apple P&L = %d / %v, want 170000000 / 0.25", apple.DollarPnLMicros, apple.PercentReturn)
	}
	if apple.Cabals[0].DollarPnLMicros != 120_000_000 {
		t.Errorf("Sunday's Apple P&L = %d, want 120000000 (60%% of the $200 gain)", apple.Cabals[0].DollarPnLMicros)
	}
	if tesla.DollarPnLMicros != -20_000_000 || tesla.PercentReturn == nil || *tesla.PercentReturn != "-0.1" {
		t.Errorf("Tesla P&L = %d / %v, want -20000000 / -0.1", tesla.DollarPnLMicros, tesla.PercentReturn)
	}
}

func TestGetPortfolio_partsAddUpToTheTotal(t *testing.T) {
	// Arrange
	fx := newPortfolioFixture(t)

	// Act
	result := fx.get(t)

	// Assert
	if result.TotalMicros != 1_250_000_000 {
		t.Fatalf("total = %d, want 1250000000", result.TotalMicros)
	}
	if result.CashMicros != 220_000_000 {
		t.Errorf("cash = %d, want 220000000 ($120 in Sunday, $100 in Semis)", result.CashMicros)
	}
	var sum int64
	for _, holding := range result.Holdings {
		sum += holding.ValueMicros
	}
	if sum+result.CashMicros != result.TotalMicros {
		t.Errorf("holdings %d + cash %d != total %d", sum, result.CashMicros, result.TotalMicros)
	}
	if result.DollarPnLMicros != 150_000_000 || result.PercentReturn == nil || *result.PercentReturn != "0.136364" {
		t.Errorf("all-time = %d / %v, want 150000000 / 0.136364", result.DollarPnLMicros, result.PercentReturn)
	}
}

func TestGetPortfolio_sortsByValueAndSaysEachHoldingsShareOfTheTotal(t *testing.T) {
	// Arrange
	fx := newPortfolioFixture(t)

	// Act
	result := fx.get(t)

	// Assert
	if len(result.Holdings) != 2 || result.Holdings[0].Symbol != "AAPLx" || result.Holdings[1].Symbol != "TSLAx" {
		t.Fatalf("holdings = %+v, want Apple then Tesla", result.Holdings)
	}
	if result.Holdings[0].ShareOfTotal != "0.68" || result.Holdings[1].ShareOfTotal != "0.144" {
		t.Errorf("shares = %q, %q, want 0.68, 0.144", result.Holdings[0].ShareOfTotal, result.Holdings[1].ShareOfTotal)
	}
}

// The portfolio is what Home's "Your money in cabals" is made of, so the two must agree.
func TestGetPortfolio_totalIsHomesMoneyInCabals(t *testing.T) {
	// Arrange
	fx := newPortfolioFixture(t)

	// Act
	result := fx.get(t)
	dashboard, err := fx.home.GetHomeDashboard(context.Background(), fx.token, HomeLeaderboardRangeALL)
	if err != nil {
		t.Fatalf("GetHomeDashboard: %v", err)
	}

	// Assert
	if got := FormatUsdDecimal(result.TotalMicros); got != dashboard.NetWorthUsd {
		t.Errorf("portfolio total %s != Home net worth %s", got, dashboard.NetWorthUsd)
	}
	if got := FormatSignedUsdDecimal(result.DollarPnLMicros); got != dashboard.NetWorthDollarPnL {
		t.Errorf("portfolio P&L %s != Home P&L %s", got, dashboard.NetWorthDollarPnL)
	}
}

func TestGetPortfolio_carriesTheAccountBalanceOutsideTheTotal(t *testing.T) {
	// Arrange
	fx := newPortfolioFixture(t)

	// Act
	result := fx.get(t)

	// Assert
	if result.AccountBalanceMicros == nil || *result.AccountBalanceMicros != 42_500_000 {
		t.Fatalf("account balance = %v, want 42500000", result.AccountBalanceMicros)
	}
	if result.TotalMicros != 1_250_000_000 {
		t.Errorf("total = %d, want the account balance left out of it", result.TotalMicros)
	}
}

// A cabal whose pot cannot be priced is counted, not shown at zero: zero would tell Ada
// her money there is gone.
func TestGetPortfolio_countsACabalThatCannotBeValued(t *testing.T) {
	// Arrange
	fx := newPortfolioFixture(t)
	fx.home = NewHomeService(fx.h.Store, failingTreasuryRPC{Client: fx.h.Privy}, fx.h.Pyth, nil, fx.h.Symbols)
	fx.portfolio = NewPortfolioService(fx.home)

	// Act
	result := fx.get(t)

	// Assert
	if result.UnvaluedCabals != 2 {
		t.Fatalf("unvalued = %d, want 2 with the treasury read down", result.UnvaluedCabals)
	}
	if result.TotalMicros != 0 || len(result.Holdings) != 0 {
		t.Errorf("total = %d, holdings = %d, want nothing invented", result.TotalMicros, len(result.Holdings))
	}
}

// A price outage values holdings at what the cabal paid, as every screen does; the
// portfolio still renders, with no gain.
func TestGetPortfolio_priceOutageCarriesHoldingsAtCost(t *testing.T) {
	// Arrange
	fx := newPortfolioFixture(t)
	for _, groupID := range []string{fx.sunday, fx.semis} {
		pyth.RegisterMarkedPotError(fx.h.Pyth, pyth.TreasuryRef{GroupID: groupID}, errors.New("hermes unavailable"))
	}

	// Act
	result := fx.get(t)

	// Assert
	apple := findHolding(t, result, "AAPLx")
	if apple.ValueMicros != 680_000_000 || apple.DollarPnLMicros != 0 {
		t.Errorf("Apple at cost = %d / %d, want 680000000 / 0", apple.ValueMicros, apple.DollarPnLMicros)
	}
	if result.UnvaluedCabals != 0 {
		t.Errorf("unvalued = %d, want 0: a pot at cost is still valued", result.UnvaluedCabals)
	}
}

func TestGetPortfolio_memberInNoCabalsHasAnEmptyPortfolio(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	sessions := NewSessionService(h.Store, h.Privy)
	openTestSession(t, h.ISO, sessions, h.Privy, "loner", "Lone")
	portfolio := NewPortfolioService(NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols))

	// Act
	result, err := portfolio.GetPortfolio(context.Background(), h.ISO.UniqueToken("loner"))

	// Assert
	if err != nil {
		t.Fatalf("GetPortfolio: %v", err)
	}
	if result.TotalMicros != 0 || result.Holdings == nil || len(result.Holdings) != 0 || result.PercentReturn != nil {
		t.Errorf("result = %+v, want an empty portfolio with no return", result)
	}
}

func TestGetPortfolio_rejectsAnUnknownToken(t *testing.T) {
	h := integrationApp(t)
	portfolio := NewPortfolioService(NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols))

	_, err := portfolio.GetPortfolio(context.Background(), "not-a-token")

	if !errors.Is(err, privy.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestShareOfTotalDecimal(t *testing.T) {
	cases := []struct {
		part, total int64
		want        string
	}{
		{850, 1250, "0.68"},
		{180, 1250, "0.144"},
		{1, 3, "0.333333"},
		{0, 1250, "0"},
		{1250, 1250, "1"},
		{5, 0, "0"},
		{1, 10_000_000, "0"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d/%d", tc.part, tc.total), func(t *testing.T) {
			if got := shareOfTotalDecimal(tc.part, tc.total); got != tc.want {
				t.Errorf("shareOfTotalDecimal(%d, %d) = %q, want %q", tc.part, tc.total, got, tc.want)
			}
		})
	}
}

// The same ids and tints are pinned on iOS (PortfolioTests.tintMatchesTheBackend), so the
// two copies of the rule cannot drift apart without a test failing on one side.
func TestCabalTintName_matchesTheAppsRule(t *testing.T) {
	cases := []struct{ id, want string }{
		{"g1", "moss"},
		{"g2", "ochre"},
		{"g3", "plum"},
		{"g5", "pine"},
		{"3f2504e0-4f89-11d3-9a0c-0305e82c3301", "ochre"},
		// The app lowercases and trims before hashing; Swift's UUID strings are uppercase.
		{" 3F2504E0-4F89-11D3-9A0C-0305E82C3301 ", "ochre"},
	}
	for _, tc := range cases {
		if got := CabalTintName(tc.id); got != tc.want {
			t.Errorf("CabalTintName(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}
