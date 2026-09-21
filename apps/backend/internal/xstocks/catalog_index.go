package xstocks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MintCatalog resolves a Solana mint address to catalog metadata.
type MintCatalog interface {
	LookupByMint(ctx context.Context, mint string) (CatalogAsset, bool, error)
}

// SymbolCatalog resolves a ticker to catalog metadata.
//
// This exists because resolving a ticker through Search is the wrong shape for
// it. Search walks every catalogue page on every call, so asking it for twelve
// held symbols is twelve full crawls of a third-party API — per request, per
// viewer, on a screen that polls. The catalogue is one small, slow-moving list:
// crawl it once, index it by symbol and by mint, and answer from memory.
type SymbolCatalog interface {
	LookupBySymbol(ctx context.Context, symbol string) (CatalogAsset, bool, error)
}

const (
	// catalogIndexTTL is how long one crawl of the catalogue is reused. The list
	// changes when Backed lists a new xStock, which is weeks apart, so this is
	// about picking up a new listing within the hour rather than about freshness.
	catalogIndexTTL = 15 * time.Minute
	// catalogIndexRetryAfter keeps a failed crawl from being retried on every
	// request. A catalogue that is down stays down for a few seconds at a time.
	catalogIndexRetryAfter = 10 * time.Second
	// catalogIndexLoadTimeout bounds one crawl. It is generous because the crawl
	// is shared by everyone and nobody should ever have to repeat it, and because
	// callers no longer wait on it past their own deadline.
	catalogIndexLoadTimeout = 45 * time.Second
	// catalogIndexMaxPages stops a crawl that an upstream paging bug would
	// otherwise run forever. The catalogue is a few hundred rows.
	catalogIndexMaxPages = 64
)

// catalogIndex is the whole xStocks catalogue, crawled once and kept for
// catalogIndexTTL.
//
// The crawl runs under its own lock and its own context, and the result is
// swapped in under the data lock at the end, so readers are never blocked for the
// length of a multi-page fetch and one caller walking away does not abandon the
// crawl everybody else is waiting on.
type catalogIndex struct {
	mu       sync.RWMutex
	entries  catalogEntries
	loadedAt time.Time
	failedAt time.Time

	loadMu sync.Mutex
}

// catalogEntries is one crawl's result: the same assets three ways. `all` keeps
// catalogue order, which is what a search result is ranked from, so a query
// answers in a stable order rather than a map's.
type catalogEntries struct {
	all      []CatalogAsset
	byMint   map[string]CatalogAsset
	bySymbol map[string]CatalogAsset
}

func (e catalogEntries) loaded() bool { return e.byMint != nil }

// snapshot returns the indexed catalogue and whether it is still fresh.
func (i *catalogIndex) snapshot() (catalogEntries, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	fresh := !i.loadedAt.IsZero() && time.Since(i.loadedAt) < catalogIndexTTL
	return i.entries, fresh
}

// backoff reports whether a crawl failed recently enough that retrying now would
// just be a second failure.
func (i *catalogIndex) backoff() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return !i.failedAt.IsZero() && time.Since(i.failedAt) < catalogIndexRetryAfter
}

func (i *catalogIndex) store(entries catalogEntries) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.entries = entries
	i.loadedAt = time.Now()
	i.failedAt = time.Time{}
}

func (i *catalogIndex) markFailed() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.failedAt = time.Now()
}

// LookupByMint resolves a Solana mint through the catalogue index.
func (s *HTTPCatalogSearcher) LookupByMint(ctx context.Context, mint string) (CatalogAsset, bool, error) {
	mint = strings.TrimSpace(mint)
	if mint == "" {
		return CatalogAsset{}, false, nil
	}
	entries, err := s.index(ctx)
	if err != nil {
		return CatalogAsset{}, false, err
	}
	asset, ok := entries.byMint[mint]
	return asset, ok, nil
}

// LookupBySymbol resolves a ticker through the catalogue index, case-insensitively.
func (s *HTTPCatalogSearcher) LookupBySymbol(ctx context.Context, symbol string) (CatalogAsset, bool, error) {
	key := strings.ToUpper(strings.TrimSpace(symbol))
	if key == "" {
		return CatalogAsset{}, false, nil
	}
	entries, err := s.index(ctx)
	if err != nil {
		return CatalogAsset{}, false, err
	}
	asset, ok := entries.bySymbol[key]
	return asset, ok, nil
}

// index returns the crawled catalogue, refreshing it if the last crawl has aged out.
//
// A caller whose own deadline expires while the crawl is running gets its error
// and leaves; the crawl itself carries on to completion on a detached context, so
// the work is not thrown away and the next caller is served from memory. The
// alternative — letting the first caller's cancellation kill the crawl — means a
// busy page with short budgets never finishes building the index at all.
func (s *HTTPCatalogSearcher) index(ctx context.Context) (catalogEntries, error) {
	if entries, fresh := s.catalog.snapshot(); fresh {
		return entries, nil
	}

	type loadResult struct {
		entries catalogEntries
		err     error
	}
	done := make(chan loadResult, 1)
	go func() {
		s.catalog.loadMu.Lock()
		defer s.catalog.loadMu.Unlock()
		// Another goroutine may have finished the crawl while this one waited.
		if entries, fresh := s.catalog.snapshot(); fresh {
			done <- loadResult{entries: entries}
			return
		}
		stale, _ := s.catalog.snapshot()
		if s.catalog.backoff() {
			if stale.loaded() {
				// Stale beats empty: the catalogue is a slow-moving list, and a row
				// resolved from a quarter-hour-old crawl is still the right row.
				done <- loadResult{entries: stale}
				return
			}
			done <- loadResult{err: fmt.Errorf("%w: catalogue index unavailable", ErrInvalidResponse)}
			return
		}

		crawlCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), catalogIndexLoadTimeout)
		defer cancel()
		entries, err := s.crawlCatalog(crawlCtx)
		if err != nil {
			s.catalog.markFailed()
			if stale.loaded() {
				done <- loadResult{entries: stale}
				return
			}
			done <- loadResult{err: err}
			return
		}
		s.catalog.store(entries)
		done <- loadResult{entries: entries}
	}()

	select {
	case result := <-done:
		return result.entries, result.err
	case <-ctx.Done():
		// The caller's budget is up. Anything already indexed still answers.
		if stale, _ := s.catalog.snapshot(); stale.loaded() {
			return stale, nil
		}
		return catalogEntries{}, ctx.Err()
	}
}

// crawlCatalog walks every catalogue page once and builds the index from it.
func (s *HTTPCatalogSearcher) crawlCatalog(ctx context.Context) (catalogEntries, error) {
	entries := catalogEntries{
		byMint:   make(map[string]CatalogAsset),
		bySymbol: make(map[string]CatalogAsset),
	}

	hasNextPage := true
	for page := 0; hasNextPage && page < catalogIndexMaxPages; page++ {
		body, err := s.fetchCatalogListPage(ctx, page)
		if err != nil {
			return catalogEntries{}, err
		}
		var list catalogListResponse
		if err := json.Unmarshal(body, &list); err != nil {
			return catalogEntries{}, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
		}
		for _, node := range list.Nodes {
			mint, err := solanaMintFromDeployments(node.Deployments)
			if err != nil {
				continue
			}
			asset := catalogAssetFromNode(node, mint)
			if _, seen := entries.byMint[mint]; seen {
				continue
			}
			entries.byMint[mint] = asset
			if key := strings.ToUpper(asset.Symbol); key != "" {
				entries.bySymbol[key] = asset
			}
			entries.all = append(entries.all, asset)
		}
		hasNextPage = list.Page.HasNextPage
	}
	return entries, nil
}

func (f *fakeCatalogSearcher) LookupByMint(ctx context.Context, mint string) (CatalogAsset, bool, error) {
	_ = ctx
	mint = strings.TrimSpace(mint)
	if mint == "" {
		return CatalogAsset{}, false, nil
	}

	f.mu.Lock()
	assets := append([]CatalogAsset(nil), f.assets...)
	f.mu.Unlock()

	for _, asset := range assets {
		if strings.TrimSpace(asset.SolanaMint) == mint {
			return asset, true, nil
		}
	}
	return CatalogAsset{}, false, nil
}

func (f *fakeCatalogSearcher) LookupBySymbol(ctx context.Context, symbol string) (CatalogAsset, bool, error) {
	_ = ctx
	key := strings.ToUpper(strings.TrimSpace(symbol))
	if key == "" {
		return CatalogAsset{}, false, nil
	}

	f.mu.Lock()
	if f.listErr != nil {
		err := f.listErr
		f.mu.Unlock()
		return CatalogAsset{}, false, err
	}
	assets := append([]CatalogAsset(nil), f.assets...)
	f.mu.Unlock()

	for _, asset := range assets {
		if strings.ToUpper(strings.TrimSpace(asset.Symbol)) == key {
			return asset, true, nil
		}
	}
	return CatalogAsset{}, false, nil
}
