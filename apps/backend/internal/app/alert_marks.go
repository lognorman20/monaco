package app

import (
	"context"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// AlertMark is one stock's current mark, as a price alert reads it.
type AlertMark struct {
	// Symbol is the catalogue's spelling; Name its display name.
	Symbol          string
	Name            string
	PriceUsdcMicros int64
}

// AlertMarkSource prices a set of symbols for price alerts. The map is keyed by upper-cased
// symbol; a symbol that could not be resolved or priced is simply absent.
type AlertMarkSource interface {
	Marks(ctx context.Context, symbols []string) (map[string]AlertMark, error)
}

// CatalogMarkSource prices symbols the way the Stocks tab prices a row
// (GET /v1/assets/popular): the catalogue names each symbol's mint, and Jupiter's Price API
// marks every mint in one batched call. An alert therefore fires on the figure the member
// sees on the row, not on some other feed's idea of the price.
type CatalogMarkSource struct {
	Catalog xstocks.SymbolCatalog
	Price   jupiter.PriceClient
}

// Marks resolves and prices symbols. A catalogue miss or error on one symbol drops that
// symbol; the call fails only when the price read fails, or when nothing resolved and the
// catalogue said why.
func (s *CatalogMarkSource) Marks(ctx context.Context, symbols []string) (map[string]AlertMark, error) {
	out := map[string]AlertMark{}
	if s == nil || s.Catalog == nil || s.Price == nil || len(symbols) == 0 {
		return out, nil
	}

	byMint := map[string]xstocks.CatalogAsset{}
	mints := make([]string, 0, len(symbols))
	seen := map[string]bool{}
	var lookupErr error
	for _, raw := range symbols {
		key := strings.ToUpper(strings.TrimSpace(raw))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		asset, found, err := s.Catalog.LookupBySymbol(ctx, raw)
		if err != nil {
			lookupErr = err
			continue
		}
		if !found {
			continue
		}
		n := asset.Normalize()
		mint := strings.TrimSpace(n.SolanaMint)
		if mint == "" {
			continue
		}
		if _, dup := byMint[mint]; !dup {
			mints = append(mints, mint)
		}
		byMint[mint] = n
	}
	if len(mints) == 0 {
		return out, lookupErr
	}

	prices, err := s.Price.Prices(ctx, mints)
	if err != nil {
		return nil, err
	}
	for mint, asset := range byMint {
		price, ok := prices[mint]
		if !ok || price.PriceUsdcMicros <= 0 {
			continue
		}
		out[strings.ToUpper(strings.TrimSpace(asset.Symbol))] = AlertMark{
			Symbol:          asset.Symbol,
			Name:            asset.Name,
			PriceUsdcMicros: price.PriceUsdcMicros,
		}
	}
	return out, nil
}
