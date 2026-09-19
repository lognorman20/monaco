package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func integrationAssetsApp(t *testing.T) (*AssetsHandlers, *AuthHandlers, privy.Client, jupiter.Client, *postgres.TestIsolation) {
	t.Helper()

	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	jupiterClient := jupiter.NewFakeClient()
	catalog := xstocks.NewFakeCatalogSearcher()
	pythClient := pyth.NewFakeAssetPriceClient()
	handlers := &AssetsHandlers{
		Store:   store,
		Privy:   privyClient,
		Catalog: catalog,
		Pyth:    pythClient,
		Jupiter: jupiterClient,
	}
	return handlers, authHandlers, privyClient, jupiterClient, iso
}

func seedAssetsToken(t *testing.T, iso *postgres.TestIsolation, authHandlers *AuthHandlers, privyClient privy.Client) string {
	t.Helper()
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "assets-user", "Assets User")
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

	handlers, authHandlers, privyClient, jupiterClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	xstocks.RegisterCatalogAsset(handlers.Catalog, xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple",
		SolanaMint: jupiter.AAPLxMint,
		Routable:   true,
	})
	pyth.RegisterAssetMark(handlers.Pyth, "AAPLx", pyth.AssetMark{
		PriceUsdcMicros: 185_000_000,
	})
	jupiter.RegisterQuoteBuy(jupiterClient, jupiter.AAPLxMint, 1_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "1000000",
		OutAmount:  "100000000",
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

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	xstocks.RegisterCatalogAsset(handlers.Catalog, xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple xStock",
		SolanaMint: jupiter.AAPLxMint,
		Routable:   true,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/popular?limit=5", nil)
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
	if payload.Assets[0].Symbol != "AAPLx" {
		t.Fatalf("symbol = %q, want AAPLx", payload.Assets[0].Symbol)
	}
	if payload.Assets[0].Name != "Apple" {
		t.Fatalf("name = %q, want cleaned Apple", payload.Assets[0].Name)
	}
	if payload.Assets[0].LogoURL == nil || *payload.Assets[0].LogoURL == "" {
		t.Fatal("expected logoUrl for AAPLx")
	}
}

func TestGET_assets_canceledContext_isNot500(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	xstocks.RegisterCatalogSearchError(handlers.Catalog, context.Canceled)

	req := httptest.NewRequest(http.MethodGet, "/v1/assets?query=Goog", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.ListAssetsHandler(rec, req)

	if rec.Code == http.StatusInternalServerError {
		t.Fatalf("status = 500, canceled catalog search must not surface as internal error")
	}
}


func TestGET_assets_symbol_returnsDetailAndLiquidity(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, jupiterClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	xstocks.RegisterCatalogAsset(handlers.Catalog, xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple",
		SolanaMint: jupiter.AAPLxMint,
		Routable:   true,
	})
	pyth.RegisterAssetMark(handlers.Pyth, "AAPLx", pyth.AssetMark{PriceUsdcMicros: 185_000_000})
	jupiter.RegisterQuoteBuy(jupiterClient, jupiter.AAPLxMint, 1_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "1000000",
		OutAmount:  "100000000",
	})
	jupiter.RegisterSellQuote(jupiterClient, jupiter.AAPLxMint, jupiter.XStockAtomicScale, jupiter.SellQuote{
		Routable:   true,
		InputMint:  jupiter.AAPLxMint,
		OutputMint: jupiter.USDCMint,
		InAmount:   "100000000",
		OutAmount:  "1800000",
	})

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
	if payload.Liquidity.Label != "Via Jupiter" {
		t.Fatalf("liquidity label = %q", payload.Liquidity.Label)
	}
	if payload.Liquidity.BuyProbeOutAmount == "" {
		t.Fatal("expected buy probe output")
	}
	if !payload.Liquidity.Routable {
		t.Fatal("expected routable liquidity snippet")
	}
	if !payload.Routable {
		t.Fatal("top-level routable must match the live Jupiter probe")
	}
}

func TestGET_assets_symbol_catalogNotRoutable_liveQuoteSetsRoutable(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, jupiterClient, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	xstocks.RegisterCatalogAsset(handlers.Catalog, xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple",
		SolanaMint: jupiter.AAPLxMint,
		Routable:   false,
	})
	pyth.RegisterAssetMark(handlers.Pyth, "AAPLx", pyth.AssetMark{PriceUsdcMicros: 185_000_000})
	jupiter.RegisterQuoteBuy(jupiterClient, jupiter.AAPLxMint, 1_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "1000000",
		OutAmount:  "100000000",
	})

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
	if !payload.Liquidity.Routable || !payload.Routable {
		t.Fatalf("live quote must not keep a stale catalog not-routable flag: routable=%v liquidity=%v", payload.Routable, payload.Liquidity.Routable)
	}
}

func TestGET_assets_symbol_quoteFail_marksNotRoutable(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	xstocks.RegisterCatalogAsset(handlers.Catalog, xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple",
		SolanaMint: jupiter.AAPLxMint,
		Routable:   true,
	})
	pyth.RegisterAssetMark(handlers.Pyth, "AAPLx", pyth.AssetMark{PriceUsdcMicros: 185_000_000})

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
	if payload.Liquidity.Routable {
		t.Fatal("quote fail must not invent a routable book")
	}
	if payload.Routable {
		t.Fatal("top-level routable must stay false when the Jupiter probe fails")
	}
}

func TestGET_assets_symbol_chart_returnsSeries(t *testing.T) {
	t.Parallel()

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	xstocks.RegisterCatalogAsset(handlers.Catalog, xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple",
		SolanaMint: jupiter.AAPLxMint,
	})
	pyth.RegisterChartSeries(handlers.Pyth, "AAPLx", pyth.ChartRange1D, pyth.AssetChartSeries{
		Points: []pyth.ChartPoint{
			{Timestamp: 1_700_000_000, PriceUsdcMicros: 180_000_000},
			{Timestamp: 1_700_003_600, PriceUsdcMicros: 185_000_000},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLx/chart?range=1D", nil)
	req.SetPathValue("symbol", "AAPLx")
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

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	xstocks.RegisterCatalogAsset(handlers.Catalog, xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple",
		SolanaMint: jupiter.AAPLxMint,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLx/chart?range=1W", nil)
	req.SetPathValue("symbol", "AAPLx")
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

	handlers, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	xstocks.RegisterCatalogAsset(handlers.Catalog, xstocks.CatalogAsset{
		Symbol: "AAPLx",
		Name:   "Apple",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLx/chart?range=1Y", nil)
	req.SetPathValue("symbol", "AAPLx")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.GetAssetChartHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
