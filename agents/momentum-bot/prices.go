package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Jupiter's keyless host for the Price API v3, the same API the Monaco backend
// prices its asset list with.
const defaultPriceURL = "https://lite-api.jup.ag/price/v3"

// PriceClient reads current USD prices by Solana mint.
type PriceClient struct {
	baseURL string
	http    *http.Client
}

// NewPriceClient returns a Jupiter price client. httpClient may be nil.
func NewPriceClient(baseURL string, httpClient *http.Client) *PriceClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultPriceURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &PriceClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), http: httpClient}
}

// Prices returns USD per share keyed by mint. Mints Jupiter has no price for are
// absent from the map. The bot watches a handful of symbols, well under the API's
// 50-id batch limit.
func (c *PriceClient) Prices(ctx context.Context, mints []string) (map[string]float64, error) {
	endpoint := fmt.Sprintf("%s?ids=%s", c.baseURL, url.QueryEscape(strings.Join(mints, ",")))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jupiter price: %w", redactURLError(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("jupiter price: read body: %w", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &ThrottledError{RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jupiter price: status %d", resp.StatusCode)
	}

	var payload map[string]struct {
		UsdPrice float64 `json:"usdPrice"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("jupiter price: decode: %w", err)
	}
	out := make(map[string]float64, len(payload))
	for mint, entry := range payload {
		if entry.UsdPrice > 0 {
			out[mint] = entry.UsdPrice
		}
	}
	return out, nil
}
