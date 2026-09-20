package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func seedApple(t *testing.T, handlers *AssetsHandlers) {
	t.Helper()
	xstocks.RegisterCatalogAsset(handlers.Catalog, xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple",
		SolanaMint: jupiter.AAPLxMint,
		Routable:   true,
	})
	jupiter.RegisterPrice(handlers.Price, jupiter.AAPLxMint, jupiter.TokenPrice{
		PriceUsdcMicros: 232_050_000,
	})
}

func getAssetDetail(t *testing.T, handlers *AssetsHandlers, token string) assetDetailResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLx", nil)
	req.SetPathValue("symbol", "AAPLx")
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
	return payload
}

func livePythQuotes() pyth.ReferenceQuotes {
	publishedAt := assetsTestClock.Add(-10 * time.Second)
	return pyth.ReferenceQuotes{
		Symbol: "AAPLx",
		Equity: pyth.ReferenceQuote{
			Source:          pyth.QuoteSourcePythEquity,
			Status:          pyth.QuoteStatusLive,
			PriceUsdcMicros: 231_400_000,
			ConfUsdcMicros:  30_000,
			PublishedAt:     publishedAt,
		},
		Token: pyth.ReferenceQuote{
			Source:          pyth.QuoteSourcePythCrypto,
			Status:          pyth.QuoteStatusLive,
			PriceUsdcMicros: 232_050_000,
			ConfUsdcMicros:  50_000,
			PublishedAt:     publishedAt,
		},
		AsOf: assetsTestClock,
	}
}

func TestGET_assets_marketStatus_isOnTheListAndPopularEnvelopes(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	seedApple(t, handlers)

	req := httptest.NewRequest(http.MethodGet, "/v1/assets?query=AAPL", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.ListAssetsHandler(rec, req)

	var listed listAssetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if listed.Market == nil {
		t.Fatal("expected a market status on the list response")
	}
	if listed.Market.Session != "open" || !listed.Market.IsOpen || listed.Market.AfterHours {
		t.Fatalf("market = %+v, want the regular session", listed.Market)
	}
	if listed.Market.NextSession != "after_hours" {
		t.Fatalf("next session = %q, want after_hours", listed.Market.NextSession)
	}
	// 16:00 ET on 2026-09-22 is 20:00 UTC.
	if listed.Market.NextTransition != "2026-09-22T20:00:00Z" {
		t.Fatalf("next transition = %q, want 2026-09-22T20:00:00Z", listed.Market.NextTransition)
	}

	popularReq := httptest.NewRequest(http.MethodGet, "/v1/assets/popular", nil)
	popularReq.Header.Set("Authorization", "Bearer "+token)
	popularRec := httptest.NewRecorder()
	handlers.PopularAssetsHandler(popularRec, popularReq)

	var popular popularAssetsResponse
	if err := json.Unmarshal(popularRec.Body.Bytes(), &popular); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if popular.Market == nil || popular.Market.Session != "open" {
		t.Fatalf("popular market = %+v", popular.Market)
	}
}

func TestGET_assets_symbol_afterHoursSessionIsReported(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	handlers.Now = func() time.Time { return assetsTestClockClosed }
	seedApple(t, handlers)

	detail := getAssetDetail(t, handlers, token)
	if detail.MarketSession != "after_hours" {
		t.Fatalf("marketSession = %q, want after_hours", detail.MarketSession)
	}
	if !detail.AfterHours {
		t.Fatal("afterHours must be true outside the regular session")
	}
	if detail.Market == nil || detail.Market.NextSession != "closed" {
		t.Fatalf("market = %+v, want the post session closing next", detail.Market)
	}
}

func TestGET_assets_symbol_stockVsToken_carriesBothLegsAndThePremium(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	seedApple(t, handlers)
	pyth.RegisterReferenceQuotes(handlers.Quotes, "AAPLx", livePythQuotes())

	detail := getAssetDetail(t, handlers, token)
	if detail.StockVsToken == nil {
		t.Fatal("expected the stock vs token card")
	}
	card := detail.StockVsToken
	if card.Equity.Source != "pyth_equity" || card.Equity.Status != "live" {
		t.Fatalf("equity leg = %+v", card.Equity)
	}
	if card.Equity.PriceUsdcMicros == nil || *card.Equity.PriceUsdcMicros != 231_400_000 {
		t.Fatalf("equity price = %v", card.Equity.PriceUsdcMicros)
	}
	if card.Token.Source != "pyth_crypto" || card.Token.PriceUsdcMicros == nil {
		t.Fatalf("token leg = %+v", card.Token)
	}
	if card.PremiumBps == nil || *card.PremiumBps != 28 {
		t.Fatalf("premium = %v, want 28 bps", card.PremiumBps)
	}
	if card.Equity.PublishedAt == nil {
		t.Fatal("expected a publish time on a priced leg")
	}
	if _, err := time.Parse(time.RFC3339, *card.Equity.PublishedAt); err != nil {
		t.Fatalf("publishedAt %q is not RFC3339: %v", *card.Equity.PublishedAt, err)
	}
	if card.AsOf != assetsTestClock.Format(time.RFC3339) {
		t.Fatalf("asOf = %q, want the fetch instant in UTC", card.AsOf)
	}
}

func TestGET_assets_symbol_stockVsToken_missingCryptoFeedFallsBackToTheOnChainPrice(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	seedApple(t, handlers)
	quotes := livePythQuotes()
	quotes.Token = pyth.ReferenceQuote{
		Source: pyth.QuoteSourcePythCrypto,
		Status: pyth.QuoteStatusUnavailable,
		Reason: pyth.QuoteReasonNoFeed,
	}
	pyth.RegisterReferenceQuotes(handlers.Quotes, "AAPLx", quotes)

	detail := getAssetDetail(t, handlers, token)
	card := detail.StockVsToken
	if card == nil {
		t.Fatal("expected the card with the equity leg intact")
	}
	if card.Token.Source != "jupiter" {
		t.Fatalf("token source = %q, want the on-chain fallback to say so", card.Token.Source)
	}
	if card.Token.PriceUsdcMicros == nil || *card.Token.PriceUsdcMicros != 232_050_000 {
		t.Fatalf("token price = %v, want the Jupiter mark", card.Token.PriceUsdcMicros)
	}
	if card.Token.ConfUsdcMicros != nil {
		t.Fatal("Jupiter publishes no confidence interval; the response must not imply one")
	}
}

func TestGET_assets_symbol_stockVsToken_unavailableEquityLegIsExplicit(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	seedApple(t, handlers)
	quotes := livePythQuotes()
	quotes.Equity = pyth.ReferenceQuote{
		Source: pyth.QuoteSourcePythEquity,
		Status: pyth.QuoteStatusUnavailable,
		Reason: pyth.QuoteReasonNotEntitled,
	}
	pyth.RegisterReferenceQuotes(handlers.Quotes, "AAPLx", quotes)

	detail := getAssetDetail(t, handlers, token)
	card := detail.StockVsToken
	if card == nil {
		t.Fatal("expected the card with the token leg intact")
	}
	if card.Equity.Status != "unavailable" || card.Equity.Reason != "not_entitled" {
		t.Fatalf("equity leg = %+v", card.Equity)
	}
	if card.Equity.PriceUsdcMicros != nil {
		t.Fatal("an unavailable leg must carry no price")
	}
	if card.PremiumBps != nil {
		t.Fatal("a premium against a missing leg would be invented")
	}
}

func TestGET_assets_symbol_stockVsToken_absentWhenNeitherFeedHasAPrice(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	xstocks.RegisterCatalogAsset(handlers.Catalog, xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple",
		SolanaMint: jupiter.AAPLxMint,
		Routable:   true,
	})
	// No Jupiter price registered either, so there is nothing to fall back to.
	pyth.RegisterReferenceQuotes(handlers.Quotes, "AAPLx", pyth.ReferenceQuotes{
		Symbol: "AAPLx",
		Equity: pyth.ReferenceQuote{Source: pyth.QuoteSourcePythEquity, Status: pyth.QuoteStatusUnavailable, Reason: pyth.QuoteReasonUpstream},
		Token:  pyth.ReferenceQuote{Source: pyth.QuoteSourcePythCrypto, Status: pyth.QuoteStatusUnavailable, Reason: pyth.QuoteReasonNoFeed},
	})

	detail := getAssetDetail(t, handlers, token)
	if detail.StockVsToken != nil {
		t.Fatalf("expected no card at all, got %+v", detail.StockVsToken)
	}
}

func TestGET_assets_symbol_stockVsToken_quoteFetchFailureDropsTheCardNotTheResponse(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	seedApple(t, handlers)
	pyth.RegisterReferenceQuotesError(handlers.Quotes, "AAPLx", errors.New("hermes is down"))

	detail := getAssetDetail(t, handlers, token)
	if detail.StockVsToken != nil {
		t.Fatal("a failed fetch must not produce a card")
	}
	if detail.PriceUsdcMicros == nil {
		t.Fatal("the rest of the detail response must survive")
	}
	if detail.Market == nil {
		t.Fatal("the market status is computed locally and must survive an upstream outage")
	}
}

func TestGET_assets_symbol_stats_areBuiltFromCandlesAndTheYearSeries(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, jupiterClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	seedApple(t, handlers)
	pyth.RegisterReferenceQuotes(handlers.Quotes, "AAPLx", livePythQuotes())
	jupiter.RegisterQuoteBuy(jupiterClient, jupiter.AAPLxMint, 1_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "1000000",
		OutAmount:  "4300000",
	})

	previousClose := int64(226_500_000)
	pyth.RegisterChartSeries(handlers.Pyth, "AAPLx", pyth.ChartRange1D, pyth.AssetChartSeries{
		PreviousCloseUsdcMicros: &previousClose,
		Basis:                   pyth.PriceBasisUnderlying,
		BasisSymbol:             "AAPL",
		Points: []pyth.ChartPoint{
			{Timestamp: 1, PriceUsdcMicros: 229_400_000, OpenUsdcMicros: 229_000_000, HighUsdcMicros: 229_500_000, LowUsdcMicros: 228_200_000},
			{Timestamp: 2, PriceUsdcMicros: 231_400_000, OpenUsdcMicros: 231_000_000, HighUsdcMicros: 231_800_000, LowUsdcMicros: 230_400_000},
		},
	})
	pyth.RegisterChartSeries(handlers.Pyth, "AAPLx", pyth.ChartRange1Y, pyth.AssetChartSeries{
		Points: []pyth.ChartPoint{
			{Timestamp: 1, PriceUsdcMicros: 164_000_000, HighUsdcMicros: 169_000_000, LowUsdcMicros: 163_000_000},
			{Timestamp: 2, PriceUsdcMicros: 260_000_000, HighUsdcMicros: 262_000_000, LowUsdcMicros: 259_000_000},
		},
	})

	detail := getAssetDetail(t, handlers, token)
	stats := detail.Stats
	if stats == nil {
		t.Fatal("expected a stats grid")
	}
	if stats.OpenUsdcMicros == nil || *stats.OpenUsdcMicros != 229_000_000 {
		t.Fatalf("open = %v", stats.OpenUsdcMicros)
	}
	if stats.HighUsdcMicros == nil || *stats.HighUsdcMicros != 231_800_000 {
		t.Fatalf("high = %v", stats.HighUsdcMicros)
	}
	if stats.LowUsdcMicros == nil || *stats.LowUsdcMicros != 228_200_000 {
		t.Fatalf("low = %v", stats.LowUsdcMicros)
	}
	if stats.PreviousCloseUsdcMicros == nil || *stats.PreviousCloseUsdcMicros != 226_500_000 {
		t.Fatalf("previous close = %v", stats.PreviousCloseUsdcMicros)
	}
	if stats.Week52HighUsdcMicros == nil || *stats.Week52HighUsdcMicros != 262_000_000 {
		t.Fatalf("52w high = %v", stats.Week52HighUsdcMicros)
	}
	if stats.Week52LowUsdcMicros == nil || *stats.Week52LowUsdcMicros != 163_000_000 {
		t.Fatalf("52w low = %v", stats.Week52LowUsdcMicros)
	}
	if stats.ConfUsdcMicros == nil || *stats.ConfUsdcMicros != 30_000 {
		t.Fatalf("conf = %v, want Pyth's own confidence interval", stats.ConfUsdcMicros)
	}
	if stats.SpreadBps == nil {
		t.Fatal("expected the trading cost from the Jupiter probe")
	}
	// The grid is NASDAQ dollars; the hero price is the xStock's on-chain price.
	// They disagree by the premium the stock-vs-token card exists to show, so the
	// response has to name the instrument the cells are about — otherwise a hero
	// price above the day's high reads as a bug rather than as two instruments.
	if stats.Basis != pyth.PriceBasisUnderlying || stats.BasisSymbol != "AAPL" {
		t.Fatalf("basis = %q/%q, want underlying/AAPL", stats.Basis, stats.BasisSymbol)
	}
}

func TestGET_assets_symbol_stats_labelTheUnderlyingEvenWhenTheSeriesDidNotSaySo(t *testing.T) {
	t.Parallel()

	// A series from a source that predates the label, or one assembled without it.
	// Every Pyth history source reads the underlying's feed, so the honest answer is
	// still "underlying" — the one thing that must never happen is shipping the
	// cells unlabelled beside a token price.
	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	seedApple(t, handlers)
	pyth.RegisterChartSeries(handlers.Pyth, "AAPLx", pyth.ChartRange1D, pyth.AssetChartSeries{
		Points: []pyth.ChartPoint{{Timestamp: 1, PriceUsdcMicros: 229_400_000}},
	})

	stats := getAssetDetail(t, handlers, token).Stats
	if stats == nil {
		t.Fatal("expected a stats grid")
	}
	if stats.Basis != pyth.PriceBasisUnderlying || stats.BasisSymbol != "AAPL" {
		t.Fatalf("basis = %q/%q, want underlying/AAPL", stats.Basis, stats.BasisSymbol)
	}
}

func TestGET_assets_symbol_stats_aLabelAloneDoesNotMakeAGrid(t *testing.T) {
	t.Parallel()

	// The grid is omitted when nothing could be sourced. Adding a basis label must
	// not turn "nothing to show" into an object of nulls with a caption on it.
	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	seedApple(t, handlers)
	pyth.RegisterChartSeries(handlers.Pyth, "AAPLx", pyth.ChartRange1D, pyth.AssetChartSeries{
		Basis:       pyth.PriceBasisUnderlying,
		BasisSymbol: "AAPL",
		EmptyReason: pyth.EmptyReasonNoHistory,
	})

	if stats := getAssetDetail(t, handlers, token).Stats; stats != nil {
		t.Fatalf("stats = %+v, want the grid omitted", stats)
	}
}

func TestGET_assets_symbol_stats_absentWhenNothingCanBeSourced(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	// No chart series, no Pyth quotes, no routable Jupiter probe: an honest grid
	// with nothing in it is no grid at all.
	xstocks.RegisterCatalogAsset(handlers.Catalog, xstocks.CatalogAsset{
		Symbol:     "NEWx",
		Name:       "Newly listed",
		SolanaMint: "NEWmint1111111111111111111111111111111111111",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/NEWx", nil)
	req.SetPathValue("symbol", "NEWx")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.GetAssetHandler(rec, req)

	var detail assetDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if detail.Stats != nil {
		t.Fatalf("stats = %+v, want the grid omitted", detail.Stats)
	}
}

func TestGET_assets_symbol_chart_servesEveryRangeWithItsPreviousClose(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	seedApple(t, handlers)

	for _, chartRange := range pyth.ChartRanges {
		previousClose := int64(226_500_000)
		pyth.RegisterChartSeries(handlers.Pyth, "AAPLx", chartRange, pyth.AssetChartSeries{
			PreviousCloseUsdcMicros: &previousClose,
			Range:                   chartRange,
			Source:                  pyth.ChartSourceBenchmarks,
			Basis:                   pyth.PriceBasisUnderlying,
			BasisSymbol:             "AAPL",
			Points: []pyth.ChartPoint{
				{Timestamp: 1, PriceUsdcMicros: 229_400_000, OpenUsdcMicros: 229_000_000},
				{Timestamp: 2, PriceUsdcMicros: 231_400_000, OpenUsdcMicros: 231_000_000},
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLx/chart?range="+string(chartRange), nil)
		req.SetPathValue("symbol", "AAPLx")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handlers.GetAssetChartHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200; body = %s", chartRange, rec.Code, rec.Body.String())
		}
		var payload assetChartResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s decode json: %v", chartRange, err)
		}
		if payload.Range != string(chartRange) {
			t.Fatalf("range = %q, want %q echoed back", payload.Range, chartRange)
		}
		if payload.Source != pyth.ChartSourceBenchmarks {
			t.Fatalf("%s source = %q", chartRange, payload.Source)
		}
		if payload.PreviousCloseUsdcMicros == nil || *payload.PreviousCloseUsdcMicros != 226_500_000 {
			t.Fatalf("%s previous close = %v", chartRange, payload.PreviousCloseUsdcMicros)
		}
		if payload.Points[0].OpenUsdcMicros != 229_000_000 {
			t.Fatalf("%s open = %d, want the candle to survive the response", chartRange, payload.Points[0].OpenUsdcMicros)
		}
		if payload.Market == nil {
			t.Fatalf("%s: expected a market status alongside the series", chartRange)
		}
		// The curve is the underlying equity's, drawn under a token hero price.
		if payload.Basis != pyth.PriceBasisUnderlying || payload.BasisSymbol != "AAPL" {
			t.Fatalf("%s basis = %q/%q, want underlying/AAPL", chartRange, payload.Basis, payload.BasisSymbol)
		}
	}
}

func TestGET_assets_symbol_chart_sampledFallbackShipsNoPreviousClose(t *testing.T) {
	t.Parallel()

	// The Hermes sampler's grid starts inside the window, so its first point is
	// drawn in the series itself. Shipping it as previousCloseUsdcMicros put a
	// fabricated number under the same field name as the genuine Benchmarks
	// previous close: the dashed baseline landed exactly on the curve's first
	// point, and the day change read 0% at t0.
	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	seedApple(t, handlers)
	pyth.RegisterChartSeries(handlers.Pyth, "AAPLx", pyth.ChartRange1D, pyth.AssetChartSeries{
		Range:  pyth.ChartRange1D,
		Source: pyth.ChartSourceHermes,
		Points: []pyth.ChartPoint{
			{Timestamp: 1, PriceUsdcMicros: 229_400_000},
			{Timestamp: 2, PriceUsdcMicros: 231_400_000},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLx/chart?range=1D", nil)
	req.SetPathValue("symbol", "AAPLx")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.GetAssetChartHandler(rec, req)

	var payload assetChartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.Source != pyth.ChartSourceHermes {
		t.Fatalf("source = %q, want the sampled fallback", payload.Source)
	}
	if payload.PreviousCloseUsdcMicros != nil {
		t.Fatalf("previous close = %d, want the field omitted: the sampler does not know one", *payload.PreviousCloseUsdcMicros)
	}
	if !strings.Contains(rec.Body.String(), `"points"`) || strings.Contains(rec.Body.String(), "previousCloseUsdcMicros") {
		t.Fatalf("body = %s, want no previousCloseUsdcMicros key at all", rec.Body.String())
	}
}

func TestGET_assets_symbol_chart_emptySeriesShipsAnArrayNotNull(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	seedApple(t, handlers)

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLx/chart?range=ALL", nil)
	req.SetPathValue("symbol", "AAPLx")
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
	if string(raw["emptyReason"]) == "" {
		t.Fatal("expected an emptyReason the chart can show")
	}
}

// The app on main sends no range, reads points/emptyReason, and knows nothing
// about the fields added here. It must keep working against this response.
func TestGET_assets_backwardCompatibleWithTheShippedApp(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	seedApple(t, handlers)
	pyth.RegisterChartSeries(handlers.Pyth, "AAPLx", pyth.ChartRange1D, pyth.AssetChartSeries{
		Points: []pyth.ChartPoint{{Timestamp: 1, PriceUsdcMicros: 229_400_000}},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLx/chart", nil)
	req.SetPathValue("symbol", "AAPLx")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.GetAssetChartHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// The shape the old client decodes: points with timestamp and priceUsdcMicros.
	var legacy struct {
		Points []struct {
			Timestamp       int64 `json:"timestamp"`
			PriceUsdcMicros int64 `json:"priceUsdcMicros"`
		} `json:"points"`
		EmptyReason string `json:"emptyReason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(legacy.Points) != 1 || legacy.Points[0].PriceUsdcMicros != 229_400_000 {
		t.Fatalf("legacy points = %+v", legacy.Points)
	}

	detail := getAssetDetail(t, handlers, token)
	if detail.Symbol != "AAPLx" || detail.PriceUsdcMicros == nil {
		t.Fatalf("detail = %+v, want the original fields untouched", detail)
	}
	if detail.Liquidity.Label == "" {
		t.Fatal("the liquidity snippet must still be there")
	}
}
