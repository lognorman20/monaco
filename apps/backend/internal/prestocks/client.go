package prestocks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/telemetry"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const (
	defaultBaseURL   = "https://prestocks.com"
	defaultTimeout   = 15 * time.Second
	listCacheTTL     = 60 * time.Second
	lastGoodTTL      = 24 * time.Hour
	warnInterval     = 5 * time.Minute
	userAgent        = "monaco-backend/1"
	prestocksAPIPath = "/api/prestocks"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// HTTPCatalog fetches the PreStocks public catalog with caching and fallback.
type HTTPCatalog struct {
	baseURL    string
	httpClient *http.Client

	mu           sync.Mutex
	listCache    []xstocks.CatalogAsset
	listCachedAt time.Time
	lastGood     []xstocks.CatalogAsset
	lastGoodAt   time.Time
	lastWarnAt   time.Time
	now          func() time.Time
}

// NewHTTPCatalog returns a catalog client for the production PreStocks API.
func NewHTTPCatalog() *HTTPCatalog {
	return NewHTTPCatalogWithClient(defaultBaseURL, nil)
}

// NewHTTPCatalogWithClient injects base URL and HTTP client for tests.
func NewHTTPCatalogWithClient(baseURL string, httpClient *http.Client) *HTTPCatalog {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &HTTPCatalog{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: telemetry.InstrumentClient(telemetry.UpstreamPreStocks, httpClient),
		now:        time.Now,
	}
}

// List returns PreStocks catalog assets, using cache and fallbacks on failure.
func (c *HTTPCatalog) List(ctx context.Context) ([]xstocks.CatalogAsset, error) {
	now := c.now()

	c.mu.Lock()
	if len(c.listCache) > 0 && now.Sub(c.listCachedAt) < listCacheTTL {
		out := append([]xstocks.CatalogAsset(nil), c.listCache...)
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	assets, err := c.fetchList(ctx)
	if err == nil {
		c.mu.Lock()
		c.listCache = append([]xstocks.CatalogAsset(nil), assets...)
		c.listCachedAt = now
		c.lastGood = append([]xstocks.CatalogAsset(nil), assets...)
		c.lastGoodAt = now
		c.mu.Unlock()
		return append([]xstocks.CatalogAsset(nil), assets...), nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.lastGood) > 0 && now.Sub(c.lastGoodAt) < lastGoodTTL {
		return append([]xstocks.CatalogAsset(nil), c.lastGood...), nil
	}
	c.warnUpstreamFailure(err)
	return StaticFallback(), nil
}

func (c *HTTPCatalog) warnUpstreamFailure(err error) {
	now := c.now()
	if !c.lastWarnAt.IsZero() && now.Sub(c.lastWarnAt) < warnInterval {
		return
	}
	c.lastWarnAt = now
	slog.Warn("prestocks catalog list failed; using static fallback", "err", err)
}

func (c *HTTPCatalog) fetchList(ctx context.Context) ([]xstocks.CatalogAsset, error) {
	endpoint, err := url.JoinPath(c.baseURL, prestocksAPIPath)
	if err != nil {
		return nil, err
	}
	body, err := c.doGET(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var rows []apiRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("prestocks catalog: %w", err)
	}
	return catalogAssetsFromRows(rows), nil
}

func (c *HTTPCatalog) doGET(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prestocks GET %s: status %d", endpoint, resp.StatusCode)
	}
	return body, nil
}

func isValidSolanaMint(mint string) bool {
	n := len(mint)
	if n < 32 || n > 44 {
		return false
	}
	for i := 0; i < n; i++ {
		if !strings.ContainsRune(base58Alphabet, rune(mint[i])) {
			return false
		}
	}
	return true
}
