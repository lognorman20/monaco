package catalog

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/solana/mintinfo"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const searchMergeLimit = 100_000

// TaggedSource pairs a supplemental catalog source with its asset-source id.
type TaggedSource struct {
	Source   Source
	SourceID xstocks.AssetSource
}

// Composite merges xStocks catalog search with supplemental sources (e.g. Tessera, PreStocks).
type Composite struct {
	xstocks  xstocks.CatalogSearcher
	sources  []TaggedSource
	prober   xstocks.RoutabilityProber
	mintinfo mintinfo.Reader
	prices   jupiter.PriceClient

	defaultVariantMu sync.Mutex
	defaultVariant   map[string]defaultVariantEntry

	comparisonMu    sync.Mutex
	comparisonCache map[string]comparisonCacheEntry

	listErrOnce sync.Once
}

type defaultVariantEntry struct {
	mint      string
	expiresAt time.Time
}

// NewComposite returns a catalog searcher over xStocks and an optional Tessera source.
func NewComposite(xstocksSearcher xstocks.CatalogSearcher, tessera Source, prober xstocks.RoutabilityProber) *Composite {
	var sources []TaggedSource
	if tessera != nil {
		sources = []TaggedSource{{Source: tessera, SourceID: xstocks.AssetSourceTessera}}
	}
	return NewCompositeWithSources(xstocksSearcher, sources, prober, nil, nil)
}

// NewCompositeWithSources merges xStocks with tagged supplemental sources, mint enrichment, and best-price ranking.
func NewCompositeWithSources(
	xstocksSearcher xstocks.CatalogSearcher,
	sources []TaggedSource,
	prober xstocks.RoutabilityProber,
	mints mintinfo.Reader,
	prices jupiter.PriceClient,
) *Composite {
	tagCopy := append([]TaggedSource(nil), sources...)
	return &Composite{
		xstocks:         xstocksSearcher,
		sources:         tagCopy,
		prober:          prober,
		mintinfo:        mints,
		prices:          prices,
		defaultVariant:  make(map[string]defaultVariantEntry),
		comparisonCache: make(map[string]comparisonCacheEntry),
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
		if kind == xstocks.AssetKindPreIPO {
			supplemental := c.supplementalRows(ctx)
			probeSupplemental(ctx, c.prober, supplemental)
			merged = c.collapseByUnderlying(ctx, supplemental)
			hasMore = len(merged) > offset+limit
			return pageSlice(merged, offset, limit, hasMore), nil
		}
		xsPage, err := c.xstocks.Search(ctx, "", limit, offset)
		if err != nil {
			return xstocks.CatalogSearchPage{}, err
		}
		merged = append(merged, xsPage.Assets...)
		if offset == 0 && kind == "" {
			supplemental := c.supplementalRows(ctx)
			probeSupplemental(ctx, c.prober, supplemental)
			merged = append(merged, supplemental...)
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
	}

	xsPage, err := c.xstocks.Search(ctx, query, searchMergeLimit, 0)
	if err != nil {
		return xstocks.CatalogSearchPage{}, err
	}
	supplementalMatches := c.matchSupplemental(ctx, query)
	merged = mergeRankedSearch(ctx, query, xsPage.Assets, supplementalMatches, c.prober)
	hasMore = len(merged) > offset+limit

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
	hasMore = len(merged) > offset+limit

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
			out = append(out, c.enrichAsset(ctx, asset.Normalize()))
		}
	}
	for _, asset := range c.supplementalRows(ctx) {
		if underlyingKey(asset) == underlyingID {
			enriched := c.enrichAsset(ctx, asset)
			if c.prober != nil {
				enriched.Routable = c.prober.IsRoutable(ctx, enriched)
			} else {
				enriched.Routable = true
			}
			if enriched.Paused {
				enriched.Routable = false
			}
			out = append(out, enriched)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Symbol) < strings.ToLower(out[j].Symbol)
	})
	return out, nil
}

// LookupByMint returns the exact mint row from xStocks or supplemental sources.
func (c *Composite) LookupByMint(ctx context.Context, mint string) (xstocks.CatalogAsset, bool, error) {
	if c.xstocks != nil {
		asset, ok, err := c.xstocks.LookupByMint(ctx, mint)
		if err != nil || ok {
			return c.enrichAsset(ctx, asset.Normalize()), ok, err
		}
	}
	for _, asset := range c.supplementalRows(ctx) {
		if strings.TrimSpace(asset.SolanaMint) == strings.TrimSpace(mint) {
			return c.enrichAsset(ctx, asset), true, nil
		}
	}
	return xstocks.CatalogAsset{}, false, nil
}

func (c *Composite) supplementalRows(ctx context.Context) []xstocks.CatalogAsset {
	if len(c.sources) == 0 {
		return nil
	}
	var out []xstocks.CatalogAsset
	for _, tagged := range c.sources {
		if tagged.Source == nil {
			continue
		}
		rows, err := tagged.Source.List(ctx)
		if err != nil {
			c.listErrOnce.Do(func() {
				slog.Warn("catalog: supplemental list failed; continuing with xstocks only", "err", err)
			})
			continue
		}
		for _, row := range rows {
			asset := row.Normalize()
			if asset.Source == "" && tagged.SourceID != "" {
				asset.Source = tagged.SourceID
			}
			out = append(out, c.enrichAsset(ctx, asset))
		}
	}
	return out
}

func (c *Composite) enrichAsset(ctx context.Context, asset xstocks.CatalogAsset) xstocks.CatalogAsset {
	asset = asset.Normalize()
	fillIssuerName(&asset)
	if asset.Kind != xstocks.AssetKindPreIPO || c.mintinfo == nil {
		return asset
	}
	info, err := c.mintinfo.Info(ctx, asset.SolanaMint)
	if err != nil {
		if errors.Is(err, mintinfo.ErrUnknownMint) {
			return asset
		}
		return asset
	}
	asset.Decimals = info.Decimals
	asset.TransferFeeBps = info.TransferFeeBps
	if info.UiMultiplier != nil {
		asset.UiAmountMultiplier = new(big.Rat).Set(info.UiMultiplier)
	}
	asset.Paused = info.Paused
	if asset.Paused {
		asset.Routable = false
	}
	return asset
}

func fillIssuerName(asset *xstocks.CatalogAsset) {
	if asset.IssuerName != "" {
		return
	}
	switch asset.Source {
	case xstocks.AssetSourcePreStocks:
		asset.IssuerName = "PreStocks"
	case xstocks.AssetSourceTessera:
		asset.IssuerName = "Tessera"
	case xstocks.AssetSourceXStocks:
		asset.IssuerName = "xStocks"
	default:
		switch asset.Issuer {
		case "prestocks":
			asset.IssuerName = "PreStocks"
		case "tessera":
			asset.IssuerName = "Tessera"
		case "xstocks":
			asset.IssuerName = "xStocks"
		}
	}
}

func (c *Composite) matchSupplemental(ctx context.Context, query string) []xstocks.CatalogAsset {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return c.supplementalRows(ctx)
	}
	matches := make([]xstocks.CatalogAsset, 0)
	for _, asset := range c.supplementalRows(ctx) {
		if supplementalRowMatches(asset, needle) {
			matches = append(matches, asset)
		}
	}
	return matches
}

func supplementalRowMatches(asset xstocks.CatalogAsset, needle string) bool {
	symbol := strings.ToLower(strings.TrimSpace(asset.Symbol))
	name := strings.ToLower(strings.TrimSpace(asset.Name))
	sector := strings.ToLower(strings.TrimSpace(asset.Sector))
	underlying := underlyingKey(asset)
	stripped := supplementalStrippedName(asset)
	return strings.Contains(symbol, needle) ||
		strings.Contains(name, needle) ||
		strings.Contains(stripped, needle) ||
		strings.Contains(sector, needle) ||
		strings.Contains(underlying, needle)
}

func supplementalStrippedName(asset xstocks.CatalogAsset) string {
	n := strings.TrimSpace(asset.Name)
	if asset.Source == xstocks.AssetSourceTessera || asset.Issuer == "tessera" {
		if len(n) >= 2 && strings.EqualFold(n[:2], "T-") {
			return strings.ToLower(n[2:])
		}
	}
	return strings.ToLower(n)
}

func supplementalExactMatch(query string, asset xstocks.CatalogAsset) bool {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return false
	}
	if supplementalSymbolExact(query, asset) {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(asset.Name))
	if needle == name {
		return true
	}
	if needle == supplementalStrippedName(asset) {
		return true
	}
	if asset.Source == xstocks.AssetSourcePreStocks || asset.Issuer == "prestocks" {
		if needle == strings.ToLower(strings.TrimSpace(asset.Name+" PreStocks")) {
			return true
		}
	}
	return false
}

func resolverExactMatch(query string, asset xstocks.CatalogAsset) bool {
	if supplementalSymbolExact(query, asset) {
		return true
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	name := strings.ToLower(strings.TrimSpace(asset.Name))
	if needle != "" && needle == name {
		return true
	}
	stripped := supplementalStrippedName(asset)
	if needle != "" && needle == stripped && query == strings.ToLower(query) {
		return true
	}
	if asset.Source == xstocks.AssetSourcePreStocks || asset.Issuer == "prestocks" {
		full := strings.ToLower(strings.TrimSpace(asset.Name + " PreStocks"))
		if needle == full {
			return true
		}
	}
	return false
}

func supplementalSymbolExact(query string, asset xstocks.CatalogAsset) bool {
	sym := strings.TrimSpace(asset.Symbol)
	q := strings.TrimSpace(query)
	if q == sym {
		return true
	}
	if strings.EqualFold(q, sym) {
		if sym == strings.ToUpper(sym) && q != sym {
			return false
		}
		return true
	}
	return false
}

func mergeRankedSearch(ctx context.Context, query string, xs []xstocks.CatalogAsset, supplemental []xstocks.CatalogAsset, prober xstocks.RoutabilityProber) []xstocks.CatalogAsset {
	xs = append([]xstocks.CatalogAsset(nil), xs...)
	for i := range xs {
		xs[i] = xs[i].Normalize()
	}
	supplemental = append([]xstocks.CatalogAsset(nil), supplemental...)
	probeSupplemental(ctx, prober, supplemental)

	exactSupplemental := false
	for _, asset := range supplemental {
		if supplementalExactMatch(query, asset) {
			exactSupplemental = true
			break
		}
	}

	byMint := make(map[string]xstocks.CatalogAsset)
	order := make([]string, 0, len(xs)+len(supplemental))
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

	if exactSupplemental {
		sortSupplementalMatches(supplemental)
		for _, asset := range supplemental {
			add(asset)
		}
		rankXStockMatches(ctx, prober, xs)
		for _, asset := range xs {
			add(asset)
		}
		return mintOrderAssets(order, byMint)
	}

	combined := append(append([]xstocks.CatalogAsset{}, xs...), supplemental...)
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

func probeSupplemental(ctx context.Context, prober xstocks.RoutabilityProber, matches []xstocks.CatalogAsset) {
	for i := range matches {
		if matches[i].Paused {
			matches[i].Routable = false
			continue
		}
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

func sortSupplementalMatches(matches []xstocks.CatalogAsset) {
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
	if asset.Source == xstocks.AssetSourceTessera ||
		asset.Source == xstocks.AssetSourcePreStocks ||
		asset.Kind == xstocks.AssetKindPreIPO {
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
	if len(variants) == 0 {
		return xstocks.CatalogAsset{}
	}
	if len(variants) == 1 {
		return variants[0]
	}
	probeSupplemental(ctx, c.prober, variants)
	if cached, ok := c.cachedDefaultVariant(underlyingID); ok {
		for _, v := range variants {
			if v.SolanaMint == cached {
				return v
			}
		}
	}
	chosen := c.pickDefaultVariantByCompare(ctx, underlyingID, variants)
	c.storeDefaultVariant(underlyingID, chosen.SolanaMint)
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
		expiresAt: time.Now().Add(bestPriceCacheTTL),
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
		if a.Paused != b.Paused {
			return !a.Paused
		}
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
		if asset.Source == xstocks.AssetSourcePreStocks || asset.Issuer == "prestocks" {
			return 1
		}
		return 2
	default:
		if asset.Source == xstocks.AssetSourceXStocks || asset.Issuer == "xstocks" {
			return 0
		}
		return 1
	}
}

// DefaultMintForUnderlying resolves the best default variant mint for a company slug.
func (c *Composite) DefaultMintForUnderlying(ctx context.Context, underlyingID string) (string, bool) {
	underlyingID = strings.ToLower(strings.TrimSpace(underlyingID))
	variants, err := c.SearchVariants(ctx, underlyingID)
	if err != nil || len(variants) == 0 {
		return "", false
	}
	if len(variants) == 1 {
		return variants[0].SolanaMint, true
	}
	chosen := c.pickDefaultVariant(ctx, underlyingID, variants)
	if chosen.SolanaMint == "" {
		return "", false
	}
	return chosen.SolanaMint, true
}
