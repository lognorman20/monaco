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
	LogoURL        string
	UnderlyingID   string
	Issuer         string
	IssuerName     string

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

// CatalogSearcher searches the xStocks catalog and resolves Solana mints.
type CatalogSearcher interface {
	MintCatalog
	Search(ctx context.Context, query string, limit, offset int) (CatalogSearchPage, error)
}

// HTTPCatalogSearcher lists assets from the xStocks public API and filters locally.
type HTTPCatalogSearcher struct {
	baseURL     string
	httpClient  *http.Client
	mintIndex   mintIndex
	routability RoutabilityProber

	catalogMu    sync.Mutex
	catalogRows  []CatalogAsset
	catalogUntil time.Time
}

// NewHTTPCatalogSearcher returns a catalog searcher backed by the production API.
func NewHTTPCatalogSearcher() *HTTPCatalogSearcher {
	return &HTTPCatalogSearcher{
		baseURL: defaultBaseURL,
		httpClient: telemetry.InstrumentClient(telemetry.UpstreamXStocks, &http.Client{
			Timeout: defaultTimeout,
		}),
		mintIndex: mintIndex{byMint: make(map[string]CatalogAsset)},
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
		mintIndex:  mintIndex{byMint: make(map[string]CatalogAsset)},
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

// catalogAssetNode mirrors xStocks public API asset rows. As of 2026-03 the API exposes
// symbol, name, and chain deployments (Solana mint) only — no volume, holder count, or
// popularity fields; pinned symbols and Jupiter routability probes supply ranking signals.
type catalogAssetNode struct {
	Symbol      string       `json:"symbol"`
	Name        string       `json:"name"`
	Deployments []deployment `json:"deployments"`
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
		asset := CatalogAssetFromXStockNode(node.Symbol, node.Name, mint)
		return &asset, nil
	}
	return nil, nil
}

const catalogPageBatch = 6

func (s *HTTPCatalogSearcher) searchPaginatedList(ctx context.Context, query string, limit, offset int) (CatalogSearchPage, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if rows, ok := s.cachedCatalog(); ok {
		return pageFilteredAssets(ctx, s.routability, rows, needle, limit, offset), nil
	}

	first, hasNext, err := s.fetchCatalogPage(ctx, 0)
	if err != nil {
		return CatalogSearchPage{}, err
	}
	if needle != "" && hasNext && len(filterCatalogAssets(first, needle)) >= offset+limit {
		page := pageFilteredAssets(ctx, s.routability, first, needle, limit, offset)
		page.HasMore = true
		return page, nil
	}
	if !hasNext {
		s.storeCatalog(first)
		return pageFilteredAssets(ctx, s.routability, first, needle, limit, offset), nil
	}

	rest, err := s.fetchRemainingCatalog(ctx, 1)
	if err != nil {
		return CatalogSearchPage{}, err
	}
	rows := append(first, rest...)
	s.storeCatalog(rows)
	return pageFilteredAssets(ctx, s.routability, rows, needle, limit, offset), nil
}

func (s *HTTPCatalogSearcher) cachedCatalog() ([]CatalogAsset, bool) {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	if len(s.catalogRows) == 0 || time.Now().After(s.catalogUntil) {
		return nil, false
	}
	return append([]CatalogAsset(nil), s.catalogRows...), true
}

func (s *HTTPCatalogSearcher) storeCatalog(rows []CatalogAsset) {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	s.catalogRows = append([]CatalogAsset(nil), rows...)
	s.catalogUntil = time.Now().Add(5 * time.Minute)
}

func (s *HTTPCatalogSearcher) fetchRemainingCatalog(ctx context.Context, start int) ([]CatalogAsset, error) {
	var all []CatalogAsset
	for page := start; page < start+48; page += catalogPageBatch {
		batch := catalogPageBatch
		type pageResult struct {
			assets  []CatalogAsset
			hasNext bool
			missing bool
			err     error
		}
		results := make([]pageResult, batch)
		var wg sync.WaitGroup
		for i := 0; i < batch; i++ {
			wg.Add(1)
			go func(i, page int) {
				defer wg.Done()
				assets, hasNext, err := s.fetchCatalogPage(ctx, page)
				if errors.Is(err, ErrNotFound) {
					results[i].missing = true
					return
				}
				results[i] = pageResult{assets: assets, hasNext: hasNext, err: err}
			}(i, page+i)
		}
		wg.Wait()

		stop := false
		for i := 0; i < batch; i++ {
			if results[i].err != nil {
				return nil, results[i].err
			}
			if results[i].missing {
				stop = true
				break
			}
			all = append(all, results[i].assets...)
			if !results[i].hasNext {
				stop = true
				break
			}
		}
		if stop {
			break
		}
	}
	return all, nil
}

func (s *HTTPCatalogSearcher) fetchCatalogPage(ctx context.Context, page int) ([]CatalogAsset, bool, error) {
	body, err := s.fetchCatalogListPage(ctx, page)
	if err != nil {
		return nil, false, err
	}
	var list catalogListResponse
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	assets := make([]CatalogAsset, 0, len(list.Nodes))
	for _, node := range list.Nodes {
		mint, err := solanaMintFromDeployments(node.Deployments)
		if err != nil {
			continue
		}
		assets = append(assets, CatalogAssetFromXStockNode(node.Symbol, node.Name, mint))
	}
	return assets, list.Page.HasNextPage, nil
}

func filterCatalogAssets(assets []CatalogAsset, needle string) []CatalogAsset {
	if needle == "" {
		return append([]CatalogAsset(nil), assets...)
	}
	matches := make([]CatalogAsset, 0)
	for _, asset := range assets {
		if catalogNodeMatches(catalogAssetNode{Symbol: asset.Symbol, Name: asset.Name}, needle) {
			matches = append(matches, asset)
		}
	}
	return matches
}

func pageFilteredAssets(ctx context.Context, prober RoutabilityProber, assets []CatalogAsset, needle string, limit, offset int) CatalogSearchPage {
	matches := filterCatalogAssets(assets, needle)
	rankCatalogAssets(ctx, prober, matches)
	hasMore := len(matches) > offset+limit
	if offset >= len(matches) {
		return CatalogSearchPage{Assets: nil, HasMore: hasMore}
	}
	end := offset + limit
	if end > len(matches) {
		end = len(matches)
	}
	return CatalogSearchPage{Assets: matches[offset:end], HasMore: hasMore}
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

// catalogAssetsFromListResponse parses one catalog list page JSON and filters by query.
func catalogAssetsFromListResponse(body []byte, query string) ([]CatalogAsset, error) {
	var list catalogListResponse
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}

	needle := strings.ToLower(strings.TrimSpace(query))
	assets := make([]CatalogAsset, 0)
	for _, node := range list.Nodes {
		if !catalogNodeMatches(node, needle) {
			continue
		}
		mint, err := solanaMintFromDeployments(node.Deployments)
		if err != nil {
			continue
		}
		assets = append(assets, CatalogAssetFromXStockNode(node.Symbol, node.Name, mint))
	}
	return assets, nil
}

func catalogNodeMatches(node catalogAssetNode, needle string) bool {
	symbol := strings.ToLower(strings.TrimSpace(node.Symbol))
	name := strings.ToLower(strings.TrimSpace(node.Name))
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
