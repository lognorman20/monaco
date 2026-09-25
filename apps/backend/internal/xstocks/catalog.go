package xstocks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// AssetKind classifies catalog rows (listed stock vs pre-IPO token).
type AssetKind string

const (
	AssetKindStock  AssetKind = "stock"
	AssetKindPreIPO AssetKind = "pre_ipo"
)

// AssetSource identifies which catalog API populated a row.
type AssetSource string

const (
	AssetSourceXStocks   AssetSource = "xstocks"
	AssetSourceTessera   AssetSource = "tessera"
	AssetSourcePreStocks AssetSource = "prestocks"
)

// CatalogAsset is a backend-resolved catalog row for mobile search.
type CatalogAsset struct {
	Symbol     string
	Name       string
	SolanaMint string
	Routable   bool

	Kind           AssetKind
	Source         AssetSource
	Decimals       int
	TransferFeeBps int
	Sector         string
	// LogoURL is the company logo the catalogue publishes for this asset. Empty
	// when the catalogue has none, in which case a row draws its ticker tile.
	LogoURL      string
	UnderlyingID string
	Issuer       string
	IssuerName   string

	UiAmountMultiplier *big.Rat // nil = unresolved
	Paused             bool

	ReferenceMarkUsdcMicros *int64
	ReferenceValuationUsd   *int64
	ReferenceUpdatedAt      *time.Time
	ReferenceSource         string
	Holders                 *int

	LiquidityUsd int64
}

// Normalize applies stock/xStocks defaults for unset kind, source, and decimals.
func (a CatalogAsset) Normalize() CatalogAsset {
	if a.Kind == "" {
		a.Kind = AssetKindStock
	}
	if a.Decimals == 0 {
		a.Decimals = jupiter.XStockDecimals
	}
	if a.Source == "" {
		a.Source = AssetSourceXStocks
	}
	return a
}

// AtomicScale returns 10^Decimals for this asset.
func (a CatalogAsset) AtomicScale() int64 {
	return jupiter.AtomicScale(a.Normalize().Decimals)
}

// CatalogAssetFromXStockNode builds a normalized xStocks catalog row.
func CatalogAssetFromXStockNode(symbol, name, solanaMint string) CatalogAsset {
	symbol = strings.TrimSpace(symbol)
	return CatalogAsset{
		Symbol:       symbol,
		Name:         strings.TrimSpace(name),
		SolanaMint:   strings.TrimSpace(solanaMint),
		Kind:         AssetKindStock,
		Source:       AssetSourceXStocks,
		Decimals:     jupiter.XStockDecimals,
		Issuer:       string(AssetSourceXStocks),
		UnderlyingID: UnderlyingIDFromXStockSymbol(symbol),
	}.Normalize()
}

// UnderlyingIDFromXStockSymbol derives the company slug from an xStock ticker.
func UnderlyingIDFromXStockSymbol(symbol string) string {
	s := strings.ToLower(strings.TrimSpace(symbol))
	return strings.TrimSuffix(s, "x")
}

// CatalogSearchPage is one page of catalog search results.
type CatalogSearchPage struct {
	Assets  []CatalogAsset
	HasMore bool
}

// CatalogSearcher searches the xStocks catalog and resolves Solana mints and
// tickers.
type CatalogSearcher interface {
	MintCatalog
	SymbolCatalog
	Search(ctx context.Context, query string, limit, offset int) (CatalogSearchPage, error)
}

// HTTPCatalogSearcher lists assets from the xStocks public API and filters locally.
type HTTPCatalogSearcher struct {
	baseURL     string
	httpClient  *http.Client
	catalog     catalogIndex
	routability RoutabilityProber
}

// NewHTTPCatalogSearcher returns a catalog searcher backed by the production API.
func NewHTTPCatalogSearcher() *HTTPCatalogSearcher {
	return &HTTPCatalogSearcher{
		baseURL: defaultBaseURL,
		httpClient: telemetry.InstrumentClient(telemetry.UpstreamXStocks, &http.Client{
			Timeout: defaultTimeout,
		}),
	}
}

// SetRoutabilityProber configures Jupiter routability ranking for catalog search results.
func (s *HTTPCatalogSearcher) SetRoutabilityProber(prober RoutabilityProber) {
	s.routability = prober
}

// NewHTTPCatalogSearcherWithClient injects a custom base URL and HTTP client for tests.
func NewHTTPCatalogSearcherWithClient(baseURL string, httpClient *http.Client) *HTTPCatalogSearcher {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &HTTPCatalogSearcher{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: telemetry.InstrumentClient(telemetry.UpstreamXStocks, httpClient),
	}
}

const browserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/127.0.0.0 Safari/537.36"

type catalogListResponse struct {
	Nodes []catalogAssetNode `json:"nodes"`
	Page  catalogPageInfo    `json:"page"`
}

type catalogPageInfo struct {
	CurrentPage int  `json:"currentPage"`
	HasNextPage bool `json:"hasNextPage"`
}

// catalogAssetNode mirrors xStocks public API asset rows. The API exposes symbol,
// name, a logo URL, and chain deployments (Solana mint) — no volume, holder count,
// or popularity fields; pinned symbols and Jupiter routability probes supply ranking
// signals.
type catalogAssetNode struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
	// Logo is the catalogue's own artwork for the token, e.g.
	// https://xstocks-metadata.backed.fi/logos/tokens/AAPLx.png. It is the only
	// logo source we have, and it is already on every payload this package fetches.
	Logo        string       `json:"logo"`
	Deployments []deployment `json:"deployments"`
}

// catalogAssetFromNode is the one place a catalogue payload becomes a CatalogAsset,
// so a field added here reaches every path that resolves an asset — search by
// ticker, the paginated filter, and the index — rather than two of the three.
func catalogAssetFromNode(node catalogAssetNode, mint string) CatalogAsset {
	asset := CatalogAssetFromXStockNode(node.Symbol, node.Name, mint)
	asset.LogoURL = normalizeLogoURL(node.Logo)
	return asset
}

// normalizeLogoURL keeps only a logo the app can actually load. A relative path or
// a plain-HTTP URL would be a broken image on an ATS-enforcing client, and an empty
// string is what the row already knows how to fall back from.
func normalizeLogoURL(raw string) string {
	logo := strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(logo), "https://") {
		return ""
	}
	return logo
}

// Search returns one page of catalog assets whose symbol or name matches query.
func (s *HTTPCatalogSearcher) Search(ctx context.Context, query string, limit, offset int) (CatalogSearchPage, error) {
	query = strings.TrimSpace(query)
	if limit <= 0 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}

	if offset == 0 && looksLikeTickerQuery(query) {
		if asset, err := s.searchBySymbol(ctx, query); err != nil {
			logCatalogSearch(query, 0, err)
			return CatalogSearchPage{}, err
		} else if asset != nil {
			matches := []CatalogAsset{*asset}
			rankCatalogAssets(ctx, s.routability, matches)
			logCatalogSearch(query, 1, nil)
			return CatalogSearchPage{Assets: matches, HasMore: false}, nil
		}
	}

	page, err := s.searchPaginatedList(ctx, query, limit, offset)
	if err != nil {
		logCatalogSearch(query, 0, err)
		return CatalogSearchPage{}, err
	}
	logCatalogSearch(query, len(page.Assets), nil)
	return page, nil
}

func (s *HTTPCatalogSearcher) searchBySymbol(ctx context.Context, query string) (*CatalogAsset, error) {
	needle := strings.ToLower(query)
	for _, symbol := range xStockSymbolCandidates(query) {
		node, err := s.fetchCatalogAssetNode(ctx, symbol)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		if !catalogNodeMatches(*node, needle) {
			continue
		}
		mint, err := solanaMintFromDeployments(node.Deployments)
		if err != nil {
			continue
		}
		asset := catalogAssetFromNode(*node, mint)
		return &asset, nil
	}
	return nil, nil
}

// searchPaginatedList filters the catalogue index rather than re-walking every
// catalogue page.
//
// It used to crawl the whole catalogue per call, which made every query — and
// every pinned symbol the popular list resolves, and every symbol a cabal holds —
// its own full crawl of a third-party API. The catalogue is one small list that
// changes when a new xStock is listed, so it is crawled once and searched in
// memory.
func (s *HTTPCatalogSearcher) searchPaginatedList(ctx context.Context, query string, limit, offset int) (CatalogSearchPage, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	entries, err := s.index(ctx)
	if err != nil {
		return CatalogSearchPage{}, err
	}

	matches := make([]CatalogAsset, 0)
	for _, asset := range entries.all {
		if !catalogAssetMatches(asset, needle) {
			continue
		}
		matches = append(matches, asset)
	}

	rankCatalogAssets(ctx, s.routability, matches)

	hasMore := len(matches) > offset+limit
	if offset >= len(matches) {
		return CatalogSearchPage{Assets: nil, HasMore: hasMore}, nil
	}
	end := offset + limit
	if end > len(matches) {
		end = len(matches)
	}
	return CatalogSearchPage{
		Assets:  matches[offset:end],
		HasMore: hasMore,
	}, nil
}

func (s *HTTPCatalogSearcher) fetchCatalogListPage(ctx context.Context, page int) ([]byte, error) {
	endpoint, err := url.JoinPath(s.baseURL, assetsPath)
	if err != nil {
		return nil, err
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	query := parsed.Query()
	query.Set("page", strconv.Itoa(page))
	parsed.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	if page > 0 {
		req.Header.Set("User-Agent", browserUserAgent)
	}

	return s.doGET(req)
}

func (s *HTTPCatalogSearcher) fetchCatalogAssetNode(ctx context.Context, symbol string) (*catalogAssetNode, error) {
	endpoint, err := url.JoinPath(s.baseURL, assetsPath, url.PathEscape(symbol))
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	body, err := s.doGET(req)
	if err != nil {
		return nil, err
	}

	var node catalogAssetNode
	if err := json.Unmarshal(body, &node); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return &node, nil
}

func (s *HTTPCatalogSearcher) doGET(req *http.Request) ([]byte, error) {
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrInvalidResponse, resp.StatusCode)
	}
	return body, nil
}

func looksLikeTickerQuery(query string) bool {
	q := strings.TrimSpace(query)
	if q == "" || len(q) > 10 {
		return false
	}
	for _, r := range q {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	if strings.HasSuffix(strings.ToLower(q), "x") {
		return true
	}
	return q == strings.ToUpper(q) && unicode.IsLetter(rune(q[0]))
}

func xStockSymbolCandidates(query string) []string {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	if strings.HasSuffix(strings.ToLower(q), "x") {
		return []string{q}
	}
	upper := strings.ToUpper(q)
	return []string{upper + "x", q + "x"}
}

func catalogNodeMatches(node catalogAssetNode, needle string) bool {
	symbol := strings.ToLower(strings.TrimSpace(node.Symbol))
	name := strings.ToLower(strings.TrimSpace(node.Name))
	return strings.Contains(symbol, needle) || strings.Contains(name, needle)
}

func catalogAssetMatches(asset CatalogAsset, needle string) bool {
	symbol := strings.ToLower(strings.TrimSpace(asset.Symbol))
	name := strings.ToLower(strings.TrimSpace(asset.Name))
	return strings.Contains(symbol, needle) || strings.Contains(name, needle)
}

type fakeCatalogSearcher struct {
	mu          sync.Mutex
	assets      []CatalogAsset
	listErr     error
	routability RoutabilityProber
}

// NewFakeCatalogSearcher returns an in-memory catalog searcher for tests.
func NewFakeCatalogSearcher() CatalogSearcher {
	return &fakeCatalogSearcher{
		assets: make([]CatalogAsset, 0),
	}
}

// RegisterCatalogAsset adds a searchable asset to the fake catalog searcher.
func RegisterCatalogAsset(searcher CatalogSearcher, asset CatalogAsset) {
	fake, ok := searcher.(*fakeCatalogSearcher)
	if !ok {
		panic("xstocks: RegisterCatalogAsset requires NewFakeCatalogSearcher")
	}
	fake.mu.Lock()
	fake.assets = append(fake.assets, asset)
	fake.mu.Unlock()
}

// SetFakeCatalogRoutabilityProber configures routability ranking on a fake catalog searcher.
func SetFakeCatalogRoutabilityProber(searcher CatalogSearcher, prober RoutabilityProber) {
	fake, ok := searcher.(*fakeCatalogSearcher)
	if !ok {
		panic("xstocks: SetFakeCatalogRoutabilityProber requires NewFakeCatalogSearcher")
	}
	fake.mu.Lock()
	fake.routability = prober
	fake.mu.Unlock()
}

// RegisterCatalogSearchError forces Search to return err on the fake catalog searcher.
func RegisterCatalogSearchError(searcher CatalogSearcher, err error) {
	fake, ok := searcher.(*fakeCatalogSearcher)
	if !ok {
		panic("xstocks: RegisterCatalogSearchError requires NewFakeCatalogSearcher")
	}
	fake.mu.Lock()
	fake.listErr = err
	fake.mu.Unlock()
}

func (f *fakeCatalogSearcher) Search(ctx context.Context, query string, limit, offset int) (CatalogSearchPage, error) {
	_ = ctx
	query = strings.TrimSpace(query)
	if limit <= 0 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}

	f.mu.Lock()
	if f.listErr != nil {
		err := f.listErr
		f.mu.Unlock()
		return CatalogSearchPage{}, err
	}
	assets := append([]CatalogAsset(nil), f.assets...)
	prober := f.routability
	f.mu.Unlock()

	needle := strings.ToLower(query)
	matches := make([]CatalogAsset, 0, len(assets))
	for _, asset := range assets {
		symbol := strings.ToLower(strings.TrimSpace(asset.Symbol))
		name := strings.ToLower(strings.TrimSpace(asset.Name))
		if needle == "" || strings.Contains(symbol, needle) || strings.Contains(name, needle) {
			matches = append(matches, asset)
		}
	}

	rankCatalogAssets(ctx, prober, matches)

	hasMore := len(matches) > offset+limit
	if offset >= len(matches) {
		return CatalogSearchPage{Assets: nil, HasMore: hasMore}, nil
	}
	end := offset + limit
	if end > len(matches) {
		end = len(matches)
	}
	return CatalogSearchPage{
		Assets:  matches[offset:end],
		HasMore: hasMore,
	}, nil
}
