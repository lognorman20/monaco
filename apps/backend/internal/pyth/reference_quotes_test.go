package pyth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// hermesFeedRouter stands in for Hermes: it answers the feed search per query
// string and the latest-price call per feed id.
type hermesFeedRouter struct {
	feeds  map[string]string // Hermes query symbol -> feed id
	prices map[string]string // feed id -> raw parsed payload
	status map[string]int    // feed id -> non-200 status for the price call
	calls  *atomic.Int32
}

func (r hermesFeedRouter) server(t *testing.T) *HermesClient {
	t.Helper()
	ClearFeedRegistry()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.calls != nil {
			r.calls.Add(1)
		}
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
			feedID := strings.TrimPrefix(req.URL.Query().Get("ids[]"), "0x")
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
	t.Cleanup(ClearFeedRegistry)
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

func TestEquityQuote_liveCarriesPriceConfidenceAndPublishTime(t *testing.T) {
	now := time.Now().UTC().Unix()
	client := hermesFeedRouter{
		feeds:  map[string]string{"Equity.US.AAPL/USD": "feed-equity"},
		prices: map[string]string{"feed-equity": livePayload("feed-equity", "23140000", "3000", now)},
	}.server(t)

	quote := client.EquityQuote(context.Background(), "AAPLc")
	if quote.Source != QuoteSourcePythEquity || quote.Status != QuoteStatusLive {
		t.Fatalf("quote = %+v, want live pyth_equity", quote)
	}
	if quote.PriceUsdcMicros != 231_400_000 || quote.ConfUsdcMicros != 30_000 {
		t.Fatalf("price/conf = %d/%d", quote.PriceUsdcMicros, quote.ConfUsdcMicros)
	}
	if quote.PublishedAt.Unix() != now || quote.PublishedAt.Location() != time.UTC {
		t.Fatalf("publishedAt = %s, want the feed's publish time in UTC", quote.PublishedAt)
	}
}

func TestEquityQuote_entitlementDenialIsReportedAsSuch(t *testing.T) {
	client := hermesFeedRouter{
		feeds:  map[string]string{"Equity.US.AAPL/USD": "feed-equity"},
		status: map[string]int{"feed-equity": http.StatusForbidden},
	}.server(t)

	quote := client.EquityQuote(context.Background(), "AAPLc")
	if quote.Status != QuoteStatusUnavailable || quote.Reason != QuoteReasonNotEntitled {
		t.Fatalf("equity = %q/%q, want unavailable/not_entitled", quote.Status, quote.Reason)
	}
	if quote.PriceUsdcMicros != 0 {
		t.Fatal("an unavailable quote carries no price")
	}
}

func TestEquityQuote_missingFeedIsNoFeed(t *testing.T) {
	client := hermesFeedRouter{feeds: map[string]string{}}.server(t)
	quote := client.EquityQuote(context.Background(), "SPCXc")
	if quote.Status != QuoteStatusUnavailable || quote.Reason != QuoteReasonNoFeed {
		t.Fatalf("quote = %q/%q, want unavailable/no_feed", quote.Status, quote.Reason)
	}
}

func TestEquityQuote_frozenPublishTimeIsStaleNotLive(t *testing.T) {
	frozenAt := time.Now().UTC().Add(-3 * time.Hour).Unix()
	client := hermesFeedRouter{
		feeds:  map[string]string{"Equity.US.AAPL/USD": "feed-equity"},
		prices: map[string]string{"feed-equity": frozenPayload("feed-equity", "23140000", frozenAt)},
	}.server(t)

	quote := client.EquityQuote(context.Background(), "AAPLc")
	if quote.Status != QuoteStatusStale {
		t.Fatalf("status = %q, want stale", quote.Status)
	}
	if quote.PublishedAt.Unix() != frozenAt {
		t.Fatalf("publishedAt = %s, want the instant it froze at", quote.PublishedAt)
	}
}

func TestEquityQuote_staleByAgeEvenWhenPublishTimesDiffer(t *testing.T) {
	old := time.Now().UTC().Add(-2 * ReferenceQuoteMaxAge).Unix()
	client := hermesFeedRouter{
		feeds:  map[string]string{"Equity.US.AAPL/USD": "feed-equity"},
		prices: map[string]string{"feed-equity": livePayload("feed-equity", "23140000", "3000", old)},
	}.server(t)

	if quote := client.EquityQuote(context.Background(), "AAPLc"); quote.Status != QuoteStatusStale {
		t.Fatalf("status = %q, want stale", quote.Status)
	}
}

func TestEquityQuote_malformedPriceIsUnavailable(t *testing.T) {
	client := hermesFeedRouter{
		feeds:  map[string]string{"Equity.US.AAPL/USD": "feed-equity"},
		prices: map[string]string{"feed-equity": `{"id":"feed-equity","price":{"price":"not-a-number","expo":-5,"publish_time":1}}`},
	}.server(t)

	quote := client.EquityQuote(context.Background(), "AAPLc")
	if quote.Status != QuoteStatusUnavailable || quote.Reason != QuoteReasonUpstream {
		t.Fatalf("equity = %q/%q, want unavailable/upstream_error", quote.Status, quote.Reason)
	}
}

func TestEquityQuote_overflowingPriceIsUnavailableNotWrapped(t *testing.T) {
	client := hermesFeedRouter{
		feeds:  map[string]string{"Equity.US.AAPL/USD": "feed-equity"},
		prices: map[string]string{"feed-equity": livePayload("feed-equity", "99999999999999999999999", "1", time.Now().Unix())},
	}.server(t)

	quote := client.EquityQuote(context.Background(), "AAPLc")
	if quote.Status != QuoteStatusUnavailable {
		t.Fatalf("quote = %+v, want unavailable for a price that overflows int64", quote)
	}
}

func TestEquityQuote_networkFailureIsUpstreamAndNotCached(t *testing.T) {
	ClearFeedRegistry()
	t.Cleanup(ClearFeedRegistry)
	client := NewHermesClientWithHTTP("http://127.0.0.1:1", &http.Client{Timeout: time.Second}, "test-pyth-key")
	quote := client.EquityQuote(context.Background(), "AAPLc")
	if quote.Status != QuoteStatusUnavailable || quote.Reason != QuoteReasonUpstream {
		t.Fatalf("quote = %q/%q, want unavailable/upstream_error", quote.Status, quote.Reason)
	}
	client.quoteMu.Lock()
	cached := len(client.quoteCache)
	client.quoteMu.Unlock()
	if cached != 0 {
		t.Fatal("an outage is not an answer and must not be cached")
	}
}

func TestEquityQuote_withoutAKeyIsNotConfiguredAndCallsNothing(t *testing.T) {
	var calls atomic.Int32
	router := hermesFeedRouter{feeds: map[string]string{"Equity.US.AAPL/USD": "feed-equity"}, calls: &calls}
	keyed := router.server(t)
	keyless := NewHermesClientWithHTTP(keyed.baseURL, nil, "")

	quote := keyless.EquityQuote(context.Background(), "AAPLc")
	if quote.Status != QuoteStatusUnavailable || quote.Reason != QuoteReasonNotConfigured {
		t.Fatalf("quote = %q/%q, want unavailable/not_configured", quote.Status, quote.Reason)
	}
	if calls.Load() != 0 {
		t.Fatalf("hermes called %d times without a key", calls.Load())
	}
}

func TestEquityQuote_isSharedForItsTTL(t *testing.T) {
	var calls atomic.Int32
	now := time.Now().UTC()
	client := hermesFeedRouter{
		feeds:  map[string]string{"Equity.US.AAPL/USD": "feed-equity"},
		prices: map[string]string{"feed-equity": livePayload("feed-equity", "23140000", "3000", now.Unix())},
		calls:  &calls,
	}.server(t)
	client.now = func() time.Time { return now }

	client.EquityQuote(context.Background(), "AAPLc")
	first := calls.Load()
	client.EquityQuote(context.Background(), "AAPLc")
	if calls.Load() != first {
		t.Fatalf("a second read inside the TTL went upstream (%d -> %d calls)", first, calls.Load())
	}
	now = now.Add(EquityQuoteTTL + time.Second)
	client.EquityQuote(context.Background(), "AAPLc")
	if calls.Load() == first {
		t.Fatal("a read after the TTL must refresh")
	}
}

func TestSelectFeed_prefersTheExactSymbolOverASubstringMatch(t *testing.T) {
	t.Parallel()

	feeds := []priceFeedResponse{
		{ID: "index", Attributes: feedAttributes{Symbol: "Equity.Index.AAPL/USD"}},
		{ID: "xstock", Attributes: feedAttributes{Symbol: "Crypto.AAPLX/USD"}},
		{ID: "right", Attributes: feedAttributes{Symbol: "Equity.US.AAPL/USD"}},
	}
	feed, ok := selectFeed(feeds, "Equity.US.AAPL/USD")
	if !ok || feed.ID != "right" {
		t.Fatalf("selectFeed = %+v %v, want the exact match", feed, ok)
	}
	if _, ok := selectFeed(feeds, "Equity.US.MSFT/USD"); ok {
		t.Fatal("a page with no matching symbol must not resolve to the first result")
	}
}

func TestKyberTokenQuote_midSpreadAndNoPublishTime(t *testing.T) {
	t.Parallel()
	// 1 USDC buys 0.00430000 AAPLc (ask 232.558139), 1 AAPLc sells for 231.10 USDC.
	quote := KyberTokenQuote(KyberProbe{
		BuyInUsdcMicros:   1_000_000,
		BuyOutAtomics:     "430000",
		SellInAtomics:     100_000_000,
		SellOutUsdcMicros: "231100000",
		TokenAtomicScale:  100_000_000,
	})
	if quote.Quote.Source != QuoteSourceDexKyber || quote.Quote.Status != QuoteStatusLive {
		t.Fatalf("quote = %+v", quote.Quote)
	}
	if quote.AskUsdcMicros != 232_558_139 || quote.BidUsdcMicros != 231_100_000 {
		t.Fatalf("ask/bid = %d/%d", quote.AskUsdcMicros, quote.BidUsdcMicros)
	}
	if quote.Quote.PriceUsdcMicros != 231_829_069 {
		t.Fatalf("mid = %d", quote.Quote.PriceUsdcMicros)
	}
	if quote.SpreadBps == nil || *quote.SpreadBps != 63 {
		t.Fatalf("spread = %v, want 63 bps", quote.SpreadBps)
	}
	if !quote.Quote.PublishedAt.IsZero() || quote.Quote.ConfUsdcMicros != 0 {
		t.Fatal("a Kyber quote has no publish time and no confidence interval; claiming either is fiction")
	}
}

func TestKyberTokenQuote_halfAMarketIsNoRoute(t *testing.T) {
	t.Parallel()
	cases := map[string]KyberProbe{
		"no buy route":  {BuyInUsdcMicros: 1_000_000, SellInAtomics: 100_000_000, SellOutUsdcMicros: "231100000", TokenAtomicScale: 100_000_000},
		"no sell route": {BuyInUsdcMicros: 1_000_000, BuyOutAtomics: "430000", SellInAtomics: 100_000_000, TokenAtomicScale: 100_000_000},
		"garbage":       {BuyInUsdcMicros: 1_000_000, BuyOutAtomics: "4e5", SellInAtomics: 100_000_000, SellOutUsdcMicros: "231100000", TokenAtomicScale: 100_000_000},
		"zero out":      {BuyInUsdcMicros: 1_000_000, BuyOutAtomics: "0", SellInAtomics: 100_000_000, SellOutUsdcMicros: "231100000", TokenAtomicScale: 100_000_000},
		"overflow":      {BuyInUsdcMicros: 1_000_000, BuyOutAtomics: "430000", SellInAtomics: 1, SellOutUsdcMicros: "99999999999999999999999", TokenAtomicScale: 100_000_000},
	}
	for name, probe := range cases {
		quote := KyberTokenQuote(probe)
		if quote.Quote.Priced() || quote.SpreadBps != nil {
			t.Errorf("%s: quote = %+v, want unavailable with no spread", name, quote)
		}
		if quote.Quote.Status != QuoteStatusUnavailable {
			t.Errorf("%s: status = %q", name, quote.Quote.Status)
		}
	}
}

// tuesdayMidSession is 10:00 ET on a Tuesday, inside the regular session.
var tuesdayMidSession = time.Date(2026, time.September, 22, 14, 0, 0, 0, time.UTC)

func TestPremiumBps_isTheTokenAgainstTheMark(t *testing.T) {
	t.Parallel()
	mark := MarkQuote(&AssetMark{PriceUsdcMicros: 100_000_000, UpdatedAt: tuesdayMidSession.Add(-time.Minute)}, tuesdayMidSession)
	if mark.Status != QuoteStatusLive {
		t.Fatalf("mark = %+v, want live mid-session", mark)
	}
	token := ReferenceQuote{Source: QuoteSourceDexKyber, Status: QuoteStatusLive, PriceUsdcMicros: 99_000_000}
	bps := PremiumBps(token, mark)
	if bps == nil || *bps != -100 {
		t.Fatalf("premium = %v, want -100 bps", bps)
	}
	if PremiumBps(unavailableQuote(QuoteSourceDexKyber, QuoteReasonNoRoute), mark) != nil {
		t.Fatal("no premium without a token price")
	}
	if PremiumBps(token, MarkQuote(nil, tuesdayMidSession)) != nil {
		t.Fatal("no premium without a mark")
	}
	if got := MarkQuote(&AssetMark{PriceUsdcMicros: 0}, tuesdayMidSession); got.Priced() {
		t.Fatal("a zero mark is not a price")
	}
}

func TestMarkQuote_carriesTheRoundTimeInUTC(t *testing.T) {
	t.Parallel()
	struck := time.Date(2026, time.September, 22, 9, 58, 0, 0, time.FixedZone("ET", -4*3600))
	mark := MarkQuote(&AssetMark{PriceUsdcMicros: 100_000_000, UpdatedAt: struck}, tuesdayMidSession)
	if !mark.PublishedAt.Equal(struck) || mark.PublishedAt.Location() != time.UTC {
		t.Fatalf("publishedAt = %v, want the round time in UTC", mark.PublishedAt)
	}
}

func TestMarkQuote_staleMarksCarryNoPremium(t *testing.T) {
	t.Parallel()
	friday := time.Date(2026, time.September, 25, 19, 59, 0, 0, time.UTC)      // 15:59 ET
	fridayNight := time.Date(2026, time.September, 25, 23, 59, 0, 0, time.UTC) // 19:59 ET, post-market
	cases := []struct {
		name string
		mark AssetMark
		now  time.Time
		want QuoteStatus
	}{
		{
			name: "saturday holds friday's close",
			mark: AssetMark{PriceUsdcMicros: 100_000_000, UpdatedAt: fridayNight},
			now:  time.Date(2026, time.September, 26, 15, 0, 0, 0, time.UTC),
			want: QuoteStatusStale,
		},
		{
			name: "sunday afternoon, still under the 25h heartbeat, still friday's close",
			mark: AssetMark{PriceUsdcMicros: 100_000_000, UpdatedAt: fridayNight},
			now:  time.Date(2026, time.September, 26, 22, 0, 0, 0, time.UTC),
			want: QuoteStatusStale,
		},
		{
			name: "a holiday holds the last session's close",
			mark: AssetMark{PriceUsdcMicros: 100_000_000, UpdatedAt: time.Date(2026, time.November, 25, 23, 0, 0, 0, time.UTC)},
			now:  time.Date(2026, time.November, 26, 17, 0, 0, 0, time.UTC), // Thanksgiving
			want: QuoteStatusStale,
		},
		{
			name: "the source's own after-hours verdict",
			mark: AssetMark{PriceUsdcMicros: 100_000_000, UpdatedAt: tuesdayMidSession.Add(-time.Minute), AfterHours: true},
			now:  tuesdayMidSession,
			want: QuoteStatusStale,
		},
		{
			name: "older than the heartbeat mid-session",
			mark: AssetMark{PriceUsdcMicros: 100_000_000, UpdatedAt: tuesdayMidSession.Add(-MarkHeartbeat - time.Minute)},
			now:  tuesdayMidSession,
			want: QuoteStatusStale,
		},
		{
			name: "no round time is never assumed fresh",
			mark: AssetMark{PriceUsdcMicros: 100_000_000},
			now:  tuesdayMidSession,
			want: QuoteStatusStale,
		},
		{
			name: "post-market still moves the 24/5 feed",
			mark: AssetMark{PriceUsdcMicros: 100_000_000, UpdatedAt: friday},
			now:  fridayNight,
			want: QuoteStatusLive,
		},
		{
			name: "a weekday overnight round after the post-market is live",
			mark: AssetMark{PriceUsdcMicros: 100_000_000, UpdatedAt: time.Date(2026, time.September, 23, 2, 0, 0, 0, time.UTC)},
			now:  time.Date(2026, time.September, 23, 2, 30, 0, 0, time.UTC), // Tue 22:30 ET
			want: QuoteStatusLive,
		},
	}
	token := ReferenceQuote{Source: QuoteSourceDexKyber, Status: QuoteStatusLive, PriceUsdcMicros: 101_000_000}
	for _, tc := range cases {
		mark := tc.mark
		got := MarkQuote(&mark, tc.now)
		if got.Status != tc.want {
			t.Errorf("%s: status = %q, want %q", tc.name, got.Status, tc.want)
			continue
		}
		if !got.Priced() {
			t.Errorf("%s: a stale mark is still a price to show", tc.name)
		}
		premium := PremiumBps(token, got)
		if tc.want == QuoteStatusStale && premium != nil {
			t.Errorf("%s: premium = %d against a stale mark", tc.name, *premium)
		}
		if tc.want == QuoteStatusLive && premium == nil {
			t.Errorf("%s: want a premium against a live mark", tc.name)
		}
	}
}
