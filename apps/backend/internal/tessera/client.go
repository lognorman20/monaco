package tessera

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/telemetry"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const (
	defaultBaseURL       = "https://rest-api.tessera.pe"
	defaultTimeout       = 15 * time.Second
	listCacheTTL         = 60 * time.Second
	lastGoodTTL          = 24 * time.Hour
	logoCacheTTL         = 24 * time.Hour
	warnInterval         = 5 * time.Minute
	userAgent            = "monaco-backend/1"
	tokenDetailsPath     = "/v1/public/token-details"
	tokensPath           = "/v1/public/tokens"
	referenceIssuerStale = "issuer_stale"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// Catalog lists Tessera pre-IPO assets.
type Catalog interface {
	List(ctx context.Context) ([]xstocks.CatalogAsset, error)
}

// HTTPCatalog fetches Tessera public catalog APIs with caching and fallback.
type HTTPCatalog struct {
	baseURL    string
	httpClient *http.Client

	mu           sync.Mutex
	listCache    []xstocks.CatalogAsset
	listCachedAt time.Time
	lastGood     []xstocks.CatalogAsset
	lastGoodAt   time.Time
	logoByMint   map[string]logoCacheEntry
	lastWarnAt   time.Time
	now          func() time.Time
}

type logoCacheEntry struct {
	url       string
	fetchedAt time.Time
}

type tokenDetail struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Symbol        string  `json:"symbol"`
	Code          string  `json:"code"`
	Sector        string  `json:"sector"`
	Mint          string  `json:"mint"`
	MarkPrice     float64 `json:"markPrice"`
	Holders       int     `json:"holders"`
	MarkValuation int64   `json:"markValuation"`
}

type tokenRow struct {
	Token        string `json:"token"`
	LatestSupply string `json:"latest_supply"`
	Name         string `json:"name"`
	Symbol       string `json:"symbol"`
	URI          string `json:"uri"`
}

type tokenURIDocument struct {
	Image string `json:"image"`
}

// NewHTTPCatalog returns a catalog client for the production Tessera API.
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
		httpClient: telemetry.InstrumentClient(telemetry.UpstreamTessera, httpClient),
		logoByMint: make(map[string]logoCacheEntry),
		now:        time.Now,
	}
}

// List returns Tessera catalog assets, using cache and fallbacks on failure.
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
	slog.Warn("tessera catalog list failed; using static fallback", "err", err)
}

func (c *HTTPCatalog) fetchList(ctx context.Context) ([]xstocks.CatalogAsset, error) {
	details, err := c.fetchTokenDetails(ctx)
	if err != nil {
		return nil, err
	}
	uriByMint, err := c.fetchTokenURIs(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]xstocks.CatalogAsset, 0, len(details))
	for _, d := range details {
		asset, ok := c.buildAsset(ctx, d, uriByMint[d.Mint])
		if !ok {
			continue
		}
		out = append(out, asset)
	}
	return out, nil
}

func (c *HTTPCatalog) buildAsset(ctx context.Context, d tokenDetail, tokenURI string) (xstocks.CatalogAsset, bool) {
	code := strings.TrimSpace(d.Code)
	mint := strings.TrimSpace(d.Mint)
	if code == "" || !isValidSolanaMint(mint) {
		return xstocks.CatalogAsset{}, false
	}

	markMicros := int64(math.Round(d.MarkPrice * 1e6))
	valuation := d.MarkValuation
	holders := d.Holders

	asset := xstocks.CatalogAsset{
		Symbol:                  code,
		Name:                    strings.TrimSpace(d.Name),
		SolanaMint:              mint,
		Kind:                    xstocks.AssetKindPreIPO,
		Source:                  xstocks.AssetSourceTessera,
		Issuer:                  string(xstocks.AssetSourceTessera),
		UnderlyingID:            underlyingIDFromName(d.Name),
		Decimals:                tesseraDecimals,
		TransferFeeBps:          tesseraTransferFeeBps,
		Sector:                  strings.TrimSpace(d.Sector),
		ReferenceMarkUsdcMicros: &markMicros,
		ReferenceValuationUsd:   &valuation,
		ReferenceSource:         referenceIssuerStale,
		Holders:                 &holders,
		LogoURL:                 c.logoURL(ctx, mint, tokenURI),
	}
	return asset, true
}

func (c *HTTPCatalog) logoURL(ctx context.Context, mint, tokenURI string) string {
	tokenURI = strings.TrimSpace(tokenURI)
	if tokenURI == "" {
		return ""
	}

	now := c.now()
	c.mu.Lock()
	if entry, ok := c.logoByMint[mint]; ok && now.Sub(entry.fetchedAt) < logoCacheTTL {
		url := entry.url
		c.mu.Unlock()
		return url
	}
	c.mu.Unlock()

	image, err := c.fetchLogoImage(ctx, tokenURI)
	c.mu.Lock()
	c.logoByMint[mint] = logoCacheEntry{url: image, fetchedAt: now}
	c.mu.Unlock()
	if err != nil {
		return ""
	}
	return image
}

func (c *HTTPCatalog) fetchLogoImage(ctx context.Context, tokenURI string) (string, error) {
	body, err := c.doGET(ctx, tokenURI)
	if err != nil {
		return "", err
	}
	var doc tokenURIDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", err
	}
	return strings.TrimSpace(doc.Image), nil
}

func (c *HTTPCatalog) fetchTokenDetails(ctx context.Context) ([]tokenDetail, error) {
	endpoint, err := url.JoinPath(c.baseURL, tokenDetailsPath)
	if err != nil {
		return nil, err
	}
	body, err := c.doGET(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var details []tokenDetail
	if err := json.Unmarshal(body, &details); err != nil {
		return nil, fmt.Errorf("tessera token-details: %w", err)
	}
	return details, nil
}

func (c *HTTPCatalog) fetchTokenURIs(ctx context.Context) (map[string]string, error) {
	endpoint, err := url.JoinPath(c.baseURL, tokensPath)
	if err != nil {
		return nil, err
	}
	body, err := c.doGET(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var rows []tokenRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("tessera tokens: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		mint := strings.TrimSpace(row.Token)
		if mint == "" {
			continue
		}
		out[mint] = strings.TrimSpace(row.URI)
	}
	return out, nil
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
		return nil, fmt.Errorf("tessera GET %s: status %d", endpoint, resp.StatusCode)
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
