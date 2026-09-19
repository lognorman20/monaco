package xstocks

import (
	"context"
	"strings"
	"sync"
)

// Popular returns pinned major xStocks up to limit, resolved via catalog search.
func Popular(ctx context.Context, searcher CatalogSearcher, limit int) ([]CatalogAsset, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > len(pinnedCatalogSymbols) {
		limit = len(pinnedCatalogSymbols)
	}

	type slot struct {
		asset CatalogAsset
		ok    bool
	}
	slots := make([]slot, len(pinnedCatalogSymbols))
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

	for i, symbol := range pinnedCatalogSymbols {
		wg.Add(1)
		go func(i int, symbol string) {
			defer wg.Done()
			page, err := searcher.Search(ctx, symbol, 1, 0)
			if err != nil {
				errOnce.Do(func() { firstErr = err })
				return
			}
			if len(page.Assets) == 0 {
				return
			}
			slots[i] = slot{asset: page.Assets[0], ok: true}
		}(i, symbol)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	out := make([]CatalogAsset, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, item := range slots {
		if len(out) >= limit {
			break
		}
		if !item.ok {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(item.asset.Symbol))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item.asset)
	}
	return out, nil
}
