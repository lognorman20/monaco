package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

func integrationAssetsApp(t *testing.T) (*AssetsHandlers, *AuthHandlers, wallets.Client, dex.Client, *postgres.TestIsolation) {
	t.Helper()

	authHandlers, walletClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	dexClient := dex.NewFakeClient()
	catalog := b20.NewFakeCatalog()
	pythClient := pyth.NewFakeAssetPriceClient()
	handlers := &AssetsHandlers{
		Store:   store,
		Auth:    authHandlers.Verifier,
		Wallets: walletClient,
		Catalog: catalog,
		Pyth:    pythClient,
		Charts:  pyth.NewFakeAssetPriceClient().(pyth.MarketDataClient),
		Quotes:  pyth.NewFakeEquityQuoteClient(),
		Dex:     dexClient,
		// Pin the clock to a Tuesday inside the regular session so session
		// assertions do not depend on when the suite happens to run.
		Now: func() time.Time { return assetsTestClock },
	}
	return handlers, authHandlers, walletClient, dexClient, iso
}

// assetsTestClock is 2026-09-22 14:00 UTC — 10:00 ET on an ordinary Tuesday.
var assetsTestClock = time.Date(2026, time.September, 22, 14, 0, 0, 0, time.UTC)

// assetsTestClockClosed is the same day at 23:00 UTC — 19:00 ET, after-hours.
var assetsTestClockClosed = time.Date(2026, time.September, 22, 23, 0, 0, 0, time.UTC)

func seedAssetsToken(t *testing.T, iso *postgres.TestIsolation, authHandlers *AuthHandlers, walletClient wallets.Client) string {
	t.Helper()
	_, token := seedAuthenticatedUser(t, iso, authHandlers, walletClient, "assets-user", "Assets User")
	return string(token)
}

func TestGET_assets_missingAuth_returns401(t *testing.T) {
	t.Parallel()
	handlers, _, _, _, _ := integrationAssetsApp(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/assets", nil)
	rec := httptest.NewRecorder()
	handlers.ListAssetsHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGET_assets_listsCatalogWithPrices(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, dexClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{
		Symbol:       "AAPLc",
		Name:         "Apple",
		TokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb",
		Routable:     true,
	})
	pyth.RegisterAssetMark(handlers.Pyth, "AAPLc", pyth.AssetMark{PriceUsdcMicros: 185_000_000})
	dex.RegisterQuoteBuy(dexClient, "0xb200000000000000000000c2e324d24d7eecd1fb", 1_000_000, dex.BuyQuote{
		Routable:    true,
		InputToken:  dex.USDCAddress(),
		OutputToken: "0xb200000000000000000000c2e324d24d7eecd1fb",
		InAmount:    "1000000",
		OutAmount:   "100000000",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets?query=AAPL&limit=5", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.ListAssetsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload listAssetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Assets) != 1 {
		t.Fatalf("assets len = %d, want 1", len(payload.Assets))
	}
	if payload.Assets[0].PriceUsdcMicros == nil || *payload.Assets[0].PriceUsdcMicros != 185_000_000 {
		t.Fatalf("priceUsdcMicros = %v, want 185000000", payload.Assets[0].PriceUsdcMicros)
	}
}

func TestGET_assets_popular_returnsPinnedAssets(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{
		Symbol:       "AAPLc",
		Name:         "Apple",
		TokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb",
		Routable:     true,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/popular?limit=3", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.PopularAssetsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload popularAssetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Assets) != 1 {
		t.Fatalf("assets len = %d, want 1", len(payload.Assets))
	}
	if payload.Assets[0].Symbol != "AAPLc" {
		t.Fatalf("symbol = %q, want AAPLc", payload.Assets[0].Symbol)
	}
}

func TestGET_assets_popular_batchedPrices_preservesOrder(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{
		Symbol:       "AAPLc",
		Name:         "Apple",
		TokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb",
		Routable:     true,
	})
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{
		Symbol:       "MSFTc",
		Name:         "Microsoft",
		TokenAddress: "0xb200000000000000000000ab99cfa739e253872b",
		Routable:     true,
	})
	// MSFTc's mark is registered before AAPLc's so a lucky map-iteration order
	// wouldn't mask a real ordering bug in the response assembly.
	pyth.RegisterAssetMark(handlers.Pyth, "MSFTc", pyth.AssetMark{PriceUsdcMicros: 400_000_000})
	pyth.RegisterAssetMark(handlers.Pyth, "AAPLc", pyth.AssetMark{PriceUsdcMicros: 185_000_000})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/popular?limit=3", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.PopularAssetsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload popularAssetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Assets) != 2 {
		t.Fatalf("assets len = %d, want 2", len(payload.Assets))
	}
	if payload.Assets[0].Symbol != "AAPLc" || payload.Assets[1].Symbol != "MSFTc" {
		t.Fatalf("order = [%s, %s], want [AAPLc, MSFTc]", payload.Assets[0].Symbol, payload.Assets[1].Symbol)
	}
	if payload.Assets[0].PriceUsdcMicros == nil || *payload.Assets[0].PriceUsdcMicros != 185_000_000 {
		t.Fatalf("AAPLc price = %v, want 185000000", payload.Assets[0].PriceUsdcMicros)
	}
	if payload.Assets[1].PriceUsdcMicros == nil || *payload.Assets[1].PriceUsdcMicros != 400_000_000 {
		t.Fatalf("MSFTc price = %v, want 400000000", payload.Assets[1].PriceUsdcMicros)
	}
}

func TestGET_assets_popular_usesPriceAPI_skipsQuoteBuy(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, dexClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{
		Symbol:       "AAPLc",
		Name:         "Apple",
		TokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb",
		Routable:     true,
	})
	pyth.RegisterAssetMark(handlers.Pyth, "AAPLc", pyth.AssetMark{PriceUsdcMicros: 185_000_000})
	dex.RegisterQuoteBuy(dexClient, "0xb200000000000000000000c2e324d24d7eecd1fb", 1_000_000, dex.BuyQuote{
		Routable:    true,
		InputToken:  dex.USDCAddress(),
		OutputToken: "0xb200000000000000000000c2e324d24d7eecd1fb",
		InAmount:    "1000000",
		OutAmount:   "100000000",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/popular?limit=3", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.PopularAssetsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload popularAssetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.Assets[0].PriceUsdcMicros == nil || *payload.Assets[0].PriceUsdcMicros != 185_000_000 {
		t.Fatalf("priceUsdcMicros = %v, want 185000000", payload.Assets[0].PriceUsdcMicros)
	}
	if got := dex.QuoteBuyCallCount(dexClient); got != 0 {
		t.Fatalf("Price.Prices calls = %d, want 1 batched call for the whole popular strip", got)
	}
	if got := dex.QuoteBuyCallCount(dexClient); got != 0 {
		t.Fatalf("QuoteBuy calls = %d, want 0 on popular enrichment", got)
	}
}

func TestGET_assets_popular_missingPrice_omitsPriceField(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, dexClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{
		Symbol:       "AAPLc",
		Name:         "Apple",
		TokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb",
		Routable:     true,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/popular?limit=3", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.PopularAssetsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload popularAssetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Assets) != 1 {
		t.Fatalf("assets len = %d, want 1", len(payload.Assets))
	}
	if payload.Assets[0].PriceUsdcMicros != nil {
		t.Fatalf("priceUsdcMicros = %v, want nil without a registered price", payload.Assets[0].PriceUsdcMicros)
	}
	if got := dex.QuoteBuyCallCount(dexClient); got != 0 {
		t.Fatalf("QuoteBuy calls = %d, want 0 when price is missing", got)
	}
}

func TestGET_assets_symbol_returnsDetailAndLiquidity(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, dexClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{
		Symbol:       "AAPLc",
		Name:         "Apple",
		TokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb",
		Routable:     true,
	})
	pyth.RegisterAssetMark(handlers.Pyth, "AAPLc", pyth.AssetMark{PriceUsdcMicros: 185_000_000})
	dex.RegisterQuoteBuy(dexClient, "0xb200000000000000000000c2e324d24d7eecd1fb", 1_000_000, dex.BuyQuote{
		Routable:    true,
		InputToken:  dex.USDCAddress(),
		OutputToken: "0xb200000000000000000000c2e324d24d7eecd1fb",
		InAmount:    "1000000",
		OutAmount:   "100000000",
	})
	dex.RegisterSellQuote(dexClient, "0xb200000000000000000000c2e324d24d7eecd1fb", b20.TokenAtomicScale, dex.SellQuote{
		Routable:   true,
		InputToken: "0xb200000000000000000000c2e324d24d7eecd1fb",
		InAmount:   "100000000",
		OutAmount:  "1800000",
	})

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
	if payload.Liquidity.Label != "Via DEX" {
		t.Fatalf("liquidity label = %q", payload.Liquidity.Label)
	}
	if payload.Liquidity.BuyProbeOutAmount == "" {
		t.Fatal("expected buy probe output")
	}
	if !payload.Liquidity.Routable {
		t.Fatal("expected routable liquidity snippet")
	}
	if !payload.Routable {
		t.Fatal("top-level routable must match the live Kyber probe")
	}
}

func TestGET_assets_symbol_catalogNotRoutable_liveQuoteSetsRoutable(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, dexClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{
		Symbol:       "AAPLc",
		Name:         "Apple",
		TokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb",
		Routable:     false,
	})
	pyth.RegisterAssetMark(handlers.Pyth, "AAPLc", pyth.AssetMark{PriceUsdcMicros: 185_000_000})
	dex.RegisterQuoteBuy(dexClient, "0xb200000000000000000000c2e324d24d7eecd1fb", 1_000_000, dex.BuyQuote{
		Routable:    true,
		InputToken:  dex.USDCAddress(),
		OutputToken: "0xb200000000000000000000c2e324d24d7eecd1fb",
		InAmount:    "1000000",
		OutAmount:   "100000000",
	})

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
	if !payload.Liquidity.Routable || !payload.Routable {
		t.Fatalf("live quote must not keep a stale catalog not-routable flag: routable=%v liquidity=%v", payload.Routable, payload.Liquidity.Routable)
	}
}

func TestGET_assets_symbol_quoteFail_marksNotRoutable(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{
		Symbol:       "AAPLc",
		Name:         "Apple",
		TokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb",
		Routable:     true,
	})
	pyth.RegisterAssetMark(handlers.Pyth, "AAPLc", pyth.AssetMark{PriceUsdcMicros: 185_000_000})

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
	if payload.Liquidity.Routable {
		t.Fatal("quote fail must not invent a routable book")
	}
	if !payload.Routable {
		t.Fatal("listed token stays buyable when the DEX probe fails")
	}
}

func TestGET_assets_symbol_chart_returnsSeries(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{
		Symbol:       "AAPLc",
		Name:         "Apple",
		TokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb",
	})
	pyth.RegisterChartSeries(handlers.Pyth, "AAPLc", pyth.ChartRange1D, pyth.AssetChartSeries{
		Points: []pyth.ChartPoint{
			{Timestamp: 1_700_000_000, PriceUsdcMicros: 180_000_000},
			{Timestamp: 1_700_003_600, PriceUsdcMicros: 185_000_000},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLc/chart?range=1D", nil)
	req.SetPathValue("symbol", "AAPLc")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.GetAssetChartHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload assetChartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Points) != 2 {
		t.Fatalf("points len = %d, want 2", len(payload.Points))
	}
}

func TestGET_assets_symbol_chart_emptySeries_returnsReason(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{
		Symbol:       "AAPLc",
		Name:         "Apple",
		TokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLc/chart?range=1W", nil)
	req.SetPathValue("symbol", "AAPLc")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.GetAssetChartHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload assetChartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(payload.Points) != 0 {
		t.Fatalf("points len = %d, want 0", len(payload.Points))
	}
	if payload.EmptyReason == "" {
		t.Fatal("expected emptyReason when history is missing")
	}
}

func TestGET_assets_symbol_chart_invalidRange_returns400(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	b20.RegisterCatalogAsset(handlers.Catalog, b20.Asset{
		Symbol: "AAPLc",
		Name:   "Apple",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLc/chart?range=5Y", nil)
	req.SetPathValue("symbol", "AAPLc")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.GetAssetChartHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
