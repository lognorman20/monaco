package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/news"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// NewsReader answers headlines. *news.Service is the live one.
type NewsReader interface {
	Asset(ctx context.Context, subject news.Subject) (news.Feed, error)
	Market(ctx context.Context) (news.Feed, error)
}

// NewsHandlers serves GET /v1/assets/{symbol}/news and GET /v1/news/market.
type NewsHandlers struct {
	// Assets authenticates the caller and resolves the symbol exactly as the other
	// asset routes do, so a symbol that has no stock screen has no news either.
	Assets *AssetsHandlers
	News   NewsReader
}

type newsItemResponse struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Source string `json:"source"`
	// PublishedAt is RFC3339 in UTC, or null when the feed gave no usable date.
	PublishedAt *string `json:"publishedAt"`
}

type newsFeedResponse struct {
	// Items is newest first, at most twelve, and always a list.
	Items []newsItemResponse `json:"items"`
	// AsOf is when the list was read from the feed. A list kept through a feed
	// outage keeps the time it was actually read.
	AsOf string `json:"asOf"`
}

// GetAssetNewsHandler handles GET /v1/assets/{symbol}/news.
func (h *NewsHandlers) GetAssetNewsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/assets/{symbol}/news")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	if _, err := h.Assets.authorizeUser(ctx, token); err != nil {
		writeAssetsError(ctx, log, w, err)
		return
	}

	symbol := strings.TrimSpace(r.PathValue("symbol"))
	if symbol == "" {
		logJSONError(ctx, log, "missing_symbol", w, http.StatusNotFound, "asset not found")
		return
	}
	// The symbol ends up in a feed URL's query and in the logs; keep it to a ticker's shape.
	if !isPlausibleTicker(symbol) {
		logJSONError(ctx, log, "invalid_symbol", w, http.StatusBadRequest, "invalid symbol")
		return
	}

	asset, found, err := h.Assets.lookupAsset(ctx, symbol)
	if err != nil {
		logJSONError(ctx, log, "asset_lookup_failed", w, http.StatusInternalServerError, "internal server error", "symbol", symbol, "err", err.Error())
		return
	}
	if !found {
		logJSONError(ctx, log, "asset_not_found", w, http.StatusNotFound, "asset not found", "symbol", symbol)
		return
	}
	if h.News == nil {
		logJSONError(ctx, log, "news_not_configured", w, http.StatusServiceUnavailable, "news unavailable", "symbol", symbol)
		return
	}

	normalized := asset.Normalize()
	subject := news.SubjectFor(normalized.Symbol, normalized.Name, normalized.Kind == xstocks.AssetKindPreIPO)
	feed, err := h.News.Asset(ctx, subject)
	if err != nil {
		logJSONError(ctx, log, "news_unavailable", w, http.StatusServiceUnavailable, "news unavailable",
			"symbol", symbol, "ticker", subject.Ticker, "name", subject.Name, "err", err.Error())
		return
	}
	resp := newsFeedResponseFor(feed)
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok",
		"symbol", symbol,
		"ticker", subject.Ticker,
		"item_count", len(resp.Items),
		"as_of", resp.AsOf,
	)
}

// GetMarketNewsHandler handles GET /v1/news/market.
func (h *NewsHandlers) GetMarketNewsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/news/market")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	if _, err := h.Assets.authorizeUser(ctx, token); err != nil {
		writeAssetsError(ctx, log, w, err)
		return
	}
	if h.News == nil {
		logJSONError(ctx, log, "news_not_configured", w, http.StatusServiceUnavailable, "news unavailable")
		return
	}

	feed, err := h.News.Market(ctx)
	if err != nil {
		logJSONError(ctx, log, "news_unavailable", w, http.StatusServiceUnavailable, "news unavailable", "err", err.Error())
		return
	}
	resp := newsFeedResponseFor(feed)
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok", "item_count", len(resp.Items), "as_of", resp.AsOf)
}

func newsFeedResponseFor(feed news.Feed) newsFeedResponse {
	resp := newsFeedResponse{
		Items: make([]newsItemResponse, 0, len(feed.Items)),
		AsOf:  feed.AsOf.UTC().Format(time.RFC3339),
	}
	for _, item := range feed.Items {
		row := newsItemResponse{Title: item.Title, URL: item.URL, Source: item.Source}
		if item.PublishedAt != nil {
			published := item.PublishedAt.UTC().Format(time.RFC3339)
			row.PublishedAt = &published
		}
		resp.Items = append(resp.Items, row)
	}
	return resp
}
