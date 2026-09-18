package xstocks

import (
	"context"
	"strings"
)

// Popular returns pinned major xStocks up to limit, resolved via catalog search.
func Popular(ctx context.Context, searcher CatalogSearcher, limit int) ([]CatalogAsset, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > len(pinnedCatalogSymbols) {
		limit = len(pinnedCatalogSymbols)
	}

	out := make([]CatalogAsset, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, symbol := range pinnedCatalogSymbols {
		if len(out) >= limit {
			break
		}
		page, err := searcher.Search(ctx, symbol, 1, 0)
		if err != nil {
			return nil, err
		}
		for _, asset := range page.Assets {
			key := strings.ToUpper(strings.TrimSpace(asset.Symbol))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, asset)
			break
		}
	}
	return out, nil
}
