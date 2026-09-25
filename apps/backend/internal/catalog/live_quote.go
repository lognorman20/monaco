package catalog

import (
	"context"
	"math/big"
	"strconv"
	"strings"
	"sync"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const maxLiveQuoteVariants = 8

// LiveQuoteOutAmount is one price-only Jupiter buy quote for variant comparison.
type LiveQuoteOutAmount struct {
	Asset     xstocks.CatalogAsset
	OutAmount string
}

// ReferenceExposureUsdcMicros is dollars of reference exposure from a live buy quote:
// outAmount × multiplier / 10^decimals × stockData.price, adjusted for transfer fee when configured.
func ReferenceExposureUsdcMicros(outAmount int64, decimals int, mult *big.Rat, stockPriceUsd float64, transferFeeBps int) (int64, bool) {
	if outAmount <= 0 || stockPriceUsd <= 0 || decimals < 0 {
		return 0, false
	}
	m := mult
	if m == nil {
		m = big.NewRat(1, 1)
	}
	if m.Sign() <= 0 {
		return 0, false
	}
	scale := jupiter.AtomicScale(decimals)
	num := new(big.Int).SetInt64(outAmount)
	num.Mul(num, m.Num())
	den := new(big.Int).Mul(new(big.Int).SetInt64(scale), m.Denom())
	if den.Sign() <= 0 {
		return 0, false
	}
	scaled := new(big.Rat).SetFrac(num, den)
	exposure := new(big.Float).SetRat(scaled)
	exposure.Mul(exposure, big.NewFloat(stockPriceUsd))
	if !JupiterNetsTransferFee && transferFeeBps > 0 && transferFeeBps < 10000 {
		feeFactor := 1 - float64(transferFeeBps)/10000
		exposure.Mul(exposure, big.NewFloat(feeFactor))
	}
	microsFloat := new(big.Float).Mul(exposure, big.NewFloat(1_000_000))
	micros, _ := microsFloat.Int64()
	if micros <= 0 {
		return 0, false
	}
	return micros, true
}

// PickBuyVariantLive compares live quotes for every eligible variant of a pre-IPO company.
// When picked is false, callers should quote requestedSymbol and use Basis unavailable (or single).
func (c *Composite) PickBuyVariantLive(
	ctx context.Context,
	resolved xstocks.CatalogAsset,
	requestedSymbol string,
	usdcMicros int64,
	quoteFn func(ctx context.Context, asset xstocks.CatalogAsset) (outAmount string, err error),
) (chosen xstocks.CatalogAsset, cmp Comparison, picked bool) {
	requestedSymbol = strings.TrimSpace(requestedSymbol)
	resolved = c.enrichAsset(ctx, resolved.Normalize())
	chosen = resolved

	underlyingID := strings.ToLower(strings.TrimSpace(resolved.UnderlyingID))
	if underlyingID == "" || resolved.Kind != xstocks.AssetKindPreIPO {
		return chosen, Comparison{}, false
	}

	variants, err := c.SearchVariants(ctx, underlyingID)
	if err != nil || len(variants) <= 1 {
		if len(variants) == 1 {
			v := c.enrichAsset(ctx, variants[0].Normalize())
			return v, Comparison{
				Basis:        "single",
				ChosenSymbol: v.Symbol,
				Candidates: []ComparisonCandidate{{
					Symbol:     v.Symbol,
					Issuer:     v.Issuer,
					IssuerName: v.IssuerName,
					SolanaMint: v.SolanaMint,
				}},
			}, true
		}
		return chosen, Comparison{}, false
	}

	if len(variants) > maxLiveQuoteVariants {
		return chosen, Comparison{
			Basis:        "unavailable",
			ChosenSymbol: requestedSymbol,
		}, false
	}

	eligible := make([]xstocks.CatalogAsset, 0, len(variants))
	scores := make([]variantScore, 0, len(variants))
	for _, v := range variants {
		v = c.enrichAsset(ctx, v.Normalize())
		s := variantScore{asset: v}
		if v.Paused {
			s.reason = "issuer_paused"
		} else if !v.Routable {
			s.reason = "no_route"
		} else {
			s.eligible = true
			eligible = append(eligible, v)
		}
		scores = append(scores, s)
	}

	if len(eligible) <= 1 {
		if len(eligible) == 1 {
			v := eligible[0]
			return v, Comparison{
				Basis:        "single",
				ChosenSymbol: v.Symbol,
				Candidates: []ComparisonCandidate{{
					Symbol:     v.Symbol,
					Issuer:     v.Issuer,
					IssuerName: v.IssuerName,
					SolanaMint: v.SolanaMint,
				}},
			}, true
		}
		return chosen, Comparison{
			Basis:        "unavailable",
			ChosenSymbol: requestedSymbol,
			Candidates:   comparisonCandidatesFromScores(scores, requestedSymbol, 0),
		}, false
	}

	type quoteResult struct {
		asset     xstocks.CatalogAsset
		outAmount string
		err       error
	}
	results := make([]quoteResult, len(eligible))
	var wg sync.WaitGroup
	wg.Add(len(eligible))
	for i, asset := range eligible {
		i, asset := i, asset
		go func() {
			defer wg.Done()
			out, err := quoteFn(ctx, asset)
			results[i] = quoteResult{asset: asset, outAmount: out, err: err}
		}()
	}
	wg.Wait()

	quoted := make([]LiveQuoteOutAmount, 0, len(eligible))
	for _, r := range results {
		if r.err != nil || strings.TrimSpace(r.outAmount) == "" {
			continue
		}
		quoted = append(quoted, LiveQuoteOutAmount{Asset: r.asset, OutAmount: r.outAmount})
	}
	if len(quoted) == 0 {
		return chosen, Comparison{
			Basis:        "unavailable",
			ChosenSymbol: requestedSymbol,
			Candidates:   comparisonCandidatesFromScores(scores, requestedSymbol, 0),
		}, false
	}

	cmp, winner, ok := c.compareLiveQuoteSamples(ctx, requestedSymbol, scores, quoted)
	if !ok {
		return chosen, cmp, false
	}
	return winner, cmp, true
}

func (c *Composite) compareLiveQuoteSamples(
	ctx context.Context,
	requestedSymbol string,
	scores []variantScore,
	quotes []LiveQuoteOutAmount,
) (Comparison, xstocks.CatalogAsset, bool) {
	if c.prices == nil {
		return Comparison{
			Basis:        "unavailable",
			ChosenSymbol: requestedSymbol,
		}, xstocks.CatalogAsset{}, false
	}

	mints := make([]string, 0, len(quotes))
	for _, q := range quotes {
		mints = append(mints, q.Asset.SolanaMint)
	}
	prices, err := c.prices.Prices(ctx, mints)
	if err != nil {
		return Comparison{
			Basis:        "unavailable",
			ChosenSymbol: requestedSymbol,
		}, xstocks.CatalogAsset{}, false
	}

	type exposureScore struct {
		asset          xstocks.CatalogAsset
		exposureMicros int64
	}
	exposures := make([]exposureScore, 0, len(quotes))
	exposureByMint := make(map[string]int64, len(quotes))

	for _, q := range quotes {
		price, ok := prices[q.Asset.SolanaMint]
		if !ok || price.StockData == nil || !price.StockData.Fresh(referenceFreshMax) {
			return Comparison{
				Basis:        "unavailable",
				ChosenSymbol: requestedSymbol,
			}, xstocks.CatalogAsset{}, false
		}
		outAtomics, err := strconv.ParseInt(strings.TrimSpace(q.OutAmount), 10, 64)
		if err != nil || outAtomics <= 0 {
			return Comparison{
				Basis:        "unavailable",
				ChosenSymbol: requestedSymbol,
			}, xstocks.CatalogAsset{}, false
		}
		decimals := q.Asset.Decimals
		if decimals == 0 {
			decimals = jupiter.XStockDecimals
		}
		exposure, ok := ReferenceExposureUsdcMicros(outAtomics, decimals, q.Asset.UiAmountMultiplier, price.StockData.Price, q.Asset.TransferFeeBps)
		if !ok {
			return Comparison{
				Basis:        "unavailable",
				ChosenSymbol: requestedSymbol,
			}, xstocks.CatalogAsset{}, false
		}
		exposures = append(exposures, exposureScore{asset: q.Asset, exposureMicros: exposure})
		exposureByMint[q.Asset.SolanaMint] = exposure
	}

	if len(exposures) == 0 {
		return Comparison{
			Basis:        "unavailable",
			ChosenSymbol: requestedSymbol,
		}, xstocks.CatalogAsset{}, false
	}

	winner := exposures[0]
	for _, e := range exposures[1:] {
		if e.exposureMicros > winner.exposureMicros {
			winner = e
		}
	}

	for i := range scores {
		if exp, ok := exposureByMint[scores[i].asset.SolanaMint]; ok {
			scores[i].costRatioBps = exp
		}
	}

	return Comparison{
		Basis:        "live_quote",
		ChosenSymbol: winner.asset.Symbol,
		Candidates:   liveComparisonCandidates(scores, winner.asset.Symbol, winner.exposureMicros),
	}, winner.asset, true
}

func liveComparisonCandidates(scores []variantScore, chosenSymbol string, chosenExposureMicros int64) []ComparisonCandidate {
	out := make([]ComparisonCandidate, 0, len(scores))
	for _, s := range scores {
		exposure := s.costRatioBps
		delta := int64(0)
		if s.asset.Symbol != chosenSymbol && chosenExposureMicros > 0 && exposure > 0 && exposure < chosenExposureMicros {
			delta = ((chosenExposureMicros - exposure) * 10000) / chosenExposureMicros
		}
		reason := s.reason
		if s.eligible && exposure == 0 && chosenExposureMicros == 0 {
			reason = "reference_unavailable"
		}
		out = append(out, ComparisonCandidate{
			Symbol:             s.asset.Symbol,
			Issuer:             s.asset.Issuer,
			IssuerName:         s.asset.IssuerName,
			SolanaMint:         s.asset.SolanaMint,
			ExposureUsdcMicros: exposure,
			DeltaBps:           delta,
			Reason:             reason,
		})
	}
	return out
}
