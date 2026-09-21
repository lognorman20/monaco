package httpapi

import (
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

const appleToken = "0xb200000000000000000000c2e324d24d7eecd1fb"

// seedApple lists AAPLc with a Chainlink mark of $232.05.
func seedApple(t *testing.T, handlers *AssetsHandlers) {
	t.Helper()
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{
		Symbol:       "AAPLc",
		Name:         "Apple",
		TokenAddress: appleToken,
		Routable:     true,
	})
	pyth.RegisterAssetMark(handlers.Pyth, "AAPLc", pyth.AssetMark{PriceUsdcMicros: 232_050_000})
}

// seedKyberProbes makes 1 USDC buy 0.00430000 AAPLc (ask $232.558139) and one
// AAPLc sell for $231.10.
func seedKyberProbes(t *testing.T, client dex.Client) {
	t.Helper()
	dex.RegisterQuote(client, dex.Quote{
		TokenIn: dex.USDCAddress(), TokenOut: appleToken,
		AmountIn: big.NewInt(1_000_000), AmountOut: big.NewInt(430_000), Routable: true,
	})
	dex.RegisterQuote(client, dex.Quote{
		TokenIn: appleToken, TokenOut: dex.USDCAddress(),
		AmountIn: big.NewInt(b20.TokenAtomicScale), AmountOut: big.NewInt(231_100_000), Routable: true,
	})
}

func getAssetDetail(t *testing.T, handlers *AssetsHandlers, token string) (assetDetailResponse, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLc", nil)
	req.SetPathValue("symbol", "AAPLc")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.GetAssetHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload assetDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	return payload, rec.Body.String()
}

func liveEquityQuote() pyth.ReferenceQuote {
	return pyth.ReferenceQuote{
		Source:          pyth.QuoteSourcePythEquity,
		Status:          pyth.QuoteStatusLive,
		PriceUsdcMicros: 231_400_000,
		ConfUsdcMicros:  30_000,
		PublishedAt:     assetsTestClock.Add(-10 * time.Second),
	}
}

// underlyingDay is a Pyth 1D series for AAPL: prev close $228, now $231.42.
func underlyingDay() pyth.AssetChartSeries {
	regularOpen := time.Date(2026, time.September, 22, 13, 30, 0, 0, time.UTC)
	return pyth.AssetChartSeries{
		Source:                  pyth.ChartSourceBenchmarks,
		Range:                   pyth.ChartRange1D,
		Basis:                   pyth.PriceBasisUnderlying,
		BasisSymbol:             "AAPL",
		PreviousCloseUsdcMicros: int64Ptr(228_000_000),
		RegularOpen:             regularOpen,
		RegularClose:            regularOpen.Add(390 * time.Minute),
		Points: []pyth.ChartPoint{
			// 08:00 ET pre-market spike: in the chart, not in the regular-session stats.
			{Timestamp: regularOpen.Add(-90 * time.Minute).Unix(), PriceUsdcMicros: 240_000_000},
			{Timestamp: regularOpen.Unix(), PriceUsdcMicros: 229_500_000, OpenUsdcMicros: 229_000_000, HighUsdcMicros: 230_100_000, LowUsdcMicros: 228_600_000},
			{Timestamp: regularOpen.Add(5 * time.Minute).Unix(), PriceUsdcMicros: 231_420_000, OpenUsdcMicros: 229_500_000, HighUsdcMicros: 232_000_000, LowUsdcMicros: 229_400_000},
		},
	}
}

func int64Ptr(v int64) *int64 { return &v }

func TestGET_assets_marketStatus_isOnTheListAndPopularEnvelopes(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	seedApple(t, handlers)

	req := httptest.NewRequest(http.MethodGet, "/v1/assets?query=AAPL", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.ListAssetsHandler(rec, req)
	var listed listAssetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if listed.Market == nil || listed.Market.Session != "open" || !listed.Market.IsOpen || listed.Market.AfterHours {
		t.Fatalf("market = %+v, want the regular session", listed.Market)
	}
	if listed.Market.NextSession != "after_hours" || listed.Market.NextTransition != "2026-09-22T20:00:00Z" {
		t.Fatalf("next = %q at %q, want after_hours at 2026-09-22T20:00:00Z", listed.Market.NextSession, listed.Market.NextTransition)
	}
	if listed.Market.AsOf != "2026-09-22T14:00:00Z" {
		t.Fatalf("asOf = %q, want UTC", listed.Market.AsOf)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/assets/popular", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handlers.PopularAssetsHandler(rec, req)
	var popular popularAssetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &popular); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if popular.Market == nil || popular.Market.Session != "open" {
		t.Fatalf("popular market = %+v", popular.Market)
	}
}

func TestGET_assets_list_change24hIsTheUnderlyingsDayMove(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	seedApple(t, handlers)
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{Symbol: "AMZNc", Name: "Amazon", TokenAddress: "0xb2000000000000000000000000000000000000a1", Routable: true})
	pyth.RegisterChartSeries(handlers.Charts.(pyth.AssetPriceClient), "AAPLc", pyth.ChartRange1D, underlyingDay())
	// AMZN's day series came from a token-basis source: not a day move of the stock.
	pyth.RegisterChartSeries(handlers.Charts.(pyth.AssetPriceClient), "AMZNc", pyth.ChartRange1D, pyth.AssetChartSeries{
		Basis:                   pyth.PriceBasisToken,
		PreviousCloseUsdcMicros: int64Ptr(100_000_000),
		Points:                  []pyth.ChartPoint{{Timestamp: 1, PriceUsdcMicros: 110_000_000}},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets?query=", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.ListAssetsHandler(rec, req)
	var listed listAssetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	bySymbol := map[string]marketAssetResponse{}
	for _, asset := range listed.Assets {
		bySymbol[asset.Symbol] = asset
	}
	apple := bySymbol["AAPLc"]
	if apple.Change24h == nil || *apple.Change24h != "0.015000" {
		t.Fatalf("AAPLc change24h = %v, want 0.015000 (231.42 over a 228 previous close)", apple.Change24h)
	}
	// The hero price stays the Chainlink mark, not the Pyth equity price.
	if apple.PriceUsdcMicros == nil || *apple.PriceUsdcMicros != 232_050_000 {
		t.Fatalf("AAPLc price = %v, want the Chainlink mark", apple.PriceUsdcMicros)
	}
	if amazon, ok := bySymbol["AMZNc"]; !ok || amazon.Change24h != nil {
		t.Fatalf("AMZNc = %+v, want no change24h from a token-basis series", amazon)
	}
}

func TestGET_assets_symbol_afterHoursSessionIsReported(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	handlers.Now = func() time.Time { return assetsTestClockClosed }
	seedApple(t, handlers)

	detail, _ := getAssetDetail(t, handlers, token)
	if detail.MarketSession != "after_hours" || !detail.AfterHours {
		t.Fatalf("marketSession = %q afterHours = %v, want after_hours", detail.MarketSession, detail.AfterHours)
	}
	if detail.Market == nil || detail.Market.NextSession != "closed" || detail.Market.NextTransition != "2026-09-23T00:00:00Z" {
		t.Fatalf("market = %+v, want the post session closing next", detail.Market)
	}
}

func TestGET_assets_symbol_stockVsToken_isKyberAgainstTheChainlinkMark(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, dexClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	seedApple(t, handlers)
	seedKyberProbes(t, dexClient)
	pyth.RegisterEquityQuote(handlers.Quotes, "AAPLc", liveEquityQuote())

	detail, body := getAssetDetail(t, handlers, token)
	card := detail.StockVsToken
	if card == nil {
		t.Fatal("expected a stock-vs-token card")
	}
	if card.Token.Source != "dex_kyber" || card.Token.Status != "live" {
		t.Fatalf("token = %+v, want a live dex_kyber leg", card.Token)
	}
	if card.Token.PriceUsdcMicros == nil || *card.Token.PriceUsdcMicros != 231_829_069 {
		t.Fatalf("token mid = %v, want 231829069", card.Token.PriceUsdcMicros)
	}
	if card.Token.AskUsdcMicros == nil || *card.Token.AskUsdcMicros != 232_558_139 || card.Token.BidUsdcMicros == nil || *card.Token.BidUsdcMicros != 231_100_000 {
		t.Fatalf("ask/bid = %v/%v", card.Token.AskUsdcMicros, card.Token.BidUsdcMicros)
	}
	if card.Token.PublishedAt != nil || card.Token.ConfUsdcMicros != nil {
		t.Fatal("a Kyber quote has no publish time and no confidence interval")
	}
	if card.Mark.Source != "chainlink_trv" || card.Mark.PriceUsdcMicros == nil || *card.Mark.PriceUsdcMicros != 232_050_000 {
		t.Fatalf("mark = %+v, want the Chainlink total-return mark", card.Mark)
	}
	// (231.829069 - 232.05) / 232.05 = -9.52 bps: the token against its own mark,
	// never against the equity (231.40), which would read the multiplier as premium.
	if card.PremiumBps == nil || *card.PremiumBps != -10 {
		t.Fatalf("premium = %v, want -10 bps against the mark", card.PremiumBps)
	}
	if card.Equity.Source != "pyth_equity" || card.Equity.PriceUsdcMicros == nil || *card.Equity.PriceUsdcMicros != 231_400_000 {
		t.Fatalf("equity = %+v, want the Pyth reference line", card.Equity)
	}
	if card.EquitySymbol != "AAPL" {
		t.Fatalf("equitySymbol = %q", card.EquitySymbol)
	}
	if card.Equity.PublishedAt == nil || *card.Equity.PublishedAt != "2026-09-22T13:59:50Z" {
		t.Fatalf("equity publishedAt = %v, want RFC3339 UTC", card.Equity.PublishedAt)
	}
	if card.SpreadBps == nil || *card.SpreadBps != 63 || detail.Liquidity.SpreadBps == nil || *detail.Liquidity.SpreadBps != 63 {
		t.Fatalf("spread = %v / liquidity %v, want 63 bps on both", card.SpreadBps, detail.Liquidity.SpreadBps)
	}
	if card.AsOf != "2026-09-22T14:00:00Z" {
		t.Fatalf("asOf = %q", card.AsOf)
	}
	for _, banned := range []string{"pyth_crypto", "jupiter", "Crypto."} {
		if strings.Contains(body, banned) {
			t.Fatalf("body mentions %q: %s", banned, body)
		}
	}
}

func TestGET_assets_symbol_stockVsToken_noSellRouteIsExplicitAndHasNoPremium(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, dexClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	seedApple(t, handlers)
	dex.RegisterQuote(dexClient, dex.Quote{
		TokenIn: dex.USDCAddress(), TokenOut: appleToken,
		AmountIn: big.NewInt(1_000_000), AmountOut: big.NewInt(430_000), Routable: true,
	})
	pyth.RegisterEquityQuote(handlers.Quotes, "AAPLc", liveEquityQuote())

	detail, _ := getAssetDetail(t, handlers, token)
	card := detail.StockVsToken
	if card == nil {
		t.Fatal("the equity line alone still makes a card")
	}
	if card.Token.Status != "unavailable" || card.Token.Reason != "no_route" || card.Token.PriceUsdcMicros != nil {
		t.Fatalf("token = %+v, want unavailable/no_route with no price", card.Token)
	}
	if card.PremiumBps != nil || card.SpreadBps != nil || detail.Liquidity.SpreadBps != nil {
		t.Fatal("half a market has no premium and no spread")
	}
}

func TestGET_assets_symbol_stockVsToken_probeErrorIsUpstreamAndNotCached(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, dexClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	seedApple(t, handlers)
	dex.RegisterQuote(dexClient, dex.Quote{
		TokenIn: dex.USDCAddress(), TokenOut: appleToken,
		AmountIn: big.NewInt(1_000_000), AmountOut: big.NewInt(430_000), Routable: true,
	})
	dex.RegisterQuoteError(dexClient, appleToken, dex.USDCAddress(), big.NewInt(b20.TokenAtomicScale), errors.New("kyber: context deadline exceeded"))
	pyth.RegisterEquityQuote(handlers.Quotes, "AAPLc", liveEquityQuote())

	detail, _ := getAssetDetail(t, handlers, token)
	if detail.StockVsToken == nil || detail.StockVsToken.Token.Reason != "upstream_error" {
		t.Fatalf("card = %+v, want the token leg unavailable because of the outage", detail.StockVsToken)
	}

	// Kyber recovers; the next load must probe again rather than serve the outage.
	dex.RegisterQuote(dexClient, dex.Quote{
		TokenIn: appleToken, TokenOut: dex.USDCAddress(),
		AmountIn: big.NewInt(b20.TokenAtomicScale), AmountOut: big.NewInt(231_100_000), Routable: true,
	})
	detail, _ = getAssetDetail(t, handlers, token)
	if detail.StockVsToken == nil || detail.StockVsToken.Token.Status != "live" {
		t.Fatalf("token = %+v, want a fresh live probe after the outage", detail.StockVsToken)
	}
}

func TestGET_assets_symbol_stockVsToken_unavailableEquityLineIsExplicit(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, dexClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	seedApple(t, handlers)
	seedKyberProbes(t, dexClient)
	pyth.RegisterEquityQuote(handlers.Quotes, "AAPLc", pyth.ReferenceQuote{
		Source: pyth.QuoteSourcePythEquity, Status: pyth.QuoteStatusUnavailable, Reason: pyth.QuoteReasonNotEntitled,
	})

	detail, _ := getAssetDetail(t, handlers, token)
	card := detail.StockVsToken
	if card == nil || card.Equity.Status != "unavailable" || card.Equity.Reason != "not_entitled" || card.Equity.PriceUsdcMicros != nil {
		t.Fatalf("card = %+v, want the equity line unavailable with its reason", card)
	}
	// The premium does not depend on the equity line at all.
	if card.PremiumBps == nil {
		t.Fatal("the premium is token against mark and survives a missing equity line")
	}
}

func TestGET_assets_symbol_stockVsToken_absentWhenNothingHasAPrice(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	seedApple(t, handlers)
	handlers.Quotes = nil

	detail, _ := getAssetDetail(t, handlers, token)
	if detail.StockVsToken != nil {
		t.Fatalf("expected no card at all, got %+v", detail.StockVsToken)
	}
	if detail.PriceUsdcMicros == nil {
		t.Fatal("the response itself still carries the mark")
	}
}

func TestGET_assets_symbol_stats_areTheUnderlyingsRegularSession(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, dexClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	seedApple(t, handlers)
	seedKyberProbes(t, dexClient)
	pyth.RegisterEquityQuote(handlers.Quotes, "AAPLc", liveEquityQuote())
	charts := handlers.Charts.(pyth.AssetPriceClient)
	pyth.RegisterChartSeries(charts, "AAPLc", pyth.ChartRange1D, underlyingDay())
	pyth.RegisterChartSeries(charts, "AAPLc", pyth.ChartRange1Y, pyth.AssetChartSeries{
		Basis: pyth.PriceBasisUnderlying, BasisSymbol: "AAPL",
		Points: []pyth.ChartPoint{
			{Timestamp: 1, PriceUsdcMicros: 170_000_000, HighUsdcMicros: 172_000_000, LowUsdcMicros: 164_080_000},
			{Timestamp: 2, PriceUsdcMicros: 250_000_000, HighUsdcMicros: 260_100_000, LowUsdcMicros: 248_000_000},
		},
	})

	detail, _ := getAssetDetail(t, handlers, token)
	stats := detail.Stats
	if stats == nil {
		t.Fatal("expected a stats grid")
	}
	check := func(name string, got *int64, want int64) {
		t.Helper()
		if got == nil || *got != want {
			t.Fatalf("%s = %v, want %d", name, got, want)
		}
	}
	check("open", stats.OpenUsdcMicros, 229_000_000)
	check("high", stats.HighUsdcMicros, 232_000_000)
	check("low", stats.LowUsdcMicros, 228_600_000)
	check("previous close", stats.PreviousCloseUsdcMicros, 228_000_000)
	check("52w high", stats.Week52HighUsdcMicros, 260_100_000)
	check("52w low", stats.Week52LowUsdcMicros, 164_080_000)
	check("conf", stats.ConfUsdcMicros, 30_000)
	if stats.SpreadBps == nil || *stats.SpreadBps != 63 {
		t.Fatalf("spread = %v, want the Kyber spread", stats.SpreadBps)
	}
	if stats.Basis != "underlying" || stats.BasisSymbol != "AAPL" {
		t.Fatalf("basis = %q/%q, want underlying/AAPL", stats.Basis, stats.BasisSymbol)
	}
	if detail.Change24h == nil || *detail.Change24h != "0.015000" {
		t.Fatalf("change24h = %v, want 0.015000", detail.Change24h)
	}
}

func TestGET_assets_symbol_stats_neverFoldTokenRounds(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	seedApple(t, handlers)
	pyth.RegisterChartSeries(handlers.Charts.(pyth.AssetPriceClient), "AAPLc", pyth.ChartRange1D, pyth.AssetChartSeries{
		Source: pyth.ChartSourceChainlink, Basis: pyth.PriceBasisToken, BasisSymbol: "AAPLc",
		Points: []pyth.ChartPoint{{Timestamp: 1, PriceUsdcMicros: 232_000_000}, {Timestamp: 2, PriceUsdcMicros: 233_000_000}},
	})

	detail, _ := getAssetDetail(t, handlers, token)
	if detail.Stats != nil {
		t.Fatalf("stats = %+v, want the grid omitted: token rounds are not the equity's session", detail.Stats)
	}
	if detail.Change24h != nil {
		t.Fatalf("change24h = %q, want none", *detail.Change24h)
	}
}

func TestGET_assets_symbol_chart_servesEveryRangeWithItsSourceAndBasis(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	seedApple(t, handlers)
	for _, chartRange := range pyth.ChartRanges {
		pyth.RegisterChartSeries(handlers.Pyth, "AAPLc", chartRange, pyth.AssetChartSeries{
			Source: pyth.ChartSourceBenchmarks, Range: chartRange,
			Basis: pyth.PriceBasisUnderlying, BasisSymbol: "AAPL",
			PreviousCloseUsdcMicros: int64Ptr(228_000_000),
			Points: []pyth.ChartPoint{
				{Timestamp: 1, PriceUsdcMicros: 229_000_000, OpenUsdcMicros: 228_500_000},
				{Timestamp: 2, PriceUsdcMicros: 230_000_000},
			},
		})
	}
	for _, chartRange := range pyth.ChartRanges {
		// The path uses the canonical spelling here; a lower-cased path must reach
		// the same series through the catalog's own symbol.
		req := httptest.NewRequest(http.MethodGet, "/v1/assets/aaplc/chart?range="+string(chartRange), nil)
		req.SetPathValue("symbol", "aaplc")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handlers.GetAssetChartHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d; body = %s", chartRange, rec.Code, rec.Body.String())
		}
		var payload assetChartResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s decode json: %v", chartRange, err)
		}
		if payload.Range != string(chartRange) || payload.Source != "benchmarks" {
			t.Fatalf("%s range/source = %q/%q", chartRange, payload.Range, payload.Source)
		}
		if payload.PreviousCloseUsdcMicros == nil || *payload.PreviousCloseUsdcMicros != 228_000_000 {
			t.Fatalf("%s previous close = %v", chartRange, payload.PreviousCloseUsdcMicros)
		}
		if len(payload.Points) != 2 || payload.Points[0].OpenUsdcMicros != 228_500_000 {
			t.Fatalf("%s points = %+v, want the candle to survive the response", chartRange, payload.Points)
		}
		if payload.Basis != "underlying" || payload.BasisSymbol != "AAPL" || payload.Market == nil {
			t.Fatalf("%s basis = %q/%q market = %v", chartRange, payload.Basis, payload.BasisSymbol, payload.Market)
		}
	}
}

func TestGET_assets_symbol_chart_chainlinkFallbackSaysItIsTheToken(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	seedApple(t, handlers)
	pyth.RegisterChartSeries(handlers.Pyth, "AAPLc", pyth.ChartRange1W, pyth.AssetChartSeries{
		Source: pyth.ChartSourceChainlink, Range: pyth.ChartRange1W,
		Basis: pyth.PriceBasisToken, BasisSymbol: "AAPLc",
		Points: []pyth.ChartPoint{{Timestamp: 1, PriceUsdcMicros: 231_000_000}, {Timestamp: 2, PriceUsdcMicros: 232_000_000}},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLc/chart?range=1W", nil)
	req.SetPathValue("symbol", "AAPLc")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.GetAssetChartHandler(rec, req)
	var payload assetChartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.Source != "chainlink" || payload.Basis != "token" || payload.BasisSymbol != "AAPLc" {
		t.Fatalf("source/basis/symbol = %q/%q/%q", payload.Source, payload.Basis, payload.BasisSymbol)
	}
	if strings.Contains(rec.Body.String(), "previousCloseUsdcMicros") {
		t.Fatalf("body = %s, want no previous close from rounds", rec.Body.String())
	}
}

func TestGET_assets_symbol_chart_emptySeriesShipsAnArrayNotNull(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	seedApple(t, handlers)
	pyth.RegisterChartSeries(handlers.Pyth, "AAPLc", pyth.ChartRangeAll, pyth.AssetChartSeries{})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLc/chart?range=ALL", nil)
	req.SetPathValue("symbol", "AAPLc")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.GetAssetChartHandler(rec, req)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if string(raw["points"]) != "[]" {
		t.Fatalf("points = %s, want an empty array so the app never decodes a null", raw["points"])
	}
	if !strings.Contains(string(raw["emptyReason"]), pyth.EmptyReasonNoHistory) {
		t.Fatalf("emptyReason = %s", raw["emptyReason"])
	}
	if string(raw["range"]) != `"ALL"` {
		t.Fatalf("range = %s", raw["range"])
	}
}

func TestGET_assets_backwardCompatibleWithTheShippedApp(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, dexClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	seedApple(t, handlers)
	seedKyberProbes(t, dexClient)

	// The shipped app decodes symbol/name/tokenAddress/routable/priceUsdcMicros and
	// liquidity; everything this change adds is optional beside them.
	_, body := getAssetDetail(t, handlers, token)
	var legacy struct {
		Symbol          string `json:"symbol"`
		Name            string `json:"name"`
		TokenAddress    string `json:"tokenAddress"`
		Routable        bool   `json:"routable"`
		PriceUsdcMicros *int64 `json:"priceUsdcMicros"`
		Liquidity       struct {
			Label    string `json:"label"`
			Routable bool   `json:"routable"`
		} `json:"liquidity"`
	}
	if err := json.Unmarshal([]byte(body), &legacy); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if legacy.Symbol != "AAPLc" || legacy.TokenAddress != appleToken || !legacy.Routable ||
		legacy.PriceUsdcMicros == nil || *legacy.PriceUsdcMicros != 232_050_000 ||
		legacy.Liquidity.Label != "Via DEX" || !legacy.Liquidity.Routable {
		t.Fatalf("detail = %+v, want the original fields untouched", legacy)
	}
}
