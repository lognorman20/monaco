package pyth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

const (
	// Post–Pyth Core upgrade Hermes host (drop-in for hermes.pyth.network).
	defaultHermesBaseURL = "https://pyth.dourolabs.app/hermes"
	defaultTimeout       = 15 * time.Second
)

// HermesClient fetches equity marks from the Pyth Hermes HTTP API.
type HermesClient struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
	chartCache *ChartSeriesCache
	// seriesSource serves a whole chart range in one call (Pyth Benchmarks). Nil
	// falls back to sampling the Hermes historical endpoint point by point.
	seriesSource  SeriesSource
	seriesBreaker *seriesBreaker

	quoteCacheMu sync.RWMutex
	quoteCache   map[string]referenceQuoteEntry
}

// NewHermesClient returns a production Hermes client authenticated with a Pyth API key.
func NewHermesClient(apiKey string) *HermesClient {
	return NewHermesClientWithBaseURL(defaultHermesBaseURL, apiKey)
}

// NewHermesClientWithBaseURL returns a Hermes client using an explicit API host.
func NewHermesClientWithBaseURL(baseURL, apiKey string) *HermesClient {
	return newHermesClient(baseURL, nil, apiKey)
}

// NewHermesClientWithHTTP is used in tests to inject an httptest server transport.
func NewHermesClientWithHTTP(baseURL string, httpClient *http.Client, apiKey string) *HermesClient {
	return newHermesClient(baseURL, httpClient, apiKey)
}

func newHermesClient(baseURL string, httpClient *http.Client, apiKey string) *HermesClient {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultHermesBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &HermesClient{
		baseURL:       strings.TrimRight(baseURL, "/"),
		httpClient:    telemetry.InstrumentClient(telemetry.UpstreamPyth, httpClient),
		apiKey:        strings.TrimSpace(apiKey),
		chartCache:    NewChartSeriesCache(DefaultChartSeriesCacheTTL),
		seriesBreaker: newSeriesBreaker(defaultSeriesBreakerCooldown),
		quoteCache:    make(map[string]referenceQuoteEntry),
	}
}

// WithSeriesSource points chart history at a one-call source such as Pyth
// Benchmarks. The Hermes per-sample path stays as the fallback behind a breaker.
func (c *HermesClient) WithSeriesSource(source SeriesSource) *HermesClient {
	c.seriesSource = source
	return c
}

func (c *HermesClient) setHermesAuth(req *http.Request) error {
	if c.apiKey == "" {
		return fmt.Errorf("PYTH_API_KEY is required for Hermes price updates")
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	return nil
}

type priceFeedResponse struct {
	ID          string         `json:"id"`
	Attributes  feedAttributes `json:"attributes"`
	MarketHours marketHours    `json:"market_hours"`
}

type feedAttributes struct {
	Symbol string `json:"symbol"`
}

type marketHours struct {
	IsOpen bool `json:"is_open"`
}

type latestPriceResponse struct {
	Parsed []parsedPriceUpdate `json:"parsed"`
}

type parsedPriceUpdate struct {
	ID       string      `json:"id"`
	Price    priceUpdate `json:"price"`
	Metadata priceMeta   `json:"metadata"`
}

type priceUpdate struct {
	Price string `json:"price"`
	// Conf is Pyth's confidence interval around Price, in the same exponent. It is
	// the vendor's own statement of how sure it is, and the only honest way to show
	// "price certainty" on the stats grid.
	Conf        string `json:"conf"`
	Expo        int32  `json:"expo"`
	PublishTime int64  `json:"publish_time"`
}

type priceMeta struct {
	PrevPublishTime int64 `json:"prev_publish_time"`
}

func (c *HermesClient) USDCOnlyPot(ctx context.Context, treasury TreasuryRef) (NavInput, error) {
	return NavInput{
		TreasuryUsdc: treasury.TreasuryUsdc,
	}, nil
}

func (c *HermesClient) MarkedPot(ctx context.Context, treasury TreasuryRef, holdings []CostBasis) (NavInput, error) {
	marked := make([]MarkedHolding, 0, len(holdings))
	for _, holding := range holdings {
		markedHolding, err := c.markHolding(ctx, holding)
		if err != nil {
			return NavInput{}, err
		}
		marked = append(marked, markedHolding)
	}
	return NavInput{
		TreasuryUsdc: treasury.TreasuryUsdc,
		Holdings:     marked,
		AfterHours:   PotAfterHours(marked),
	}, nil
}

func (c *HermesClient) markHolding(ctx context.Context, holding CostBasis) (MarkedHolding, error) {
	mark, err := c.EquityMark(ctx, holding.Symbol)
	if err != nil {
		return MarkedHolding{}, err
	}
	return MarkedHolding{
		Symbol:       holding.Symbol,
		Mint:         holding.Mint,
		Units:        holding.Units,
		MarkUsdc:     mark.PriceUsdcMicros,
		CostBasis:    holding.Price,
		AfterHours:   mark.AfterHours,
		Source:       MarkSourcePyth,
		Decimals:     holding.Decimals,
		Kind:         holding.Kind,
		UiMultiplier: holding.UiMultiplier,
	}, nil
}

// resolveFeedSession returns a cached feed id and a fresh market_hours.is_open value.
// It always goes to the wire, because is_open is the part it is asked for.
func (c *HermesClient) resolveFeedSession(ctx context.Context, symbol string) (feedID string, isOpen bool, err error) {
	return c.fetchFeedSession(ctx, symbol, EquityQuerySymbol(symbol))
}

// resolveFeedID answers from the registry when the feed id is already known. Feed
// ids do not change, so a caller that only needs the id has nothing to learn from a
// /v2/price_feeds round trip — and the reference-quote path has two legs, so paying
// for one lookup per leg per poll doubled the cost of an asset screen for nothing.
func (c *HermesClient) resolveFeedID(ctx context.Context, symbol, query string) (string, error) {
	if cachedID, ok := lookupFeedID(query); ok && cachedID != "" {
		return cachedID, nil
	}
	feedID, _, err := c.fetchFeedSession(ctx, symbol, query)
	return feedID, err
}

// fetchFeedSession fetches the feed and its market_hours.is_open. The registry is
// consulted for the id so a feed registered by a test or a previous lookup keeps
// winning, but the request happens regardless: is_open is only fresh from upstream.
func (c *HermesClient) fetchFeedSession(ctx context.Context, symbol, query string) (feedID string, isOpen bool, err error) {
	feed, err := c.fetchPriceFeedByQuery(ctx, symbol, query)
	if err != nil {
		return "", false, err
	}

	if cachedID, ok := lookupFeedID(query); ok && cachedID != "" {
		return cachedID, feed.MarketHours.IsOpen, nil
	}

	registerFeedID(query, feed.ID)
	return feed.ID, feed.MarketHours.IsOpen, nil
}

func (c *HermesClient) fetchPriceFeedBySymbol(ctx context.Context, symbol string) (priceFeedResponse, error) {
	return c.fetchPriceFeedByQuery(ctx, symbol, EquityQuerySymbol(symbol))
}

func (c *HermesClient) fetchPriceFeedByQuery(ctx context.Context, symbol, query string) (priceFeedResponse, error) {
	endpoint := fmt.Sprintf("%s/v2/price_feeds?query=%s", c.baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return priceFeedResponse{}, err
	}
	if err := c.setHermesAuth(req); err != nil {
		return priceFeedResponse{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		lookupErr := fmt.Errorf("pyth feed lookup %s: %w", symbol, err)
		logFeedLookup(symbol, "", lookupErr)
		return priceFeedResponse{}, lookupErr
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logFeedLookup(symbol, "", err)
		return priceFeedResponse{}, err
	}
	if resp.StatusCode != http.StatusOK {
		lookupErr := hermesRequestError(fmt.Sprintf("pyth feed lookup %s", symbol), resp.StatusCode, body)
		logFeedLookup(symbol, "", lookupErr)
		return priceFeedResponse{}, lookupErr
	}

	var feeds []priceFeedResponse
	if err := json.Unmarshal(body, &feeds); err != nil {
		lookupErr := fmt.Errorf("decode pyth feed lookup %s: %w", symbol, err)
		logFeedLookup(symbol, "", lookupErr)
		return priceFeedResponse{}, lookupErr
	}
	feed, found := selectFeed(feeds, query)
	if !found {
		lookupErr := fmt.Errorf("%w: %s (%s)", ErrFeedNotFound, symbol, query)
		logFeedLookup(symbol, "", lookupErr)
		return priceFeedResponse{}, lookupErr
	}
	logFeedLookup(symbol, feed.ID, nil)
	return feed, nil
}

// selectFeed picks the feed whose own symbol equals the query. The Hermes search is
// a substring match, so "Crypto.AAPLX/USD" can come back alongside other feeds; the
// first result is not necessarily the one asked for, and pricing the wrong feed is
// the kind of mistake nobody notices until it is on a chart.
func selectFeed(feeds []priceFeedResponse, query string) (priceFeedResponse, bool) {
	normalizedQuery := normalizeSymbol(query)
	for _, feed := range feeds {
		if feed.ID != "" && normalizeSymbol(feed.Attributes.Symbol) == normalizedQuery {
			return feed, true
		}
	}
	// Older Hermes payloads omit attributes entirely; fall back to the first result
	// only when no feed in the page claims a symbol at all.
	for _, feed := range feeds {
		if strings.TrimSpace(feed.Attributes.Symbol) != "" {
			return priceFeedResponse{}, false
		}
	}
	if len(feeds) > 0 && feeds[0].ID != "" {
		return feeds[0], true
	}
	return priceFeedResponse{}, false
}

func (c *HermesClient) fetchLatestPrice(ctx context.Context, feedID string) (parsedPriceUpdate, error) {
	endpoint := fmt.Sprintf("%s/v2/updates/price/latest?ids[]=%s", c.baseURL, url.QueryEscape(feedID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return parsedPriceUpdate{}, err
	}
	if err := c.setHermesAuth(req); err != nil {
		return parsedPriceUpdate{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		priceErr := fmt.Errorf("pyth latest price: %w", err)
		logLatestPrice(feedID, priceErr)
		return parsedPriceUpdate{}, priceErr
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logLatestPrice(feedID, err)
		return parsedPriceUpdate{}, err
	}
	if resp.StatusCode != http.StatusOK {
		priceErr := hermesRequestError("pyth latest price", resp.StatusCode, body)
		logLatestPrice(feedID, priceErr)
		return parsedPriceUpdate{}, priceErr
	}

	var payload latestPriceResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		priceErr := fmt.Errorf("decode pyth latest price: %w", err)
		logLatestPrice(feedID, priceErr)
		return parsedPriceUpdate{}, priceErr
	}
	if len(payload.Parsed) == 0 {
		priceErr := fmt.Errorf("pyth latest price: empty parsed payload")
		logLatestPrice(feedID, priceErr)
		return parsedPriceUpdate{}, priceErr
	}
	logLatestPrice(feedID, nil)
	return payload.Parsed[0], nil
}

func priceToUSDCMicros(price string, expo int32) (int64, error) {
	raw, err := parseDecimalInt(price)
	if err != nil {
		return 0, err
	}
	// Hermes expo is negative (e.g. -8). USDC micros use expo -6.
	shift := int64(expo) + 6
	scaled, err := scaleInt64(raw, shift)
	if err != nil {
		return 0, err
	}
	if scaled < 0 {
		return 0, fmt.Errorf("negative mark")
	}
	return scaled, nil
}

func parseDecimalInt(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty price")
	}
	sign := int64(1)
	if strings.HasPrefix(value, "-") {
		sign = -1
		value = strings.TrimPrefix(value, "-")
	}
	var n int64
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid price %q", value)
		}
		digit := int64(ch - '0')
		// Untrusted upstream input: a long enough digit string wraps int64 silently
		// and turns a nonsense payload into a plausible-looking price. This path
		// parses both `price` and `conf` from Hermes, and the Jupiter out-amount.
		if n > (math.MaxInt64-digit)/10 {
			return 0, fmt.Errorf("price %q overflows", value)
		}
		n = n*10 + digit
	}
	return sign * n, nil
}

func scaleInt64(value int64, shift int64) (int64, error) {
	if shift == 0 {
		return value, nil
	}
	if shift > 0 {
		if shift > 18 {
			return 0, fmt.Errorf("shift overflow")
		}
		multiplier := int64(math.Pow10(int(shift)))
		return value * multiplier, nil
	}
	divisor := int64(math.Pow10(int(-shift)))
	return value / divisor, nil
}

// isFrozenEquityMark reports whether Hermes is serving a stale equity mark.
func isFrozenEquityMark(publishTime, prevPublishTime int64) bool {
	return publishTime > 0 && prevPublishTime > 0 && publishTime == prevPublishTime
}

func hermesRequestError(prefix string, status int, body []byte) error {
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		return &RequestError{Status: status, message: fmt.Sprintf("%s: status %d", prefix, status)}
	}
	const maxDetailLen = 240
	if len(detail) > maxDetailLen {
		detail = detail[:maxDetailLen]
	}
	if status == http.StatusForbidden && strings.Contains(strings.ToLower(detail), "not entitled") {
		return &RequestError{Status: status, message: fmt.Sprintf("%s: status %d: %s (accept equity feed grants in Pyth Terminal)", prefix, status, detail)}
	}
	return &RequestError{Status: status, message: fmt.Sprintf("%s: status %d: %s", prefix, status, detail)}
}
