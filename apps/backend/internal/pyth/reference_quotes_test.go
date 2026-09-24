package pyth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// hermesFeedRouter stands in for Hermes: it answers the feed search per query
// string and the latest-price call per feed id.
type hermesFeedRouter struct {
	feeds  map[string]string // Hermes query symbol -> feed id
	prices map[string]string // feed id -> raw parsed payload
	status map[string]int    // feed id -> non-200 status for the price call
}

func (r hermesFeedRouter) server(t *testing.T) *HermesClient {
	t.Helper()
	ClearFeedRegistry()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/v2/price_feeds":
			query := req.URL.Query().Get("query")
			feedID, ok := r.feeds[query]
			if !ok {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[{"id":"` + feedID + `","attributes":{"symbol":"` + query + `"},"market_hours":{"is_open":true}}]`))
		case strings.HasPrefix(req.URL.Path, "/v2/updates/price/latest"):
			feedID := req.URL.Query().Get("ids[]")
			if status, ok := r.status[feedID]; ok {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`Not entitled: feed ` + feedID))
				return
			}
			payload, ok := r.prices[feedID]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"parsed":[` + payload + `]}`))
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(server.Close)
	return NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
}

func livePayload(feedID, price, conf string, publishTime int64) string {
	return `{"id":"` + feedID + `","price":{"price":"` + price + `","conf":"` + conf +
		`","expo":-5,"publish_time":` + itoa(publishTime) + `},"metadata":{"prev_publish_time":` + itoa(publishTime-1) + `}}`
}

func frozenPayload(feedID, price string, publishTime int64) string {
	return `{"id":"` + feedID + `","price":{"price":"` + price + `","conf":"300","expo":-5,"publish_time":` +
		itoa(publishTime) + `},"metadata":{"prev_publish_time":` + itoa(publishTime) + `}}`
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func TestReferenceQuotes_bothFeedsLiveGivesAPremium(t *testing.T) {
	now := time.Now().UTC().Unix()
	router := hermesFeedRouter{
		feeds: map[string]string{
			"Equity.US.AAPL/USD": "feed-equity",
			"Crypto.AAPLX/USD":   "feed-token",
		},
		prices: map[string]string{
			// 231.40 with a 3c confidence, and the token 0.28% above it.
			"feed-equity": livePayload("feed-equity", "23140000", "3000", now),
			"feed-token":  livePayload("feed-token", "23205000", "5000", now),
		},
	}
	client := router.server(t)

	quotes, err := client.ReferenceQuotes(context.Background(), "AAPLx")
	if err != nil {
		t.Fatalf("ReferenceQuotes: %v", err)
	}
	if quotes.Equity.Status != QuoteStatusLive || quotes.Token.Status != QuoteStatusLive {
		t.Fatalf("statuses = %q/%q, want live/live", quotes.Equity.Status, quotes.Token.Status)
	}
	if quotes.Equity.PriceUsdcMicros != 231_400_000 {
		t.Fatalf("equity price = %d, want 231400000", quotes.Equity.PriceUsdcMicros)
	}
	if quotes.Token.PriceUsdcMicros != 232_050_000 {
		t.Fatalf("token price = %d, want 232050000", quotes.Token.PriceUsdcMicros)
	}
	if quotes.Equity.ConfUsdcMicros != 30_000 {
		t.Fatalf("equity conf = %d, want 30000 (3 cents)", quotes.Equity.ConfUsdcMicros)
	}
	if quotes.Equity.Source != QuoteSourcePythEquity || quotes.Token.Source != QuoteSourcePythCrypto {
		t.Fatalf("sources = %q/%q", quotes.Equity.Source, quotes.Token.Source)
	}
	premium := quotes.PremiumBps()
	if premium == nil {
		t.Fatal("expected a premium when both legs are priced")
	}
	if *premium != 28 {
		t.Fatalf("premium = %d bps, want 28", *premium)
	}
	if quotes.AsOf.Location() != time.UTC {
		t.Fatalf("asOf location = %s, want UTC", quotes.AsOf.Location())
	}
}

func TestReferenceQuotes_missingCryptoFeedIsExplicitlyUnavailable(t *testing.T) {
	now := time.Now().UTC().Unix()
	router := hermesFeedRouter{
		feeds:  map[string]string{"Equity.US.NVDA/USD": "feed-equity"},
		prices: map[string]string{"feed-equity": livePayload("feed-equity", "18500000", "2000", now)},
	}
	client := router.server(t)

	quotes, err := client.ReferenceQuotes(context.Background(), "NVDAx")
	if err != nil {
		t.Fatalf("ReferenceQuotes: %v", err)
	}
	if quotes.Token.Status != QuoteStatusUnavailable {
		t.Fatalf("token status = %q, want unavailable", quotes.Token.Status)
	}
	if quotes.Token.Reason != QuoteReasonNoFeed {
		t.Fatalf("token reason = %q, want %q", quotes.Token.Reason, QuoteReasonNoFeed)
	}
	if quotes.Token.PriceUsdcMicros != 0 {
		t.Fatalf("token price = %d, want no price at all", quotes.Token.PriceUsdcMicros)
	}
	if quotes.PremiumBps() != nil {
		t.Fatal("a premium against a missing leg would be invented")
	}
}

func TestReferenceQuotes_entitlementDenialIsReportedAsSuch(t *testing.T) {
	now := time.Now().UTC().Unix()
	router := hermesFeedRouter{
		feeds: map[string]string{
			"Equity.US.AAPL/USD": "feed-equity",
			"Crypto.AAPLX/USD":   "feed-token",
		},
		prices: map[string]string{"feed-token": livePayload("feed-token", "23205000", "5000", now)},
		status: map[string]int{"feed-equity": http.StatusForbidden},
	}
	client := router.server(t)

	quotes, err := client.ReferenceQuotes(context.Background(), "AAPLx")
	if err != nil {
		t.Fatalf("ReferenceQuotes: %v", err)
	}
	if quotes.Equity.Status != QuoteStatusUnavailable || quotes.Equity.Reason != QuoteReasonNotEntitled {
		t.Fatalf("equity = %q/%q, want unavailable/not_entitled", quotes.Equity.Status, quotes.Equity.Reason)
	}
	if quotes.Token.Status != QuoteStatusLive {
		t.Fatalf("token status = %q, want the working leg to survive", quotes.Token.Status)
	}
}

func TestReferenceQuotes_frozenPublishTimeIsStaleNotLive(t *testing.T) {
	frozenAt := time.Now().UTC().Add(-3 * time.Hour).Unix()
	router := hermesFeedRouter{
		feeds: map[string]string{
			"Equity.US.AAPL/USD": "feed-equity",
			"Crypto.AAPLX/USD":   "feed-token",
		},
		prices: map[string]string{
			"feed-equity": frozenPayload("feed-equity", "23140000", frozenAt),
			"feed-token":  livePayload("feed-token", "23205000", "5000", time.Now().UTC().Unix()),
		},
	}
	client := router.server(t)

	quotes, err := client.ReferenceQuotes(context.Background(), "AAPLx")
	if err != nil {
		t.Fatalf("ReferenceQuotes: %v", err)
	}
	if quotes.Equity.Status != QuoteStatusStale {
		t.Fatalf("equity status = %q, want stale", quotes.Equity.Status)
	}
	if quotes.Equity.PublishedAt.Unix() != frozenAt {
		t.Fatalf("publishedAt = %s, want the instant it froze at", quotes.Equity.PublishedAt)
	}
	// A stale price is still a real price, so the comparison still means something.
	if quotes.PremiumBps() == nil {
		t.Fatal("expected a premium against the last real equity print")
	}
}

func TestReferenceQuotes_staleByAgeEvenWhenPublishTimesDiffer(t *testing.T) {
	old := time.Now().UTC().Add(-2 * ReferenceQuoteMaxAge).Unix()
	router := hermesFeedRouter{
		feeds:  map[string]string{"Equity.US.AAPL/USD": "feed-equity"},
		prices: map[string]string{"feed-equity": livePayload("feed-equity", "23140000", "3000", old)},
	}
	client := router.server(t)

	quotes, err := client.ReferenceQuotes(context.Background(), "AAPLx")
	if err != nil {
		t.Fatalf("ReferenceQuotes: %v", err)
	}
	if quotes.Equity.Status != QuoteStatusStale {
		t.Fatalf("equity status = %q, want stale", quotes.Equity.Status)
	}
}

func TestReferenceQuotes_malformedPriceIsUnavailable(t *testing.T) {
	router := hermesFeedRouter{
		feeds:  map[string]string{"Equity.US.AAPL/USD": "feed-equity"},
		prices: map[string]string{"feed-equity": `{"id":"feed-equity","price":{"price":"not-a-number","expo":-5,"publish_time":1}}`},
	}
	client := router.server(t)

	quotes, err := client.ReferenceQuotes(context.Background(), "AAPLx")
	if err != nil {
		t.Fatalf("ReferenceQuotes: %v", err)
	}
	if quotes.Equity.Status != QuoteStatusUnavailable || quotes.Equity.Reason != QuoteReasonUpstream {
		t.Fatalf("equity = %q/%q, want unavailable/upstream_error", quotes.Equity.Status, quotes.Equity.Reason)
	}
}

func TestSelectFeed_prefersTheExactSymbolOverASubstringMatch(t *testing.T) {
	t.Parallel()

	feeds := []priceFeedResponse{
		{ID: "wrong", Attributes: feedAttributes{Symbol: "Crypto.AAPLX2/USD"}},
		{ID: "right", Attributes: feedAttributes{Symbol: "Crypto.AAPLX/USD"}},
	}
	feed, ok := selectFeed(feeds, "Crypto.AAPLX/USD")
	if !ok || feed.ID != "right" {
		t.Fatalf("selectFeed = %+v %v, want the exact match", feed, ok)
	}
	if _, ok := selectFeed(feeds, "Crypto.MSFTX/USD"); ok {
		t.Fatal("a page with no matching symbol must not resolve to the first result")
	}
}

func TestJupiterFallbackQuote_isLabelledAsOnChainNotPyth(t *testing.T) {
	t.Parallel()

	quote := JupiterFallbackQuote(232_050_000)
	if quote.Source != QuoteSourceJupiter {
		t.Fatalf("source = %q, want jupiter", quote.Source)
	}
	if quote.Status != QuoteStatusLive || quote.PriceUsdcMicros != 232_050_000 {
		t.Fatalf("quote = %+v", quote)
	}
	if quote.ConfUsdcMicros != 0 {
		t.Fatal("Jupiter publishes no confidence interval; claiming one would be fiction")
	}
	if !quote.PublishedAt.IsZero() {
		t.Fatal("Jupiter does not say when its price was struck; the server's own clock is not an answer")
	}
	if unpriced := JupiterFallbackQuote(0); unpriced.Status != QuoteStatusUnavailable {
		t.Fatalf("status = %q, want unavailable for a missing price", unpriced.Status)
	}
}

func TestPremiumBps_signAndRounding(t *testing.T) {
	t.Parallel()

	discount := ReferenceQuotes{
		Equity: ReferenceQuote{Status: QuoteStatusLive, PriceUsdcMicros: 100_000_000},
		Token:  ReferenceQuote{Status: QuoteStatusLive, PriceUsdcMicros: 99_000_000},
	}
	bps := discount.PremiumBps()
	if bps == nil || *bps != -100 {
		t.Fatalf("premium = %v, want -100 bps", bps)
	}

	unavailable := ReferenceQuotes{
		Equity: ReferenceQuote{Status: QuoteStatusUnavailable},
		Token:  ReferenceQuote{Status: QuoteStatusLive, PriceUsdcMicros: 99_000_000},
	}
	if unavailable.PremiumBps() != nil {
		t.Fatal("expected no premium without both legs")
	}
}
