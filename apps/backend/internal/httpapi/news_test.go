package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/news"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// fakeNews records the subject it was asked about and answers from a script.
type fakeNews struct {
	subject news.Subject
	asked   bool
	feed    news.Feed
	err     error
}

func (f *fakeNews) Asset(_ context.Context, subject news.Subject) (news.Feed, error) {
	f.subject, f.asked = subject, true
	return f.feed, f.err
}

func (f *fakeNews) Market(context.Context) (news.Feed, error) {
	f.asked = true
	return f.feed, f.err
}

var newsAsOf = time.Date(2026, time.September, 25, 23, 10, 0, 0, time.UTC)

func sampleNewsFeed() news.Feed {
	published := time.Date(2026, time.September, 25, 20, 58, 0, 0, time.UTC)
	return news.Feed{
		AsOf: newsAsOf,
		Items: []news.Item{
			{Title: "What Will the Q3 Earnings Season Show?", URL: "https://finance.yahoo.com/a.html", Source: "Yahoo Finance", PublishedAt: &published},
			{Title: "Alphabet vs. Apple", URL: "https://finance.yahoo.com/b.html", Source: "Yahoo Finance"},
		},
	}
}

func newsTestApp(t *testing.T, reader NewsReader) (*NewsHandlers, string) {
	t.Helper()
	assets, authHandlers, privyClient, _, iso := integrationAssetsApp(t)
	token := seedAssetsToken(t, iso, authHandlers, privyClient)
	xstocks.RegisterCatalogAsset(assets.Catalog, xstocks.CatalogAsset{
		Symbol: "GOOGLx", Name: "Alphabet xStock", SolanaMint: jupiter.AAPLxMint, Routable: true,
	})
	xstocks.RegisterCatalogAsset(assets.Catalog, xstocks.CatalogAsset{
		Symbol: "tSpaceX", Name: "T-SpaceX", SolanaMint: "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v",
		Kind: xstocks.AssetKindPreIPO, Source: xstocks.AssetSourceTessera, Decimals: 9,
	})
	return &NewsHandlers{Assets: assets, News: reader}, token
}

func getAssetNews(h *NewsHandlers, token, symbol string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/assets/symbol/news", nil)
	req.SetPathValue("symbol", symbol)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.GetAssetNewsHandler(rec, req)
	return rec
}

func decodeNewsFeed(t *testing.T, rec *httptest.ResponseRecorder) newsFeedResponse {
	t.Helper()
	var payload newsFeedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode news json: %v; body = %s", err, rec.Body.String())
	}
	return payload
}

func TestGET_assetNews_listedStock(t *testing.T) {
	t.Parallel()
	// Arrange
	reader := &fakeNews{feed: sampleNewsFeed()}
	h, token := newsTestApp(t, reader)

	// Act
	rec := getAssetNews(h, token, "GOOGLx")

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if reader.subject.Ticker != "GOOGL" || reader.subject.Name != "Alphabet" || reader.subject.PreIPO {
		t.Errorf("subject = %+v, want GOOGL / Alphabet", reader.subject)
	}
	payload := decodeNewsFeed(t, rec)
	if payload.AsOf != "2026-09-25T23:10:00Z" || len(payload.Items) != 2 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Items[0].PublishedAt == nil || *payload.Items[0].PublishedAt != "2026-09-25T20:58:00Z" {
		t.Errorf("publishedAt = %v", payload.Items[0].PublishedAt)
	}
	if payload.Items[1].PublishedAt != nil {
		t.Errorf("an undated item should carry publishedAt null, got %v", *payload.Items[1].PublishedAt)
	}
}

func TestGET_assetNews_undatedItemIsNullNotMissing(t *testing.T) {
	t.Parallel()
	// Arrange
	h, token := newsTestApp(t, &fakeNews{feed: sampleNewsFeed()})

	// Act
	rec := getAssetNews(h, token, "GOOGLx")

	// Assert: the key is there, and null, so the contract has one shape.
	var raw struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, ok := raw.Items[1]["publishedAt"]; !ok || string(got) != "null" {
		t.Errorf("publishedAt = %s (present %v), want null", got, ok)
	}
}

func TestGET_assetNews_preIPOSearchesTheCompanyName(t *testing.T) {
	t.Parallel()
	// Arrange
	reader := &fakeNews{feed: news.Feed{AsOf: newsAsOf, Items: []news.Item{}}}
	h, token := newsTestApp(t, reader)

	// Act
	rec := getAssetNews(h, token, "tSpaceX")

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	if reader.subject.Ticker != "" || reader.subject.Name != "SpaceX" || !reader.subject.PreIPO {
		t.Errorf("subject = %+v, want the name SpaceX and no ticker", reader.subject)
	}
	if body := rec.Body.String(); body != `{"items":[],"asOf":"2026-09-25T23:10:00Z"}`+"\n" {
		t.Errorf("body = %s, want an empty list, never null", body)
	}
}

func TestGET_assetNews_refusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		symbol     string
		noToken    bool
		readerErr  error
		wantStatus int
		wantAsked  bool
	}{
		{name: "no token", symbol: "GOOGLx", noToken: true, wantStatus: http.StatusUnauthorized},
		{name: "a symbol that is not a ticker", symbol: "GOOGL x?", wantStatus: http.StatusBadRequest},
		{name: "a symbol the catalogue does not list", symbol: "NOPEx", wantStatus: http.StatusNotFound},
		{name: "every feed down with nothing cached", symbol: "GOOGLx", readerErr: errors.New("status 429"), wantStatus: http.StatusServiceUnavailable, wantAsked: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			reader := &fakeNews{feed: sampleNewsFeed(), err: tc.readerErr}
			h, token := newsTestApp(t, reader)
			if tc.noToken {
				token = ""
			}

			// Act
			rec := getAssetNews(h, token, tc.symbol)

			// Assert
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if reader.asked != tc.wantAsked {
				t.Errorf("news asked = %v, want %v", reader.asked, tc.wantAsked)
			}
		})
	}
}

func TestGET_marketNews(t *testing.T) {
	t.Parallel()
	// Arrange
	reader := &fakeNews{feed: sampleNewsFeed()}
	h, token := newsTestApp(t, reader)
	req := httptest.NewRequest(http.MethodGet, "/v1/news/market", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	// Act
	h.GetMarketNewsHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	if payload := decodeNewsFeed(t, rec); len(payload.Items) != 2 {
		t.Errorf("items = %d, want 2", len(payload.Items))
	}
}

func TestGET_marketNews_refusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		token      bool
		readerErr  error
		wantStatus int
	}{
		{name: "no token", wantStatus: http.StatusUnauthorized},
		{name: "every feed down", token: true, readerErr: errors.New("timeout"), wantStatus: http.StatusServiceUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange
			h, token := newsTestApp(t, &fakeNews{err: tc.readerErr})
			req := httptest.NewRequest(http.MethodGet, "/v1/news/market", nil)
			if tc.token {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()

			// Act
			h.GetMarketNewsHandler(rec, req)

			// Assert
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}
