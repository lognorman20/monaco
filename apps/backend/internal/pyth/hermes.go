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
	"time"
)

const (
	defaultHermesBaseURL = "https://hermes.pyth.network"
	defaultTimeout       = 15 * time.Second
)

// HermesClient fetches equity marks from the Pyth Hermes HTTP API.
type HermesClient struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
}

// NewHermesClient returns a production Hermes client authenticated with a Pyth API key.
func NewHermesClient(apiKey string) *HermesClient {
	return &HermesClient{
		baseURL: defaultHermesBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		apiKey: strings.TrimSpace(apiKey),
	}
}

// NewHermesClientWithHTTP is used in tests to inject an httptest server transport.
func NewHermesClientWithHTTP(baseURL string, httpClient *http.Client, apiKey string) *HermesClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &HermesClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		apiKey:     strings.TrimSpace(apiKey),
	}
}

func (c *HermesClient) setHermesAuth(req *http.Request) error {
	if c.apiKey == "" {
		return fmt.Errorf("PYTH_API_KEY is required for Hermes price updates")
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	return nil
}

type priceFeedResponse struct {
	ID           string `json:"id"`
	MarketHours  marketHours `json:"market_hours"`
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
	Price       string `json:"price"`
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
	feedID, isOpen, err := c.resolveFeedSession(ctx, holding.Symbol)
	if err != nil {
		return MarkedHolding{}, err
	}

	latest, err := c.fetchLatestPrice(ctx, feedID)
	if err != nil {
		return MarkedHolding{}, err
	}

	markUsdc, err := priceToUSDCMicros(latest.Price.Price, latest.Price.Expo)
	if err != nil {
		return MarkedHolding{}, err
	}

	// After-hours when cash equity session closed (Hermes market_hours.is_open)
	// or Hermes serves a frozen mark (publish_time == prev_publish_time).
	afterHours := !isOpen || isFrozenEquityMark(latest.Price.PublishTime, latest.Metadata.PrevPublishTime)

	return MarkedHolding{
		Symbol:     holding.Symbol,
		Mint:       holding.Mint,
		Units:      holding.Units,
		MarkUsdc:   markUsdc,
		CostBasis:  holding.Price,
		AfterHours: afterHours,
	}, nil
}

// resolveFeedSession returns a cached feed id and a fresh market_hours.is_open value.
func (c *HermesClient) resolveFeedSession(ctx context.Context, symbol string) (feedID string, isOpen bool, err error) {
	feed, err := c.fetchPriceFeedBySymbol(ctx, symbol)
	if err != nil {
		return "", false, err
	}

	if cachedID, ok := lookupFeedID(symbol); ok && cachedID != "" {
		return cachedID, feed.MarketHours.IsOpen, nil
	}

	registerFeedID(symbol, feed.ID)
	return feed.ID, feed.MarketHours.IsOpen, nil
}

func (c *HermesClient) fetchPriceFeedBySymbol(ctx context.Context, symbol string) (priceFeedResponse, error) {
	query := EquityQuerySymbol(symbol)
	endpoint := fmt.Sprintf("%s/v2/price_feeds?query=%s", c.baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return priceFeedResponse{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return priceFeedResponse{}, fmt.Errorf("pyth feed lookup %s: %w", symbol, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return priceFeedResponse{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return priceFeedResponse{}, fmt.Errorf("pyth feed lookup %s: status %d", symbol, resp.StatusCode)
	}

	var feeds []priceFeedResponse
	if err := json.Unmarshal(body, &feeds); err != nil {
		return priceFeedResponse{}, fmt.Errorf("decode pyth feed lookup %s: %w", symbol, err)
	}
	if len(feeds) == 0 || feeds[0].ID == "" {
		return priceFeedResponse{}, fmt.Errorf("pyth feed not found for %s", symbol)
	}
	return feeds[0], nil
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
		return parsedPriceUpdate{}, fmt.Errorf("pyth latest price: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return parsedPriceUpdate{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return parsedPriceUpdate{}, fmt.Errorf("pyth latest price: status %d", resp.StatusCode)
	}

	var payload latestPriceResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return parsedPriceUpdate{}, fmt.Errorf("decode pyth latest price: %w", err)
	}
	if len(payload.Parsed) == 0 {
		return parsedPriceUpdate{}, fmt.Errorf("pyth latest price: empty parsed payload")
	}
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
		n = n*10 + int64(ch-'0')
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
