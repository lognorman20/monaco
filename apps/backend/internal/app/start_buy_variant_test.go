package app

import (
	"context"
	"math/big"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/catalog"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/solana/mintinfo"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/packages/domain"
)

const (
	variantTestSpaceXPreMint = "PreANxuXjsy2pvisWWMNB6YaJNzr7681wJJr2rHsfTh"
	variantTestTSpaceXMint   = "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v"
	variantTestUSDCMicros    = 10_000_000
)

type variantPriceClient struct {
	prices map[string]jupiter.TokenPrice
}

func (f *variantPriceClient) Prices(_ context.Context, mints []string) (map[string]jupiter.TokenPrice, error) {
	out := make(map[string]jupiter.TokenPrice, len(mints))
	for _, mint := range mints {
		if p, ok := f.prices[mint]; ok {
			out[mint] = p
		}
	}
	return out, nil
}

func freshVariantStock(price float64) *jupiter.StockData {
	return &jupiter.StockData{
		Price:     price,
		Mcap:      1.958e12,
		UpdatedAt: time.Now().UTC(),
	}
}

func variantTesseraSpaceX() xstocks.CatalogAsset {
	return xstocks.CatalogAsset{
		Symbol:         "tSpaceX",
		Name:           "T-SpaceX",
		SolanaMint:     variantTestTSpaceXMint,
		Kind:           xstocks.AssetKindPreIPO,
		Source:         xstocks.AssetSourceTessera,
		Issuer:         "tessera",
		IssuerName:     "Tessera",
		UnderlyingID:   "spacex",
		Decimals:       9,
		TransferFeeBps: 20,
		Routable:       true,
	}.Normalize()
}

func variantPreStocksSpaceX() xstocks.CatalogAsset {
	return xstocks.CatalogAsset{
		Symbol:         "SPACEX",
		Name:           "SpaceX",
		SolanaMint:     variantTestSpaceXPreMint,
		Kind:           xstocks.AssetKindPreIPO,
		Source:         xstocks.AssetSourcePreStocks,
		Issuer:         "prestocks",
		IssuerName:     "PreStocks",
		UnderlyingID:   "spacex",
		Decimals:       9,
		TransferFeeBps: 100,
		Routable:       true,
	}.Normalize()
}

func spacexVariantBuyService(t *testing.T, prestocksMult *big.Rat, prices jupiter.PriceClient) (*BuyService, jupiter.Client) {
	t.Helper()
	jc := jupiter.NewFakeClient()
	if prices == nil {
		prices = &variantPriceClient{prices: map[string]jupiter.TokenPrice{
			variantTestTSpaceXMint:   {StockData: freshVariantStock(746.61)},
			variantTestSpaceXPreMint: {StockData: freshVariantStock(149.32)},
		}}
	}
	xs := xstocks.NewFakeCatalogSearcher()
	tessera := catalog.NewFakeSource(variantTesseraSpaceX())
	prestocks := catalog.NewFakeSource(variantPreStocksSpaceX())
	mints := mintinfo.NewFakeReader(map[string]mintinfo.Info{
		variantTestTSpaceXMint:   {Decimals: 9, TransferFeeBps: 20, UiMultiplier: big.NewRat(1, 1)},
		variantTestSpaceXPreMint: {Decimals: 9, TransferFeeBps: 100, UiMultiplier: prestocksMult},
	})
	prober := xstocks.NewFakeRoutabilityProber(true)
	comp := catalog.NewCompositeWithSources(xs, []catalog.TaggedSource{
		{Source: tessera, SourceID: xstocks.AssetSourceTessera},
		{Source: prestocks, SourceID: xstocks.AssetSourcePreStocks},
	}, prober, mints, prices)
	resolver := catalog.NewResolverWithCatalog(xstocks.NewFakeResolver(), comp)
	buy := NewBuyService(jc, resolver)
	buy.SetMintCatalog(comp)
	buy.SetBuyVariantPicker(comp)
	registerSpaceXVariantQuotes(t, jc)
	return buy, jc
}

func registerSpaceXVariantQuotes(t *testing.T, jc jupiter.Client) {
	t.Helper()
	jupiter.RegisterQuoteBuy(jc, variantTestSpaceXPreMint, variantTestUSDCMicros, jupiter.BuyQuote{
		Routable:   true,
		OutputMint: variantTestSpaceXPreMint,
		OutAmount:  "16688071",
	})
	jupiter.RegisterQuoteBuy(jc, variantTestTSpaceXMint, variantTestUSDCMicros, jupiter.BuyQuote{
		Routable:   true,
		OutputMint: variantTestTSpaceXMint,
		OutAmount:  "17711094",
	})
}

func TestStartBuy_selectBestVariant_measuredFixture_picksTessera(t *testing.T) {
	buy, jc := spacexVariantBuyService(t, big.NewRat(5, 1), nil)
	jupiter.ResetQuoteBuyRequests(jc)

	result, err := buy.StartBuy(context.Background(), StartBuyRequest{
		GroupID:           "g1",
		UserID:            "u1",
		Symbol:            "SPACEX",
		USDCAmount:        variantTestUSDCMicros,
		SelectBestVariant: true,
	})
	if err != nil {
		t.Fatalf("StartBuy: %v", err)
	}
	if result.Symbol != "tSpaceX" {
		t.Fatalf("symbol = %q, want tSpaceX", result.Symbol)
	}
	if result.PriceComparison == nil || result.PriceComparison.Basis != "live_quote" {
		t.Fatalf("basis = %v, want live_quote", result.PriceComparison)
	}
	if result.Provider == nil || result.Provider.IssuerName != "Tessera" {
		t.Fatalf("provider = %+v, want Tessera", result.Provider)
	}
	calls := jupiter.LastQuoteBuyRequests(jc)
	if len(calls) > 3 {
		t.Fatalf("QuoteBuy calls = %d, want at most 3 (2 price-only + 1 final)", len(calls))
	}
	last := calls[len(calls)-1]
	if last.Symbol != "tSpaceX" {
		t.Fatalf("final quote symbol = %q, want tSpaceX", last.Symbol)
	}

	rightExp, ok := catalog.ReferenceExposureUsdcMicros(16688071, 9, big.NewRat(5, 1), 149.32, 100)
	if !ok {
		t.Fatal("reference exposure for SPACEX fixture")
	}
	wrongExp, ok := catalog.ReferenceExposureUsdcMicros(16688071, 9, big.NewRat(1, 5), 149.32, 100)
	if !ok {
		t.Fatal("inverted multiplier exposure")
	}
	if wrongExp >= rightExp {
		t.Fatal("inverting SPACEX multiplier must reduce exposure vs 5/1")
	}
	tesseraExp, ok := catalog.ReferenceExposureUsdcMicros(17711094, 9, big.NewRat(1, 1), 746.61, 20)
	if !ok || tesseraExp <= rightExp {
		t.Fatalf("tSpaceX exposure %d must beat correct SPACEX %d", tesseraExp, rightExp)
	}
}

func TestStartBuy_selectBestVariant_singleVariant_basisSingle(t *testing.T) {
	jc := jupiter.NewFakeClient()
	only := xstocks.CatalogAsset{
		Symbol: "OPENAI", SolanaMint: "PreOpenAIMint1111111111111111111111111",
		Kind: xstocks.AssetKindPreIPO, Source: xstocks.AssetSourcePreStocks,
		Issuer: "prestocks", IssuerName: "PreStocks", UnderlyingID: "openai",
		Decimals: 9, Routable: true,
	}.Normalize()
	src := catalog.NewFakeSource(only)
	comp := catalog.NewCompositeWithSources(xstocks.NewFakeCatalogSearcher(), []catalog.TaggedSource{
		{Source: src, SourceID: xstocks.AssetSourcePreStocks},
	}, xstocks.NewFakeRoutabilityProber(true), nil, nil)
	resolver := catalog.NewResolverWithCatalog(xstocks.NewFakeResolver(), comp)
	buy := NewBuyService(jc, resolver)
	buy.SetMintCatalog(comp)
	buy.SetBuyVariantPicker(comp)
	jupiter.RegisterQuoteBuy(jc, only.SolanaMint, 5_000_000, jupiter.BuyQuote{Routable: true, OutAmount: "1000"})

	result, err := buy.StartBuy(context.Background(), StartBuyRequest{
		Symbol: "OPENAI", USDCAmount: 5_000_000, SelectBestVariant: true,
	})
	if err != nil {
		t.Fatalf("StartBuy: %v", err)
	}
	if result.PriceComparison == nil || result.PriceComparison.Basis != "single" {
		t.Fatalf("basis = %v, want single", result.PriceComparison)
	}
}

func TestStartBuy_selectBestVariant_referenceMissing_quotesRequestedSymbol_basisUnavailable(t *testing.T) {
	buy, _ := spacexVariantBuyService(t, big.NewRat(5, 1), &variantPriceClient{prices: map[string]jupiter.TokenPrice{}})

	result, err := buy.StartBuy(context.Background(), StartBuyRequest{
		Symbol: "SPACEX", USDCAmount: variantTestUSDCMicros, SelectBestVariant: true,
	})
	if err != nil {
		t.Fatalf("StartBuy: %v", err)
	}
	if result.Symbol != "SPACEX" {
		t.Fatalf("symbol = %q, want requested SPACEX", result.Symbol)
	}
	if result.PriceComparison == nil || result.PriceComparison.Basis != "unavailable" {
		t.Fatalf("basis = %v, want unavailable", result.PriceComparison)
	}
}

func TestStartBuy_selectBestVariant_false_quotesRequestedSymbol(t *testing.T) {
	buy, jc := spacexVariantBuyService(t, big.NewRat(5, 1), nil)
	jupiter.ResetQuoteBuyRequests(jc)

	result, err := buy.StartBuy(context.Background(), StartBuyRequest{
		Symbol: "SPACEX", USDCAmount: variantTestUSDCMicros, SelectBestVariant: false,
	})
	if err != nil {
		t.Fatalf("StartBuy: %v", err)
	}
	if result.Symbol != "SPACEX" {
		t.Fatalf("symbol = %q, want SPACEX", result.Symbol)
	}
	if result.PriceComparison != nil {
		t.Fatalf("expected no comparison, got %+v", result.PriceComparison)
	}
	if len(jupiter.LastQuoteBuyRequests(jc)) != 1 {
		t.Fatalf("QuoteBuy calls = %d, want 1", len(jupiter.LastQuoteBuyRequests(jc)))
	}
}

func TestStartBuy_selectBestVariant_onSell_ignored(t *testing.T) {
	// Sell quotes use QuoteProposal → quoteSellForMember; they never set SelectBestVariant on StartBuy.
	buy, jc := spacexVariantBuyService(t, big.NewRat(5, 1), nil)
	jupiter.ResetQuoteBuyRequests(jc)
	_, err := buy.StartBuy(context.Background(), StartBuyRequest{
		Symbol: "SPACEX", USDCAmount: variantTestUSDCMicros, SelectBestVariant: false,
	})
	if err != nil {
		t.Fatalf("StartBuy: %v", err)
	}
	if len(jupiter.LastQuoteBuyRequests(jc)) != 1 {
		t.Fatalf("sell path must not fan out variant quotes; got %d QuoteBuy calls", len(jupiter.LastQuoteBuyRequests(jc)))
	}
}

func TestCreateProposal_selectBestVariant_persistsServerSymbol(t *testing.T) {
	if postgresURLUnset() {
		t.Skip("requires DATABASE_URL")
	}
	h := integrationGovernanceApp(t)
	comp, resolver := wireSpaceXCompositeHarnessOnly(t)
	buy := NewBuyService(h.Jupiter, resolver)
	buy.SetMintCatalog(comp)
	buy.SetBuyVariantPicker(comp)
	h.Governance.SetBuyService(buy)

	userID := openTestSession(t, h.ISO, h.Sessions, h.Privy, "prop", "Proposer")
	token := h.ISO.UniqueToken("prop")
	created, err := h.Governance.CreateGroupWithRules(context.Background(), string(token), testGroupName(h.ISO, "pick"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 20_000_000)
	registerSpaceXVariantQuotes(t, h.Jupiter)

	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID: created.GroupID, ProposerID: userID.UserID,
		Symbol: "SPACEX", UsdcMicros: variantTestUSDCMicros, SelectBestVariant: true,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	if proposal.Symbol != "tSpaceX" {
		t.Fatalf("persisted symbol = %q, want tSpaceX", proposal.Symbol)
	}
}

func TestExecute_doesNotRepick(t *testing.T) {
	if postgresURLUnset() {
		t.Skip("requires DATABASE_URL")
	}
	h := integrationExecuteOnPassApp(t)
	comp, resolver := wireSpaceXCompositeHarnessOnly(t)
	buy := NewBuyService(h.App.Jupiter, resolver)
	buy.SetMintCatalog(comp)
	buy.SetBuyVariantPicker(comp)
	h.Governance.SetBuyService(buy)
	h.App.Swap.buy = buy
	mints := mintinfo.NewFakeReader(map[string]mintinfo.Info{
		variantTestTSpaceXMint:   {Decimals: 9, UiMultiplier: big.NewRat(1, 1)},
		variantTestSpaceXPreMint: {Decimals: 9, UiMultiplier: big.NewRat(5, 1)},
	})
	symbols := NewSymbolResolver(comp)
	symbols.SetMintInfo(mints)
	h.App.Symbols = symbols
	h.App.Swap.symbols = symbols

	ctx := context.Background()
	userID := openTestSession(t, h.App.ISO, NewSessionService(h.App.Store, h.App.Privy), h.App.Privy, "exec", "Exec")
	token := h.App.ISO.UniqueToken("exec")
	created, err := h.Governance.CreateGroupWithRules(ctx, token, testGroupName(h.App.ISO, "exec"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.App.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.App.Privy, created.TreasuryAddress, 20_000_000)
	registerSpaceXVariantQuotes(t, h.App.Jupiter)

	proposal, err := h.Governance.CreateProposal(ctx, CreateProposalInput{
		GroupID: created.GroupID, ProposerID: userID.UserID,
		Symbol: "SPACEX", UsdcMicros: variantTestUSDCMicros, SelectBestVariant: true,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	requestID := testRequestID(h.App.ISO, "tspacex-exec")
	signature := testTxSignature(h.App.ISO, "tspacex-exec")
	jupiter.RegisterQuoteBuy(h.App.Jupiter, variantTestTSpaceXMint, variantTestUSDCMicros, jupiter.BuyQuote{
		Routable: true, OutputMint: variantTestTSpaceXMint, OutAmount: "17711094", RequestID: requestID,
	})
	jupiter.RegisterBuyOrder(h.App.Jupiter, requestID, jupiter.BuyOrder{
		RequestID: requestID, Transaction: "unsigned-buy-tx", OutAmount: "17711094", OutputMint: variantTestTSpaceXMint,
	})
	jupiter.RegisterExecutePoll(h.App.Jupiter, requestID, []jupiter.ExecuteResult{
		{Status: jupiter.ExecuteStatusPending, Code: -1},
		{
			Status:             jupiter.ExecuteStatusSuccess,
			Code:               0,
			Signature:          signature,
			InputAmountResult:  strconv.FormatInt(variantTestUSDCMicros, 10),
			OutputAmountResult: "17711094",
		},
	})

	passed, err := h.Governance.CastVote(ctx, CastVoteInput{
		ProposalID: proposal.ID, VoterID: userID.UserID, Choice: domain.VoteYes,
	})
	if err != nil {
		t.Fatalf("CastVote: %v", err)
	}

	jupiter.ResetQuoteBuyRequests(h.App.Jupiter)
	_, err = h.ExecutePass.ExecuteOnPass(ctx, passed)
	if err != nil {
		t.Fatalf("ExecuteOnPass: %v", err)
	}
	for _, c := range jupiter.LastQuoteBuyRequests(h.App.Jupiter) {
		if c.Symbol != "tSpaceX" {
			t.Fatalf("execute quoted %q, want only tSpaceX", c.Symbol)
		}
	}
}

func TestPotRows_preIpo_carryIssuerName(t *testing.T) {
	comp, _ := wireSpaceXCompositeHarnessOnly(t)
	symbols := NewSymbolResolver(comp)
	symbols.SetMintInfo(mintinfo.NewFakeReader(map[string]mintinfo.Info{
		variantTestTSpaceXMint: {Decimals: 9, UiMultiplier: big.NewRat(1, 1)},
	}))
	input := pythNavWithPreIPOHolding(t)
	rows, err := potRowsFromPythInput(context.Background(), symbols, input)
	if err != nil {
		t.Fatalf("potRowsFromPythInput: %v", err)
	}
	var found bool
	for _, row := range rows {
		if row.Symbol == "tSpaceX" {
			found = true
			if row.Issuer != "tessera" || row.IssuerName != "Tessera" {
				t.Fatalf("issuer = %q/%q, want tessera/Tessera", row.Issuer, row.IssuerName)
			}
		}
	}
	if !found {
		t.Fatal("missing tSpaceX pot row")
	}
}

func wireSpaceXCompositeHarnessOnly(t *testing.T) (*catalog.Composite, *catalog.Resolver) {
	t.Helper()
	prices := &variantPriceClient{prices: map[string]jupiter.TokenPrice{
		variantTestTSpaceXMint:   {StockData: freshVariantStock(746.61)},
		variantTestSpaceXPreMint: {StockData: freshVariantStock(149.32)},
	}}
	xs := xstocks.NewFakeCatalogSearcher()
	tessera := catalog.NewFakeSource(variantTesseraSpaceX())
	prestocks := catalog.NewFakeSource(variantPreStocksSpaceX())
	mints := mintinfo.NewFakeReader(map[string]mintinfo.Info{
		variantTestTSpaceXMint:   {Decimals: 9, UiMultiplier: big.NewRat(1, 1)},
		variantTestSpaceXPreMint: {Decimals: 9, UiMultiplier: big.NewRat(5, 1)},
	})
	prober := xstocks.NewFakeRoutabilityProber(true)
	comp := catalog.NewCompositeWithSources(xs, []catalog.TaggedSource{
		{Source: tessera, SourceID: xstocks.AssetSourceTessera},
		{Source: prestocks, SourceID: xstocks.AssetSourcePreStocks},
	}, prober, mints, prices)
	resolver := catalog.NewResolverWithCatalog(xstocks.NewFakeResolver(), comp)
	return comp, resolver
}

func postgresURLUnset() bool {
	return os.Getenv("DATABASE_URL") == ""
}

func pythNavWithPreIPOHolding(t *testing.T) pyth.NavInput {
	t.Helper()
	return pyth.NavInput{
		Holdings: []pyth.MarkedHolding{{
			Symbol:       "tSpaceX",
			Mint:         variantTestTSpaceXMint,
			Units:        1_000_000_000,
			MarkUsdc:     500_000_000,
			Decimals:     9,
			Kind:         xstocks.AssetKindPreIPO,
			UiMultiplier: big.NewRat(1, 1),
		}},
	}
}
