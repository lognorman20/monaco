package xstocks

import (
	"context"
	"strings"
	"sync"
)

const maxPopularStrip = 5

// Popular returns the Assets tab strip symbols in product order, capped at five.
func Popular(ctx context.Context, searcher CatalogSearcher, limit int) ([]CatalogAsset, error) {
	if limit <= 0 {
		limit = maxPopularStrip
	}
	if limit > maxPopularStrip {
		limit = maxPopularStrip
	}
	if limit > len(popularStripSymbols) {
		limit = len(popularStripSymbols)
	}

	type slot struct {
		asset CatalogAsset
		ok    bool
	}
	slots := make([]slot, len(popularStripSymbols))
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

	for i, symbol := range popularStripSymbols {
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
		out = append(out, item.asset)
	}
	return out, nil
}
