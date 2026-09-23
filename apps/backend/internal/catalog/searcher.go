package catalog

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const (
	defaultVariantCacheTTL = 30 * time.Minute
	searchMergeLimit       = 100_000
)

// Composite merges xStocks catalog search with supplemental sources (e.g. Tessera).
type Composite struct {
	xstocks xstocks.CatalogSearcher
	tessera Source
	prober  xstocks.RoutabilityProber

	defaultVariantMu sync.Mutex
	defaultVariant   map[string]defaultVariantEntry

	tesseraListErrOnce sync.Once
}

type defaultVariantEntry struct {
	mint      string
	expiresAt time.Time
}

// NewComposite returns a catalog searcher over xStocks and an optional Tessera source.
func NewComposite(xstocksSearcher xstocks.CatalogSearcher, tessera Source, prober xstocks.RoutabilityProber) *Composite {
	return &Composite{
		xstocks:        xstocksSearcher,
		tessera:        tessera,
		prober:         prober,
		defaultVariant: make(map[string]defaultVariantEntry),
	}
}

// Search implements xstocks.CatalogSearcher.
func (c *Composite) Search(ctx context.Context, query string, limit, offset int) (xstocks.CatalogSearchPage, error) {
	return c.SearchKind(ctx, query, "", limit, offset)
}

// SearchKind searches the merged catalog, optionally filtering to one AssetKind.
func (c *Composite) SearchKind(ctx context.Context, query string, kind xstocks.AssetKind, limit, offset int) (xstocks.CatalogSearchPage, error) {
	query = strings.TrimSpace(query)
	if limit <= 0 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}

	var merged []xstocks.CatalogAsset
	var hasMore bool

	if query == "" {
		// xStocks is already paged. Pre-IPO rows are appended on the first page
		// and are not trimmed back to limit, so a full xStocks page cannot hide them.
		// Later pages are xStocks only. A pre_ipo filter pages the Tessera list itself.
		if kind == xstocks.AssetKindPreIPO {
			tesseraRows := c.tesseraRows(ctx)
			probeAndRankTessera(ctx, c.prober, tesseraRows)
			merged = c.collapseByUnderlying(ctx, tesseraRows)
			hasMore = len(merged) > offset+limit
			return pageSlice(merged, offset, limit, hasMore), nil
		}
		xsPage, err := c.xstocks.Search(ctx, "", limit, offset)
		if err != nil {
			return xstocks.CatalogSearchPage{}, err
		}
		merged = append(merged, xsPage.Assets...)
		if offset == 0 && kind == "" {
			tesseraRows := c.tesseraRows(ctx)
			probeAndRankTessera(ctx, c.prober, tesseraRows)
			merged = append(merged, tesseraRows...)
		}
		if kind != "" {
			filtered := make([]xstocks.CatalogAsset, 0, len(merged))
			for _, asset := range merged {
				if asset.Normalize().Kind == kind {
					filtered = append(filtered, asset)
				}
			}
			merged = filtered
		}
		merged = c.collapseByUnderlying(ctx, merged)
		return xstocks.CatalogSearchPage{Assets: merged, HasMore: xsPage.HasMore}, nil
	} else {
		xsPage, err := c.xstocks.Search(ctx, query, searchMergeLimit, 0)
		if err != nil {
			return xstocks.CatalogSearchPage{}, err
		}
		tesseraMatches := c.matchTessera(ctx, query)
		merged = mergeRankedSearch(ctx, query, xsPage.Assets, tesseraMatches, c.prober)
		hasMore = len(merged) > offset+limit
	}

	if kind != "" {
		filtered := make([]xstocks.CatalogAsset, 0, len(merged))
		for _, asset := range merged {
			if asset.Normalize().Kind == kind {
				filtered = append(filtered, asset)
			}
		}
		merged = filtered
		hasMore = len(merged) > offset+limit
	}

	merged = c.collapseByUnderlying(ctx, merged)

	if query != "" {
		hasMore = len(merged) > offset+limit
	}
	if offset >= len(merged) {
		return xstocks.CatalogSearchPage{Assets: nil, HasMore: hasMore}, nil
	}
	end := offset + limit
	if end > len(merged) {
		end = len(merged)
	}
	return xstocks.CatalogSearchPage{
		Assets:  merged[offset:end],
		HasMore: hasMore,
	}, nil
}

func pageSlice(merged []xstocks.CatalogAsset, offset, limit int, hasMore bool) xstocks.CatalogSearchPage {
	if offset >= len(merged) {
		return xstocks.CatalogSearchPage{HasMore: hasMore}
	}
	end := offset + limit
	if end > len(merged) {
		end = len(merged)
	}
	return xstocks.CatalogSearchPage{Assets: merged[offset:end], HasMore: hasMore}
}

// SearchVariants returns every catalog row sharing an underlying company id.
func (c *Composite) SearchVariants(ctx context.Context, underlyingID string) ([]xstocks.CatalogAsset, error) {
	underlyingID = strings.ToLower(strings.TrimSpace(underlyingID))
	if underlyingID == "" {
		return nil, nil
	}

	xsPage, err := c.xstocks.Search(ctx, underlyingID, searchMergeLimit, 0)
	if err != nil {
		return nil, err
	}
	var out []xstocks.CatalogAsset
	for _, asset := range xsPage.Assets {
		if underlyingKey(asset) == underlyingID {
			out = append(out, asset.Normalize())
		}
	}
	for _, asset := range c.tesseraRows(ctx) {
		if underlyingKey(asset) == underlyingID {
			out = append(out, asset.Normalize())
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Symbol) < strings.ToLower(out[j].Symbol)
	})
	return out, nil
}

// LookupByMint returns the exact mint row from xStocks or Tessera.
func (c *Composite) LookupByMint(ctx context.Context, mint string) (xstocks.CatalogAsset, bool, error) {
	if c.xstocks != nil {
		asset, ok, err := c.xstocks.LookupByMint(ctx, mint)
		if err != nil || ok {
			return asset.Normalize(), ok, err
		}
	}
	for _, asset := range c.tesseraRows(ctx) {
		if strings.TrimSpace(asset.SolanaMint) == strings.TrimSpace(mint) {
			return asset.Normalize(), true, nil
		}
	}
	return xstocks.CatalogAsset{}, false, nil
}

func (c *Composite) tesseraRows(ctx context.Context) []xstocks.CatalogAsset {
	if c.tessera == nil {
		return nil
	}
	rows, err := c.tessera.List(ctx)
	if err != nil {
		c.tesseraListErrOnce.Do(func() {
			slog.Warn("catalog: tessera list failed; continuing with xstocks only", "err", err)
		})
		return nil
	}
	out := make([]xstocks.CatalogAsset, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Normalize())
	}
	return out
}

func (c *Composite) matchTessera(ctx context.Context, query string) []xstocks.CatalogAsset {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return c.tesseraRows(ctx)
	}
	matches := make([]xstocks.CatalogAsset, 0)
	for _, asset := range c.tesseraRows(ctx) {
		if tesseraRowMatches(asset, needle) {
			matches = append(matches, asset)
		}
	}
	return matches
}

func tesseraRowMatches(asset xstocks.CatalogAsset, needle string) bool {
	symbol := strings.ToLower(strings.TrimSpace(asset.Symbol))
	name := strings.ToLower(strings.TrimSpace(asset.Name))
	sector := strings.ToLower(strings.TrimSpace(asset.Sector))
	underlying := underlyingKey(asset)
	stripped := tesseraStrippedName(asset.Name)
	return strings.Contains(symbol, needle) ||
		strings.Contains(name, needle) ||
		strings.Contains(stripped, needle) ||
		strings.Contains(sector, needle) ||
		strings.Contains(underlying, needle)
}

func tesseraStrippedName(name string) string {
	n := strings.TrimSpace(name)
	if len(n) >= 2 && strings.EqualFold(n[:2], "T-") {
		return strings.ToLower(n[2:])
	}
	return strings.ToLower(n)
}

func tesseraExactMatch(query string, asset xstocks.CatalogAsset) bool {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return false
	}
	symbol := strings.ToLower(strings.TrimSpace(asset.Symbol))
	if needle == symbol {
		return true
	}
	return needle == tesseraStrippedName(asset.Name)
}

func mergeRankedSearch(ctx context.Context, query string, xs []xstocks.CatalogAsset, tessera []xstocks.CatalogAsset, prober xstocks.RoutabilityProber) []xstocks.CatalogAsset {
	xs = append([]xstocks.CatalogAsset(nil), xs...)
	for i := range xs {
		xs[i] = xs[i].Normalize()
	}
	tessera = append([]xstocks.CatalogAsset(nil), tessera...)
	probeAndRankTessera(ctx, prober, tessera)

	exactTessera := false
	for _, asset := range tessera {
		if tesseraExactMatch(query, asset) {
			exactTessera = true
			break
		}
	}

	byMint := make(map[string]xstocks.CatalogAsset)
	order := make([]string, 0, len(xs)+len(tessera))
	add := func(asset xstocks.CatalogAsset) {
		mint := strings.TrimSpace(asset.SolanaMint)
		if mint == "" {
			return
		}
		if _, ok := byMint[mint]; ok {
			return
		}
		byMint[mint] = asset
		order = append(order, mint)
	}

	if exactTessera {
		sortTesseraMatches(tessera)
		for _, asset := range tessera {
			add(asset)
		}
		rankXStockMatches(ctx, prober, xs)
		for _, asset := range xs {
			add(asset)
		}
		return mintOrderAssets(order, byMint)
	}

	combined := append(append([]xstocks.CatalogAsset{}, xs...), tessera...)
	sortMergedDefault(combined)
	for _, asset := range combined {
		add(asset)
	}
	return mintOrderAssets(order, byMint)
}

func mintOrderAssets(order []string, byMint map[string]xstocks.CatalogAsset) []xstocks.CatalogAsset {
	out := make([]xstocks.CatalogAsset, 0, len(order))
	for _, mint := range order {
		out = append(out, byMint[mint])
	}
	return out
}

func rankXStockMatches(ctx context.Context, prober xstocks.RoutabilityProber, matches []xstocks.CatalogAsset) {
	for i := range matches {
		if prober == nil {
			matches[i].Routable = true
			continue
		}
		matches[i].Routable = prober.IsRoutable(ctx, matches[i])
	}
	sortXStockMergeMatches(matches)
}

func probeAndRankTessera(ctx context.Context, prober xstocks.RoutabilityProber, matches []xstocks.CatalogAsset) {
	for i := range matches {
		if prober == nil {
			matches[i].Routable = true
			continue
		}
		matches[i].Routable = prober.IsRoutable(ctx, matches[i])
	}
}

func sortXStockMergeMatches(matches []xstocks.CatalogAsset) {
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Routable != matches[j].Routable {
			return matches[i].Routable
		}
		ri, iPinned := xstocks.PinnedCatalogOrder(matches[i].Symbol)
		rj, jPinned := xstocks.PinnedCatalogOrder(matches[j].Symbol)
		if iPinned && jPinned {
			return ri < rj
		}
		if iPinned != jPinned {
			return iPinned
		}
		return strings.ToLower(matches[i].Symbol) < strings.ToLower(matches[j].Symbol)
	})
}

func sortTesseraMatches(matches []xstocks.CatalogAsset) {
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Routable != matches[j].Routable {
			return matches[i].Routable
		}
		return strings.ToLower(matches[i].Symbol) < strings.ToLower(matches[j].Symbol)
	})
}

func sortMergedDefault(matches []xstocks.CatalogAsset) {
	sort.SliceStable(matches, func(i, j int) bool {
		ti, tj := mergeDefaultTier(matches[i]), mergeDefaultTier(matches[j])
		if ti != tj {
			return ti < tj
		}
		if ti == 0 {
			ri, _ := xstocks.PinnedCatalogOrder(matches[i].Symbol)
			rj, _ := xstocks.PinnedCatalogOrder(matches[j].Symbol)
			if ri != rj {
				return ri < rj
			}
		}
		return strings.ToLower(matches[i].Symbol) < strings.ToLower(matches[j].Symbol)
	})
}

func mergeDefaultTier(asset xstocks.CatalogAsset) int {
	asset = asset.Normalize()
	if asset.Source == xstocks.AssetSourceXStocks || asset.Kind == xstocks.AssetKindStock {
		if asset.Routable {
			if _, pinned := xstocks.PinnedCatalogOrder(asset.Symbol); pinned {
				return 0
			}
		}
	}
	if asset.Source == xstocks.AssetSourceTessera || asset.Kind == xstocks.AssetKindPreIPO {
		if asset.Routable {
			return 1
		}
	}
	return 2
}

func underlyingKey(asset xstocks.CatalogAsset) string {
	asset = asset.Normalize()
	if id := strings.ToLower(strings.TrimSpace(asset.UnderlyingID)); id != "" {
		return id
	}
	if asset.Kind == xstocks.AssetKindStock || asset.Source == xstocks.AssetSourceXStocks {
		return xstocks.UnderlyingIDFromXStockSymbol(asset.Symbol)
	}
	return strings.ToLower(strings.TrimSpace(asset.Symbol))
}

func (c *Composite) collapseByUnderlying(ctx context.Context, assets []xstocks.CatalogAsset) []xstocks.CatalogAsset {
	if len(assets) == 0 {
		return assets
	}
	groups := make(map[string][]xstocks.CatalogAsset)
	order := make([]string, 0)
	for _, asset := range assets {
		key := underlyingKey(asset)
		if key == "" {
			key = asset.SolanaMint
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], asset.Normalize())
	}
	out := make([]xstocks.CatalogAsset, 0, len(order))
	for _, key := range order {
		variants := groups[key]
		if len(variants) == 1 {
			out = append(out, variants[0])
			continue
		}
		out = append(out, c.pickDefaultVariant(ctx, key, variants))
	}
	return out
}

func (c *Composite) pickDefaultVariant(ctx context.Context, underlyingID string, variants []xstocks.CatalogAsset) xstocks.CatalogAsset {
	if cached, ok := c.cachedDefaultVariant(underlyingID); ok {
		for _, v := range variants {
			if v.SolanaMint == cached {
				return v
			}
		}
	}
	chosen := selectDefaultVariant(variants)
	c.storeDefaultVariant(underlyingID, chosen.SolanaMint)
	_ = ctx
	return chosen
}

func (c *Composite) cachedDefaultVariant(underlyingID string) (mint string, ok bool) {
	c.defaultVariantMu.Lock()
	defer c.defaultVariantMu.Unlock()
	entry, found := c.defaultVariant[underlyingID]
	if !found || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.mint, true
}

func (c *Composite) storeDefaultVariant(underlyingID, mint string) {
	c.defaultVariantMu.Lock()
	defer c.defaultVariantMu.Unlock()
	c.defaultVariant[underlyingID] = defaultVariantEntry{
		mint:      mint,
		expiresAt: time.Now().Add(defaultVariantCacheTTL),
	}
}

func selectDefaultVariant(variants []xstocks.CatalogAsset) xstocks.CatalogAsset {
	if len(variants) == 0 {
		return xstocks.CatalogAsset{}
	}
	if len(variants) == 1 {
		return variants[0]
	}
	sorted := append([]xstocks.CatalogAsset(nil), variants...)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Routable != b.Routable {
			return a.Routable
		}
		if a.LiquidityUsd != b.LiquidityUsd {
			return a.LiquidityUsd > b.LiquidityUsd
		}
		return issuerRank(a) < issuerRank(b)
	})
	return sorted[0]
}

func issuerRank(asset xstocks.CatalogAsset) int {
	asset = asset.Normalize()
	switch asset.Kind {
	case xstocks.AssetKindPreIPO:
		if asset.Source == xstocks.AssetSourceTessera || asset.Issuer == "tessera" {
			return 0
		}
		return 1
	default:
		if asset.Source == xstocks.AssetSourceXStocks || asset.Issuer == "xstocks" {
			return 0
		}
		return 1
	}
}
