package pyth

import (
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/config"
)

// NewHermesClientFromConfig returns an authenticated Hermes client. It errors
// without PYTH_API_KEY, because every Hermes request is authenticated.
func NewHermesClientFromConfig(cfg *config.Config) (*HermesClient, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.PythAPIKey == "" {
		return nil, fmt.Errorf("PYTH_API_KEY is required for Pyth Hermes")
	}
	return NewHermesClientWithBaseURL(cfg.PythHermesBaseURL, cfg.PythAPIKey), nil
}

// NewMarketDataClientFromConfig returns the stock screen's Pyth client whatever
// PYTH_API_KEY is. Chart history comes from Pyth Benchmarks, a public endpoint
// that takes no key, so it is always wired. Hermes (the per-sample chart fallback
// and the equity reference price) needs the key; without one those short-circuit
// to "unavailable" instead of sending requests that would all be refused.
func NewMarketDataClientFromConfig(cfg *config.Config) *HermesClient {
	var hermesBaseURL, apiKey, benchmarksBaseURL string
	if cfg != nil {
		hermesBaseURL, apiKey, benchmarksBaseURL = cfg.PythHermesBaseURL, cfg.PythAPIKey, cfg.PythBenchmarksBaseURL
	}
	return NewHermesClientWithBaseURL(hermesBaseURL, apiKey).
		WithSeriesSource(NewBenchmarksClientWithHTTP(benchmarksBaseURL, nil))
}
