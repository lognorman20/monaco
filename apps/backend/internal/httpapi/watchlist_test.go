package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

type watchlistTestApp struct {
	handlers *WatchlistHandlers
	assets   *AssetsHandlers
	token    string
	other    string
}

// newWatchlistTestApp wires the watchlist over the same fakes the Stocks tab tests use: Apple
// at $232.05 with a day series, Tesla at $412.70 without one.
func newWatchlistTestApp(t *testing.T) watchlistTestApp {
	t.Helper()
	assets, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	xstocks.RegisterCatalogAsset(assets.Catalog, xstocks.CatalogAsset{
		Symbol: "AAPLx", Name: "Apple", SolanaMint: jupiter.AAPLxMint, Routable: true, LogoURL: "https://example.test/AAPLx.png",
	})
	xstocks.RegisterCatalogAsset(assets.Catalog, xstocks.CatalogAsset{
		Symbol: "TSLAx", Name: "Tesla", SolanaMint: jupiter.TSLAxMint, Routable: true,
	})
	change := "0.0124"
	jupiter.RegisterPrice(assets.Price, jupiter.AAPLxMint, jupiter.TokenPrice{PriceUsdcMicros: 232_050_000, Change24h: &change})
	jupiter.RegisterPrice(assets.Price, jupiter.TSLAxMint, jupiter.TokenPrice{PriceUsdcMicros: 412_700_000})
	pyth.RegisterChartSeries(assets.Pyth, "AAPLx", pyth.ChartRange1D, dayCandles(78))

	watchlist := app.NewWatchlistService(assets.Store, privyClient, assets.Catalog,
		&app.CatalogMarkSource{Catalog: assets.Catalog, Price: assets.Price})
	assets.Watchlist = watchlist
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "watcher", "Watcher")
	_, other := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "other", "Other")
	return watchlistTestApp{
		handlers: &WatchlistHandlers{Watchlist: watchlist, Assets: assets},
		assets:   assets,
		token:    string(token),
		other:    string(other),
	}
}

func watchlistRequest(method, path, token, body string, pathValues map[string]string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range pathValues {
		req.SetPathValue(key, value)
	}
	return req
}

func (a watchlistTestApp) add(t *testing.T, token, symbol string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	a.handlers.AddToWatchlistHandler(rec, watchlistRequest(http.MethodPut, "/v1/me/watchlist/"+symbol, token, "", map[string]string{"symbol": symbol}))
	return rec
}

func (a watchlistTestApp) list(t *testing.T) watchlistResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	a.handlers.GetWatchlistHandler(rec, watchlistRequest(http.MethodGet, "/v1/me/watchlist", a.token, "", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET watchlist = %d; body %s", rec.Code, rec.Body.String())
	}
	var resp watchlistResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode watchlist: %v", err)
	}
	return resp
}

func (a watchlistTestApp) createAlert(t *testing.T, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	a.handlers.CreatePriceAlertHandler(rec, watchlistRequest(http.MethodPost, "/v1/me/alerts", token, body, nil))
	return rec
}

func decodeErrorReason(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v; body %s", err, rec.Body.String())
	}
	return body.Reason
}

func TestWatchlistRoutes_requireAuth(t *testing.T) {
	t.Parallel()
	a := newWatchlistTestApp(t)
	routes := []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
		req  *http.Request
	}{
		{"GET watchlist", a.handlers.GetWatchlistHandler, watchlistRequest(http.MethodGet, "/v1/me/watchlist", "", "", nil)},
		{"PUT symbol", a.handlers.AddToWatchlistHandler, watchlistRequest(http.MethodPut, "/v1/me/watchlist/AAPLx", "", "", map[string]string{"symbol": "AAPLx"})},
		{"DELETE symbol", a.handlers.RemoveFromWatchlistHandler, watchlistRequest(http.MethodDelete, "/v1/me/watchlist/AAPLx", "", "", map[string]string{"symbol": "AAPLx"})},
		{"PUT order", a.handlers.ReorderWatchlistHandler, watchlistRequest(http.MethodPut, "/v1/me/watchlist", "", `{"symbols":[]}`, nil)},
		{"GET alerts", a.handlers.ListPriceAlertsHandler, watchlistRequest(http.MethodGet, "/v1/me/alerts", "", "", nil)},
		{"POST alert", a.handlers.CreatePriceAlertHandler, watchlistRequest(http.MethodPost, "/v1/me/alerts", "", `{}`, nil)},
		{"DELETE alert", a.handlers.DeletePriceAlertHandler, watchlistRequest(http.MethodDelete, "/v1/me/alerts/x", "", "", map[string]string{"id": "x"})},
	}
	for _, route := range routes {
		rec := httptest.NewRecorder()
		route.call(rec, route.req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a token = %d, want 401", route.name, rec.Code)
		}
	}
}

func TestWatchlistRoutes_addListsMarketRowsInOrder(t *testing.T) {
	t.Parallel()
	// Arrange
	a := newWatchlistTestApp(t)

	// Act
	first := a.add(t, a.token, "tslax")
	second := a.add(t, a.token, "AAPLx")
	again := a.add(t, a.token, "TSLAx")
	listed := a.list(t)

	// Assert
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("adds = %d, %d; want 201, 201", first.Code, second.Code)
	}
	if again.Code != http.StatusOK {
		t.Fatalf("re-add = %d, want 200", again.Code)
	}
	var entry watchlistEntryResponse
	_ = json.Unmarshal(first.Body.Bytes(), &entry)
	if entry.Symbol != "TSLAx" || entry.Position != 0 || entry.CreatedAt == "" {
		t.Fatalf("add body = %+v, want TSLAx at 0", entry)
	}
	if len(listed.Assets) != 2 || listed.Assets[0].Symbol != "TSLAx" || listed.Assets[1].Symbol != "AAPLx" {
		t.Fatalf("watchlist rows = %+v, want TSLAx then AAPLx", listed.Assets)
	}
	apple := listed.Assets[1]
	if apple.PriceUsdcMicros == nil || *apple.PriceUsdcMicros != 232_050_000 || apple.Change24h == nil {
		t.Fatalf("AAPLx row = %+v, want the Stocks tab's price and day change", apple)
	}
	if len(apple.Spark) < 2 || apple.LogoURL == "" || apple.Kind != "stock" {
		t.Fatalf("AAPLx row spark=%d logo=%q kind=%q, want the full market row", len(apple.Spark), apple.LogoURL, apple.Kind)
	}
	if listed.Market == nil || listed.Market.Session == "" {
		t.Fatal("watchlist response carries no market session")
	}
}

func TestWatchlistRoutes_addAnUnlistedStockIs404(t *testing.T) {
	t.Parallel()
	a := newWatchlistTestApp(t)

	rec := a.add(t, a.token, "ZZZZx")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("add ZZZZx = %d, want 404; body %s", rec.Code, rec.Body.String())
	}
}

func TestWatchlistRoutes_removeIsIdempotent(t *testing.T) {
	t.Parallel()
	// Arrange
	a := newWatchlistTestApp(t)
	a.add(t, a.token, "AAPLx")

	// Act
	codes := []int{}
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		a.handlers.RemoveFromWatchlistHandler(rec, watchlistRequest(http.MethodDelete, "/v1/me/watchlist/AAPLx", a.token, "", map[string]string{"symbol": "AAPLx"}))
		codes = append(codes, rec.Code)
	}

	// Assert
	if codes[0] != http.StatusNoContent || codes[1] != http.StatusNoContent {
		t.Fatalf("removes = %v, want 204 twice", codes)
	}
	if listed := a.list(t); len(listed.Assets) != 0 {
		t.Fatalf("watchlist after remove = %+v", listed.Assets)
	}
}

func TestWatchlistRoutes_reorder(t *testing.T) {
	t.Parallel()
	// Arrange
	a := newWatchlistTestApp(t)
	a.add(t, a.token, "AAPLx")
	a.add(t, a.token, "TSLAx")
	reorder := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		a.handlers.ReorderWatchlistHandler(rec, watchlistRequest(http.MethodPut, "/v1/me/watchlist", a.token, body, nil))
		return rec
	}

	// Act
	stale := reorder(`{"symbols":["TSLAx"]}`)
	missing := reorder(`{}`)
	ok := reorder(`{"symbols":["TSLAx","AAPLx"]}`)

	// Assert
	if stale.Code != http.StatusConflict || decodeErrorReason(t, stale) != "watchlist_changed" {
		t.Fatalf("stale reorder = %d %s, want 409 watchlist_changed", stale.Code, stale.Body.String())
	}
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("reorder without symbols = %d, want 400", missing.Code)
	}
	var resp reorderWatchlistResponse
	_ = json.Unmarshal(ok.Body.Bytes(), &resp)
	if ok.Code != http.StatusOK || len(resp.Symbols) != 2 || resp.Symbols[0] != "TSLAx" {
		t.Fatalf("reorder = %d %+v, want TSLAx first", ok.Code, resp)
	}
	if listed := a.list(t); listed.Assets[0].Symbol != "TSLAx" {
		t.Fatalf("listed after reorder starts with %s", listed.Assets[0].Symbol)
	}
}

func TestAlertRoutes_createListAndDelete(t *testing.T) {
	t.Parallel()
	// Arrange
	a := newWatchlistTestApp(t)

	// Act
	created := a.createAlert(t, a.token, `{"symbol":"aaplx","direction":"above","priceUsdcMicros":240000000}`)
	listRec := httptest.NewRecorder()
	a.handlers.ListPriceAlertsHandler(listRec, watchlistRequest(http.MethodGet, "/v1/me/alerts?symbol=AAPLx", a.token, "", nil))

	// Assert
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d; body %s", created.Code, created.Body.String())
	}
	var alert priceAlertResponse
	_ = json.Unmarshal(created.Body.Bytes(), &alert)
	if alert.Symbol != "AAPLx" || alert.Direction != "above" || alert.PriceUsdcMicros != 240_000_000 || !alert.Active || alert.TriggeredAt != nil {
		t.Fatalf("created alert = %+v", alert)
	}
	var listed priceAlertsResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode alerts: %v", err)
	}
	if len(listed.Alerts) != 1 || listed.Alerts[0].ID != alert.ID {
		t.Fatalf("alerts = %+v, want the one created", listed.Alerts)
	}
	if len(listed.Assets) != 1 || listed.Assets[0].PriceUsdcMicros == nil || *listed.Assets[0].PriceUsdcMicros != 232_050_000 {
		t.Fatalf("alert assets = %+v, want Apple's market row", listed.Assets)
	}

	otherDelete := httptest.NewRecorder()
	a.handlers.DeletePriceAlertHandler(otherDelete, watchlistRequest(http.MethodDelete, "/v1/me/alerts/"+alert.ID, a.other, "", map[string]string{"id": alert.ID}))
	if otherDelete.Code != http.StatusNotFound {
		t.Fatalf("another member deleting it = %d, want 404", otherDelete.Code)
	}
	ownDelete := httptest.NewRecorder()
	a.handlers.DeletePriceAlertHandler(ownDelete, watchlistRequest(http.MethodDelete, "/v1/me/alerts/"+alert.ID, a.token, "", map[string]string{"id": alert.ID}))
	if ownDelete.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", ownDelete.Code)
	}
}

func TestAlertRoutes_createRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		body   string
		status int
		reason string
	}{
		{"sideways", `{"symbol":"AAPLx","direction":"sideways","priceUsdcMicros":240000000}`, http.StatusBadRequest, ""},
		{"no price", `{"symbol":"AAPLx","direction":"above"}`, http.StatusBadRequest, ""},
		{"unlisted", `{"symbol":"ZZZZx","direction":"above","priceUsdcMicros":240000000}`, http.StatusNotFound, ""},
		{"already above", `{"symbol":"AAPLx","direction":"above","priceUsdcMicros":230000000}`, http.StatusUnprocessableEntity, "alert_already_met"},
		{"already below", `{"symbol":"AAPLx","direction":"below","priceUsdcMicros":240000000}`, http.StatusUnprocessableEntity, "alert_already_met"},
		{"not json", `{"symbol":`, http.StatusBadRequest, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newWatchlistTestApp(t)

			rec := a.createAlert(t, a.token, tc.body)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.reason != "" && decodeErrorReason(t, rec) != tc.reason {
				t.Fatalf("reason = %q, want %q", decodeErrorReason(t, rec), tc.reason)
			}
		})
	}
}

func TestAlertRoutes_twentyFirstActiveAlertIsRefused(t *testing.T) {
	t.Parallel()
	// Arrange
	a := newWatchlistTestApp(t)
	for i := 0; i < app.MaxActivePriceAlerts; i++ {
		body := `{"symbol":"TSLAx","direction":"above","priceUsdcMicros":` + watchlistMicrosJSON(int64(420+i)*1_000_000) + `}`
		if rec := a.createAlert(t, a.token, body); rec.Code != http.StatusCreated {
			t.Fatalf("alert %d = %d; body %s", i+1, rec.Code, rec.Body.String())
		}
	}

	// Act
	rec := a.createAlert(t, a.token, `{"symbol":"TSLAx","direction":"below","priceUsdcMicros":400000000}`)

	// Assert
	if rec.Code != http.StatusConflict || decodeErrorReason(t, rec) != "alert_limit" {
		t.Fatalf("21st alert = %d %s, want 409 alert_limit", rec.Code, rec.Body.String())
	}
}

func TestGET_assets_symbol_carriesTheCallersWatchState(t *testing.T) {
	t.Parallel()
	// Arrange
	a := newWatchlistTestApp(t)
	a.add(t, a.token, "AAPLx")
	a.createAlert(t, a.token, `{"symbol":"AAPLx","direction":"above","priceUsdcMicros":250000000}`)
	a.createAlert(t, a.token, `{"symbol":"AAPLx","direction":"below","priceUsdcMicros":200000000}`)

	// Act
	mine := getAssetDetail(t, a.assets, a.token)
	theirs := getAssetDetail(t, a.assets, a.other)

	// Assert
	if mine.Watching == nil || !*mine.Watching || mine.AlertCount == nil || *mine.AlertCount != 2 {
		t.Fatalf("watching=%v alertCount=%v, want true and 2", mine.Watching, mine.AlertCount)
	}
	if theirs.Watching == nil || *theirs.Watching || theirs.AlertCount == nil || *theirs.AlertCount != 0 {
		t.Fatalf("other member watching=%v alertCount=%v, want false and 0", theirs.Watching, theirs.AlertCount)
	}
}

func watchlistMicrosJSON(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
