package xstocks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// CatalogAsset is a backend-resolved xStock catalog row for mobile search.
type CatalogAsset struct {
	Symbol     string
	Name       string
	SolanaMint string
	Routable   bool
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
	Browse(ctx context.Context, limit, offset int) (CatalogSearchPage, error)
}

// HTTPCatalogSearcher lists assets from the xStocks public API and filters locally.
type HTTPCatalogSearcher struct {
	baseURL     string
	httpClient  *http.Client
	mintIndex   mintIndex
	routability RoutabilityProber
}

// NewHTTPCatalogSearcher returns a catalog searcher backed by the production API.
func NewHTTPCatalogSearcher() *HTTPCatalogSearcher {
	return &HTTPCatalogSearcher{
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
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
		httpClient: httpClient,
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

	if offset == 0 && query != "" && looksLikeTickerQuery(query) {
		if asset, err := s.searchBySymbol(ctx, query); err != nil {
			if !isContextCanceled(err) {
				logCatalogSearch(query, 0, err)
			}
			return CatalogSearchPage{}, err
		} else if asset != nil {
			matches := []CatalogAsset{*asset}
			rankCatalogAssets(ctx, s.routability, matches)
			logCatalogSearch(query, 1, nil)
			return CatalogSearchPage{Assets: matches, HasMore: false}, nil
		}
	}

	page, err := s.searchFromIndex(ctx, query, limit, offset, false)
	if err != nil {
		if !isContextCanceled(err) {
			logCatalogSearch(query, 0, err)
		}
		return CatalogSearchPage{}, err
	}
	logCatalogSearch(query, len(page.Assets), nil)
	return page, nil
}

// Browse returns a paginated market catalog for the Assets tab, omitting the popular strip.
func (s *HTTPCatalogSearcher) Browse(ctx context.Context, limit, offset int) (CatalogSearchPage, error) {
	if limit <= 0 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	page, err := s.searchFromIndex(ctx, "", limit, offset, true)
	if err != nil {
		if !isContextCanceled(err) {
			logCatalogSearch("", 0, err)
		}
		return CatalogSearchPage{}, err
	}
	logCatalogSearch("", len(page.Assets), nil)
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
		return &CatalogAsset{
			Symbol:     strings.TrimSpace(node.Symbol),
			Name:       strings.TrimSpace(node.Name),
			SolanaMint: mint,
		}, nil
	}
	return nil, nil
}

func (s *HTTPCatalogSearcher) searchFromIndex(ctx context.Context, query string, limit, offset int, excludePopularStrip bool) (CatalogSearchPage, error) {
	if err := s.ensureMintIndex(ctx); err != nil {
		return CatalogSearchPage{}, err
	}

	needle := strings.ToLower(strings.TrimSpace(query))
	s.mintIndex.mu.RLock()
	all := append([]CatalogAsset(nil), s.mintIndex.allAssets...)
	s.mintIndex.mu.RUnlock()

	popularSet := popularStripSymbolSet()
	matches := make([]CatalogAsset, 0, len(all))
	for _, asset := range all {
		if needle == "" {
			if excludePopularStrip {
				if _, pinned := popularSet[strings.ToUpper(strings.TrimSpace(asset.Symbol))]; pinned {
					continue
				}
			}
			matches = append(matches, asset)
			continue
		}
		if catalogAssetMatches(asset, needle) {
			matches = append(matches, asset)
		}
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

func (f *fakeCatalogSearcher) Browse(ctx context.Context, limit, offset int) (CatalogSearchPage, error) {
	return f.Search(ctx, "", limit, offset)
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
	return true
}

func catalogAssetMatches(asset CatalogAsset, needle string) bool {
	if needle == "" {
		return true
	}
	symbol := strings.ToLower(strings.TrimSpace(asset.Symbol))
	name := strings.ToLower(catalogDisplayName(asset.Name))
	return strings.Contains(symbol, needle) || strings.Contains(name, needle)
}

// CatalogDisplayName strips xStocks branding from catalog asset names.
func CatalogDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	for _, suffix := range []string{" xStock", " xStocks", " xstock", " xstocks"} {
		if strings.HasSuffix(strings.ToLower(name), suffix) {
			name = strings.TrimSpace(name[:len(name)-len(suffix)])
			break
		}
	}
	return name
}

func catalogDisplayName(name string) string {
	return CatalogDisplayName(name)
}

func isContextCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
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
		assets = append(assets, CatalogAsset{
			Symbol:     strings.TrimSpace(node.Symbol),
			Name:       strings.TrimSpace(node.Name),
			SolanaMint: mint,
		})
	}
	return assets, nil
}

func catalogNodeMatches(node catalogAssetNode, needle string) bool {
	symbol := strings.ToLower(strings.TrimSpace(node.Symbol))
	name := strings.ToLower(catalogDisplayName(node.Name))
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
		if needle == "" || catalogAssetMatches(asset, needle) {
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
