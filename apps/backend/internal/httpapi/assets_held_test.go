package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/chainlink"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

func TestGET_assetsHeld_missingAuth_returns401(t *testing.T) {
	t.Parallel()
	handlers := &AssetsHandlers{}
	rec := httptest.NewRecorder()
	handlers.HeldAssetsHandler(rec, httptest.NewRequest(http.MethodGet, "/v1/assets/held", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGET_assetsHeld_withoutAHomeServiceIsUnavailable(t *testing.T) {
	t.Parallel()
	handlers := &AssetsHandlers{}
	req := httptest.NewRequest(http.MethodGet, "/v1/assets/held", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	handlers.HeldAssetsHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func heldAssetsApp(t *testing.T) (*AssetsHandlers, string) {
	t.Helper()
	handlers, authHandlers, walletClient, _, iso := integrationAssetsApp(t)
	store := handlers.Store
	symbols := app.NewSymbolResolver(handlers.Catalog)
	handlers.Home = app.NewHomeService(store, handlers.Auth, walletClient, chainlink.NewFakeClient(), nil, symbols)
	token := seedAssetsToken(t, iso, authHandlers, walletClient)
	return handlers, token
}

func TestGET_assetsHeld_noCabalsShipsTwoEmptyArrays(t *testing.T) {
	t.Parallel()
	handlers, token := heldAssetsApp(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/held", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handlers.HeldAssetsHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// A Swift decoder reads [] and an absent key differently from null.
	if !strings.Contains(body, `"held":[]`) || !strings.Contains(body, `"upForVote":[]`) {
		t.Fatalf("body = %s, want two empty arrays", body)
	}
	var resp heldAssetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Market == nil || resp.Market.AsOf != "2026-09-22T14:00:00Z" {
		t.Fatalf("market = %+v, want the session envelope in UTC", resp.Market)
	}
}

func TestGET_assetsHeld_invalidTokenIs401(t *testing.T) {
	t.Parallel()
	handlers, _ := heldAssetsApp(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/assets/held", nil)
	req.Header.Set("Authorization", "Bearer nobody")
	rec := httptest.NewRecorder()
	handlers.HeldAssetsHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
}

// The held and up-for-vote rows reuse the list row, figures and all; a symbol the
// catalog does not know still ships, named.
func TestBuildHeldAssetsResponse_rowsAreTheListRow(t *testing.T) {
	t.Parallel()
	source, charts := newMarketRowSource(t)
	registerRowApple(t, source)
	pyth.RegisterChartSeries(charts, "AAPLc", pyth.ChartRange1D, underlyingDayCandles(40))
	handlers := &AssetsHandlers{
		Catalog: source.Catalog,
		Pyth:    source.Marks,
		Charts:  source.Charts,
		Now:     func() time.Time { return assetsTestClock },
	}
	soon := time.Date(2026, time.September, 22, 18, 0, 0, 0, time.UTC)

	resp := handlers.buildHeldAssetsResponse(context.Background(), app.HeldAssetsResult{
		Held: []app.HeldAsset{{
			Symbol:         "AAPLc",
			Cabals:         []app.HeldAssetCabal{{GroupID: "g1", Name: "Weekend investors", Units: "0.5", ValueUsd: "1.20", DollarPnL: "+0.20", MySliceUsd: "0.60"}},
			TotalValueUsd:  "1.20",
			TotalDollarPnL: "+0.20",
			MySliceUsd:     "0.60",
		}},
		UpForVote: []app.VotableAsset{{Symbol: "DELISTEDc", OpenProposals: 2, SoonestExpiresAt: &soon}},
	})

	if len(resp.Held) != 1 || resp.Held[0].Asset.PriceUsdcMicros == nil || len(resp.Held[0].Asset.Spark) == 0 {
		t.Fatalf("held = %+v, want the priced AAPLc row with its line", resp.Held)
	}
	if resp.Held[0].MySliceUsd != "0.60" || resp.Held[0].Cabals[0].Name != "Weekend investors" {
		t.Fatalf("held = %+v", resp.Held[0])
	}
	vote := resp.UpForVote[0]
	if vote.Asset.Symbol != "DELISTEDc" || vote.Asset.PriceUsdcMicros != nil {
		t.Fatalf("vote asset = %+v, want the bare symbol", vote.Asset)
	}
	if vote.CabalNames == nil || vote.SoonestExpiresAt != "2026-09-22T18:00:00Z" {
		t.Fatalf("vote = %+v, want [] names and a UTC deadline", vote)
	}
}

// A cabal's holdings carry the market's figures beside the cabal's own; cash and
// an undecorated market leave the pot exactly as it was.
func TestPotRowResponses_decorateStocksAndLeaveCashAlone(t *testing.T) {
	t.Parallel()
	source, charts := newMarketRowSource(t)
	registerRowApple(t, source)
	pyth.RegisterChartSeries(charts, "AAPLc", pyth.ChartRange1D, underlyingDayCandles(40))
	rows := []app.GroupViewPotRow{
		{Symbol: "USDC", Units: "3.00", MarkUsd: "1.00", ValueUsd: "3.00", DollarPnL: "+0.00"},
		{Symbol: "AAPLc", Units: "0.5", MarkUsd: "2.40", ValueUsd: "1.20", DollarPnL: "+0.20"},
	}

	decorated := (&GroupHandlers{Market: source}).potRowResponses(context.Background(), rows)
	if decorated[0].Change24h != nil || decorated[0].Spark != nil {
		t.Fatalf("cash row = %+v, want no market figures", decorated[0])
	}
	apple := decorated[1]
	if apple.ValueUsd != "1.20" || apple.DollarPnL != "+0.20" {
		t.Fatalf("the cabal's own figures changed: %+v", apple)
	}
	if apple.Change24h == nil || apple.Change24hBasis != "underlying" || apple.Change24hBasisSymbol != "AAPL" {
		t.Fatalf("change = %v %q/%q, want the underlying's day move", apple.Change24h, apple.Change24hBasis, apple.Change24hBasisSymbol)
	}
	if len(apple.Spark) == 0 || apple.SparkBasis != "underlying" || apple.SparkBasisSymbol != "AAPL" {
		t.Fatalf("spark = %d points %q/%q", len(apple.Spark), apple.SparkBasis, apple.SparkBasisSymbol)
	}

	bare := (&GroupHandlers{}).potRowResponses(context.Background(), rows)
	if bare[1].Change24h != nil || bare[1].Spark != nil || bare[1].ValueUsd != "1.20" {
		t.Fatalf("undecorated row = %+v", bare[1])
	}
}
