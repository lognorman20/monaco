package catalog

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// JupiterNetsTransferFee controls whether transfer fees adjust the cost-ratio ranking.
// While true, fees are omitted from the ratio (list and live quote must stay aligned).
var JupiterNetsTransferFee = true

const (
	bestPriceCacheTTL    = 5 * time.Minute
	referenceFreshMax    = 48 * time.Hour
	costRatioTieBreakBps = 25
	mcapMismatchFrac     = 0.02
)

// Comparison summarizes how the default pre-IPO variant was chosen for one company.
type Comparison struct {
	Basis        string // "price_v3" | "live_quote" | "unavailable" | "single"
	ChosenSymbol string
	Candidates   []ComparisonCandidate
}

// ComparisonCandidate is one routable or excluded variant in a price comparison.
type ComparisonCandidate struct {
	Symbol             string
	Issuer             string
	IssuerName         string
	SolanaMint         string
	CostRatioBps       int64
	ExposureUsdcMicros int64 // live_quote basis
	DeltaBps           int64
	Reason             string // "" | "issuer_paused" | "no_route" | "reference_unavailable"
}

type comparisonCacheEntry struct {
	comparison Comparison
	expiresAt  time.Time
}

type variantScore struct {
	asset        xstocks.CatalogAsset
	costRatio    float64
	costRatioBps int64
	liquidity    float64
	reason       string
	eligible     bool
}

// Compare ranks every variant for underlyingID and returns the best-price comparison.
func (c *Composite) Compare(ctx context.Context, underlyingID string) (Comparison, error) {
	underlyingID = strings.ToLower(strings.TrimSpace(underlyingID))
	if underlyingID == "" {
		return Comparison{}, nil
	}
	if cached, ok := c.cachedComparison(underlyingID); ok {
		return cached, nil
	}

	variants, err := c.SearchVariants(ctx, underlyingID)
	if err != nil {
		return Comparison{}, err
	}
	cmp := c.compareVariants(ctx, underlyingID, variants)
	c.storeComparison(underlyingID, cmp)
	return cmp, nil
}

func (c *Composite) compareVariants(ctx context.Context, underlyingID string, variants []xstocks.CatalogAsset) Comparison {
	if len(variants) == 0 {
		return Comparison{}
	}
	if len(variants) == 1 {
		v := variants[0]
		return Comparison{
			Basis:        "single",
			ChosenSymbol: v.Symbol,
			Candidates: []ComparisonCandidate{{
				Symbol:     v.Symbol,
				Issuer:     v.Issuer,
				IssuerName: v.IssuerName,
				SolanaMint: v.SolanaMint,
			}},
		}
	}

	scores := c.scoreVariants(ctx, variants)
	eligible := make([]variantScore, 0, len(scores))
	for _, s := range scores {
		if s.eligible {
			eligible = append(eligible, s)
		}
	}

	chosen := selectDefaultVariant(variants)
	cmp := Comparison{
		Basis:        "unavailable",
		ChosenSymbol: chosen.Symbol,
		Candidates:   comparisonCandidatesFromScores(scores, chosen.Symbol, 0),
	}

	if len(eligible) == 0 {
		return cmp
	}

	if c.prices == nil {
		return cmp
	}

	mints := make([]string, 0, len(eligible))
	for _, s := range eligible {
		mints = append(mints, s.asset.SolanaMint)
	}
	prices, err := c.prices.Prices(ctx, mints)
	if err != nil {
		return cmp
	}

	for i := range eligible {
		price, ok := prices[eligible[i].asset.SolanaMint]
		if !ok || price.StockData == nil || !price.StockData.Fresh(referenceFreshMax) {
			return cmp
		}
		usd := tokenUsdPrice(price)
		if usd <= 0 || price.StockData.Price <= 0 {
			return cmp
		}
		ratio := usd / price.StockData.Price
		if !JupiterNetsTransferFee {
			fee := eligible[i].asset.TransferFeeBps
			if fee > 0 && fee < 10000 {
				ratio /= 1 - float64(fee)/10000
			}
		}
		eligible[i].costRatio = ratio
		eligible[i].costRatioBps = int64(math.Round(ratio * 10000))
		eligible[i].liquidity = price.LiquidityUsd
	}

	if !mcapsAligned(prices, eligible) {
		return cmp
	}

	sort.Slice(eligible, func(i, j int) bool {
		a, b := eligible[i], eligible[j]
		if a.costRatioBps != b.costRatioBps {
			return a.costRatioBps < b.costRatioBps
		}
		return a.liquidity > b.liquidity
	})

	winner := eligible[0]
	if len(eligible) > 1 {
		second := eligible[1]
		if second.costRatioBps-winner.costRatioBps <= costRatioTieBreakBps &&
			second.liquidity > winner.liquidity {
			winner = second
		}
	}

	for i := range scores {
		for _, e := range eligible {
			if scores[i].asset.SolanaMint == e.asset.SolanaMint {
				scores[i].costRatioBps = e.costRatioBps
				scores[i].costRatio = e.costRatio
				scores[i].liquidity = e.liquidity
				break
			}
		}
	}

	return Comparison{
		Basis:        "price_v3",
		ChosenSymbol: winner.asset.Symbol,
		Candidates:   comparisonCandidatesFromScores(scores, winner.asset.Symbol, winner.costRatioBps),
	}
}

func (c *Composite) scoreVariants(ctx context.Context, variants []xstocks.CatalogAsset) []variantScore {
	out := make([]variantScore, 0, len(variants))
	for _, asset := range variants {
		asset = c.enrichAsset(ctx, asset)
		s := variantScore{asset: asset}
		if asset.Paused {
			s.reason = "issuer_paused"
		} else if !asset.Routable {
			s.reason = "no_route"
		} else {
			s.eligible = true
		}
		out = append(out, s)
	}
	return out
}

func comparisonCandidatesFromScores(scores []variantScore, chosenSymbol string, chosenRatioBps int64) []ComparisonCandidate {
	out := make([]ComparisonCandidate, 0, len(scores))
	for _, s := range scores {
		delta := int64(0)
		if s.asset.Symbol != chosenSymbol && chosenRatioBps > 0 && s.costRatioBps > 0 {
			delta = s.costRatioBps - chosenRatioBps
			if delta < 0 {
				delta = 0
			}
		}
		reason := s.reason
		if s.eligible && s.costRatioBps == 0 && chosenRatioBps == 0 {
			reason = "reference_unavailable"
		}
		out = append(out, ComparisonCandidate{
			Symbol:       s.asset.Symbol,
			Issuer:       s.asset.Issuer,
			IssuerName:   s.asset.IssuerName,
			SolanaMint:   s.asset.SolanaMint,
			CostRatioBps: s.costRatioBps,
			DeltaBps:     delta,
			Reason:       reason,
		})
	}
	return out
}

func mcapsAligned(prices map[string]jupiter.TokenPrice, eligible []variantScore) bool {
	var mcaps []float64
	for _, s := range eligible {
		p, ok := prices[s.asset.SolanaMint]
		if !ok || p.StockData == nil || p.StockData.Mcap <= 0 {
			return false
		}
		mcaps = append(mcaps, p.StockData.Mcap)
	}
	if len(mcaps) < 2 {
		return true
	}
	min, max := mcaps[0], mcaps[0]
	for _, m := range mcaps[1:] {
		if m < min {
			min = m
		}
		if m > max {
			max = m
		}
	}
	if max <= 0 {
		return false
	}
	return (max-min)/max <= mcapMismatchFrac
}

func tokenUsdPrice(tp jupiter.TokenPrice) float64 {
	return float64(tp.PriceUsdcMicros) / 1_000_000
}

func (c *Composite) cachedComparison(underlyingID string) (Comparison, bool) {
	c.comparisonMu.Lock()
	defer c.comparisonMu.Unlock()
	entry, ok := c.comparisonCache[underlyingID]
	if !ok || time.Now().After(entry.expiresAt) {
		return Comparison{}, false
	}
	return entry.comparison, true
}

func (c *Composite) storeComparison(underlyingID string, cmp Comparison) {
	c.comparisonMu.Lock()
	defer c.comparisonMu.Unlock()
	if c.comparisonCache == nil {
		c.comparisonCache = make(map[string]comparisonCacheEntry)
	}
	c.comparisonCache[underlyingID] = comparisonCacheEntry{
		comparison: cmp,
		expiresAt:  time.Now().Add(bestPriceCacheTTL),
	}
}

func (c *Composite) pickDefaultVariantByCompare(ctx context.Context, underlyingID string, variants []xstocks.CatalogAsset) xstocks.CatalogAsset {
	cmp, err := c.Compare(ctx, underlyingID)
	if err != nil || cmp.ChosenSymbol == "" {
		return selectDefaultVariant(variants)
	}
	for _, v := range variants {
		if v.Symbol == cmp.ChosenSymbol {
			return v
		}
	}
	return selectDefaultVariant(variants)
}
