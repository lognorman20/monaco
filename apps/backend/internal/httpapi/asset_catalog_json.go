package httpapi

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/catalog"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const referenceFreshness = 48 * time.Hour

// Shared catalog JSON fields for list and detail asset responses.
type assetCatalogJSONFields struct {
	Kind                    string  `json:"kind"`
	Source                  string  `json:"source"`
	Issuer                  string  `json:"issuer"`
	IssuerName              string  `json:"issuerName,omitempty"`
	UnderlyingID            string  `json:"underlyingId"`
	UiAmountMultiplier      string  `json:"uiAmountMultiplier,omitempty"`
	Paused                  bool    `json:"paused,omitempty"`
	TokenDecimals           int     `json:"tokenDecimals"`
	Sector                  string  `json:"sector,omitempty"`
	LogoURL                 string  `json:"logoUrl,omitempty"`
	AlwaysOpen              bool    `json:"alwaysOpen"`
	ReferenceMarkUsdcMicros *int64  `json:"referenceMarkUsdcMicros,omitempty"`
	ReferenceValuationUsd   *int64  `json:"referenceValuationUsd,omitempty"`
	ReferenceUpdatedAt      *string `json:"referenceUpdatedAt,omitempty"`
	PremiumBps              *int    `json:"premiumBps,omitempty"`
	Holders                 *int    `json:"holders,omitempty"`
	VariantCount            int     `json:"variantCount,omitempty"`
}

type assetVariantResponse struct {
	Symbol          string `json:"symbol"`
	Issuer          string `json:"issuer"`
	IssuerName      string `json:"issuerName,omitempty"`
	SolanaMint      string `json:"solanaMint"`
	PriceUsdcMicros *int64 `json:"priceUsdcMicros,omitempty"`
	LiquidityUsd    string `json:"liquidityUsd,omitempty"`
	Routable        bool   `json:"routable"`
	TransferFeeBps  int    `json:"transferFeeBps,omitempty"`
	CostRatioBps    int64  `json:"costRatioBps,omitempty"`
	BestPrice       bool   `json:"bestPrice,omitempty"`
	Paused          bool   `json:"paused,omitempty"`
}

type catalogKindSearcher interface {
	SearchKind(ctx context.Context, query string, kind xstocks.AssetKind, limit, offset int) (xstocks.CatalogSearchPage, error)
}

type catalogVariantsSearcher interface {
	SearchVariants(ctx context.Context, underlyingID string) ([]xstocks.CatalogAsset, error)
}

func parseCatalogKindQuery(raw string) (xstocks.AssetKind, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	switch raw {
	case "":
		return "", nil
	case "stock":
		return xstocks.AssetKindStock, nil
	case "pre_ipo":
		return xstocks.AssetKindPreIPO, nil
	default:
		return "", errInvalidCatalogKind
	}
}

var errInvalidCatalogKind = invalidCatalogKindError{}

type invalidCatalogKindError struct{}

func (invalidCatalogKindError) Error() string { return "invalid catalog kind" }

func catalogSearch(ctx context.Context, catalog xstocks.CatalogSearcher, query string, kind xstocks.AssetKind, limit, offset int) (xstocks.CatalogSearchPage, error) {
	if ks, ok := catalog.(catalogKindSearcher); ok {
		return ks.SearchKind(ctx, query, kind, limit, offset)
	}
	page, err := catalog.Search(ctx, query, limit, offset)
	if err != nil || kind == "" {
		return page, err
	}
	filtered := make([]xstocks.CatalogAsset, 0, len(page.Assets))
	for _, asset := range page.Assets {
		if asset.Normalize().Kind == kind {
			filtered = append(filtered, asset)
		}
	}
	page.Assets = filtered
	return page, nil
}

func assetMatchesLookup(asset xstocks.CatalogAsset, symbol string) bool {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(asset.Symbol), symbol) {
		return true
	}
	name := strings.TrimSpace(asset.Name)
	if strings.EqualFold(name, symbol) {
		return true
	}
	if strings.HasPrefix(strings.ToUpper(name), "T-") && strings.EqualFold(strings.TrimPrefix(name, "T-"), strings.TrimPrefix(symbol, "T-")) {
		return true
	}
	if strings.EqualFold(strings.TrimPrefix(name, "T-"), symbol) {
		return true
	}
	return false
}

func variantCountFor(ctx context.Context, catalog xstocks.CatalogSearcher, asset xstocks.CatalogAsset) int {
	underlying := strings.TrimSpace(asset.UnderlyingID)
	if underlying == "" {
		return 0
	}
	vs, ok := catalog.(catalogVariantsSearcher)
	if !ok {
		return 0
	}
	variants, err := vs.SearchVariants(ctx, underlying)
	if err != nil || len(variants) <= 1 {
		return 0
	}
	return len(variants)
}

func catalogJSONFields(asset xstocks.CatalogAsset, price *jupiter.TokenPrice, variantCount int) assetCatalogJSONFields {
	n := asset.Normalize()
	out := assetCatalogJSONFields{
		Kind:               string(n.Kind),
		Source:             string(n.Source),
		Issuer:             n.Issuer,
		IssuerName:         n.IssuerName,
		UnderlyingID:       n.UnderlyingID,
		UiAmountMultiplier: uiAmountMultiplierForAsset(n),
		Paused:             n.Paused,
		TokenDecimals:      n.Decimals,
		Sector:             strings.TrimSpace(n.Sector),
		LogoURL:            strings.TrimSpace(n.LogoURL),
		AlwaysOpen:         n.Kind == xstocks.AssetKindPreIPO,
		Holders:            n.Holders,
		VariantCount:       variantCount,
	}
	if price != nil {
		applyJupiterReference(&out, *price)
	}
	return out
}

func applyJupiterReference(fields *assetCatalogJSONFields, price jupiter.TokenPrice) {
	if price.StockData == nil || !price.StockData.Fresh(referenceFreshness) {
		return
	}
	refMicros := usdToUsdcMicros(price.StockData.Price)
	if refMicros <= 0 {
		return
	}
	fields.ReferenceMarkUsdcMicros = &refMicros
	if price.StockData.Mcap > 0 {
		val := int64(math.Round(price.StockData.Mcap))
		fields.ReferenceValuationUsd = &val
	}
	ts := price.StockData.UpdatedAt.UTC().Format(time.RFC3339)
	fields.ReferenceUpdatedAt = &ts
	if price.PriceUsdcMicros > 0 {
		if bps, ok := premiumBpsFromDexAndReference(price.PriceUsdcMicros, refMicros); ok {
			fields.PremiumBps = &bps
		}
	}
}

func premiumBpsFromDexAndReference(dexMicros, refMicros int64) (int, bool) {
	if dexMicros <= 0 || refMicros <= 0 {
		return 0, false
	}
	// (dex/ref - 1) in basis points, rounded.
	ratio := float64(dexMicros) / float64(refMicros)
	bps := int(math.Round((ratio - 1) * 10_000))
	return bps, true
}

func usdToUsdcMicros(usd float64) int64 {
	if usd <= 0 || usd >= 1e9 {
		return 0
	}
	return int64(math.Round(usd * 1_000_000))
}

func lookupCatalogAssetBySymbol(ctx context.Context, catalog xstocks.CatalogSearcher, symbol string) (xstocks.CatalogAsset, bool) {
	if catalog == nil || strings.TrimSpace(symbol) == "" {
		return xstocks.CatalogAsset{}, false
	}
	page, err := catalog.Search(ctx, symbol, 25, 0)
	if err != nil {
		return xstocks.CatalogAsset{}, false
	}
	for _, asset := range page.Assets {
		if assetMatchesLookup(asset, symbol) {
			return asset.Normalize(), true
		}
	}
	return xstocks.CatalogAsset{}, false
}

func assetKindForSymbol(ctx context.Context, catalog xstocks.CatalogSearcher, symbol string) string {
	if asset, ok := lookupCatalogAssetBySymbol(ctx, catalog, symbol); ok {
		return string(asset.Kind)
	}
	return string(xstocks.AssetKindStock)
}

func variantResponses(ctx context.Context, catalog xstocks.CatalogSearcher, price jupiter.PriceClient, asset xstocks.CatalogAsset) []assetVariantResponse {
	underlying := strings.TrimSpace(asset.UnderlyingID)
	if underlying == "" {
		return nil
	}
	vs, ok := catalog.(catalogVariantsSearcher)
	if !ok {
		return nil
	}
	variants, err := vs.SearchVariants(ctx, underlying)
	if err != nil || len(variants) == 0 {
		return nil
	}
	prices := map[string]jupiter.TokenPrice{}
	if price != nil {
		mints := make([]string, 0, len(variants))
		for _, v := range variants {
			if m := strings.TrimSpace(v.SolanaMint); m != "" {
				mints = append(mints, m)
			}
		}
		if len(mints) > 0 {
			if batch, err := price.Prices(ctx, mints); err == nil {
				prices = batch
			}
		}
	}
	out := make([]assetVariantResponse, 0, len(variants))
	for _, v := range variants {
		n := v.Normalize()
		row := assetVariantResponse{
			Symbol:         n.Symbol,
			Issuer:         n.Issuer,
			IssuerName:     n.IssuerName,
			SolanaMint:     n.SolanaMint,
			Routable:       n.Routable,
			TransferFeeBps: n.TransferFeeBps,
			Paused:         n.Paused,
		}
		if p, ok := prices[n.SolanaMint]; ok && p.PriceUsdcMicros > 0 {
			micros := p.PriceUsdcMicros
			row.PriceUsdcMicros = &micros
			if p.LiquidityUsd > 0 {
				row.LiquidityUsd = strconv.FormatFloat(p.LiquidityUsd, 'f', -1, 64)
			}
		}
		out = append(out, row)
	}
	markBestPriceVariant(ctx, catalog, underlying, out)
	return out
}

func markBestPriceVariant(ctx context.Context, searcher xstocks.CatalogSearcher, underlying string, rows []assetVariantResponse) {
	cmp, ok := searcher.(interface {
		Compare(ctx context.Context, underlyingID string) (catalog.Comparison, error)
	})
	if !ok || len(rows) < 2 {
		return
	}
	comparison, err := cmp.Compare(ctx, underlying)
	if err != nil || comparison.Basis != "price_v3" || comparison.ChosenSymbol == "" {
		return
	}
	ratios := map[string]int64{}
	for _, candidate := range comparison.Candidates {
		ratios[candidate.Symbol] = candidate.CostRatioBps
	}
	for i := range rows {
		rows[i].CostRatioBps = ratios[rows[i].Symbol]
		rows[i].BestPrice = rows[i].Symbol == comparison.ChosenSymbol
	}
}
