package jupiter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
)

const (
	defaultPriceBaseURL = "https://api.jup.ag/price/v3"
	maxPriceIDsPerBatch = 50
	maxUsdPrice         = 1e9
)

// TokenPrice is a mint's current USD mark and 24h change, converted for display.
// Change24h is a decimal ratio string (e.g. "0.025000" for +2.5%), matching the
// pyth.AssetMark contract so it drops into the same response shape.
// LiquidityUsd is the pooled liquidity behind the mark; valuation callers use it to
// refuse a price that a thin pool could be pushed around to produce.
type TokenPrice struct {
	PriceUsdcMicros int64
	Change24h       *string
	LiquidityUsd    float64
}

// PriceClient batches current USD marks by Solana mint via Jupiter's Price API.
// Distinct from Client (Swap API v2): this is a read-only price feed, not a quote
// simulation, so it doesn't fan out one request per asset or risk swap rate limits.
type PriceClient interface {
	Prices(ctx context.Context, mints []string) (map[string]TokenPrice, error)
}

// HTTPPriceClient calls Jupiter's Price API v3 on Solana mainnet.
type HTTPPriceClient struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
}

// NewHTTPPriceClient returns a production price client. apiKey may be empty —
// the API serves unauthenticated requests at a lower rate limit.
func NewHTTPPriceClient(apiKey string) *HTTPPriceClient {
	return NewHTTPPriceClientWithBaseURL(defaultPriceBaseURL, nil, apiKey)
}

// NewHTTPPriceClientWithBaseURL injects a custom base URL and HTTP client for tests.
func NewHTTPPriceClientWithBaseURL(baseURL string, httpClient *http.Client, apiKey string) *HTTPPriceClient {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultPriceBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &HTTPPriceClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		apiKey:     strings.TrimSpace(apiKey),
	}
}

// Prices returns current marks for mints, batching in groups of 50 (the API max
// per request). Missing mints (no Jupiter-quoted price) are simply absent from
// the result map rather than erroring the whole call.
func (c *HTTPPriceClient) Prices(ctx context.Context, mints []string) (map[string]TokenPrice, error) {
	out := make(map[string]TokenPrice, len(mints))
	if len(mints) == 0 {
		return out, nil
	}
	for start := 0; start < len(mints); start += maxPriceIDsPerBatch {
		end := start + maxPriceIDsPerBatch
		if end > len(mints) {
			end = len(mints)
		}
		batch, err := c.fetchBatch(ctx, mints[start:end])
		if err != nil {
			return nil, err
		}
		for mint, price := range batch {
			out[mint] = price
		}
	}
	return out, nil
}

type jupiterPriceEntry struct {
	UsdPrice       float64 `json:"usdPrice"`
	PriceChange24h float64 `json:"priceChange24h"`
	Liquidity      float64 `json:"liquidity"`
}

func (c *HTTPPriceClient) fetchBatch(ctx context.Context, mints []string) (map[string]TokenPrice, error) {
	endpoint := fmt.Sprintf("%s?ids=%s", c.baseURL, url.QueryEscape(strings.Join(mints, ",")))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		fetchErr := fmt.Errorf("jupiter price fetch: %w", err)
		logPriceFetch(len(mints), 0, fetchErr)
		return nil, fetchErr
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logPriceFetch(len(mints), resp.StatusCode, err)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		fetchErr := fmt.Errorf("jupiter price fetch: status %d", resp.StatusCode)
		logPriceFetch(len(mints), resp.StatusCode, fetchErr)
		return nil, fetchErr
	}

	var payload map[string]jupiterPriceEntry
	if err := json.Unmarshal(body, &payload); err != nil {
		decodeErr := fmt.Errorf("decode jupiter price: %w", err)
		logPriceFetch(len(mints), resp.StatusCode, decodeErr)
		return nil, decodeErr
	}

	out := make(map[string]TokenPrice, len(payload))
	for mint, entry := range payload {
		out[mint] = entry.toTokenPrice()
	}
	logPriceFetch(len(mints), resp.StatusCode, nil)
	return out, nil
}

func (e jupiterPriceEntry) toTokenPrice() TokenPrice {
	// An out-of-range usdPrice would overflow the int64 conversion into a plausible-looking
	// number; report it as 0 (no usable price) instead.
	var micros int64
	if e.UsdPrice > 0 && e.UsdPrice < maxUsdPrice {
		micros = int64(math.Round(e.UsdPrice * 1_000_000))
	}
	// Jupiter reports a percentage (e.g. -0.27 for -0.27%); the response contract
	// here is a decimal ratio, matching pyth.AssetMark.Change24h.
	ratio := e.PriceChange24h / 100
	change := fmt.Sprintf("%.6f", ratio)
	return TokenPrice{
		PriceUsdcMicros: micros,
		Change24h:       &change,
		LiquidityUsd:    e.Liquidity,
	}
}
