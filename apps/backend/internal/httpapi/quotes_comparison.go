package httpapi

import (
	"strconv"

	"github.com/monaco/monaco/apps/backend/internal/catalog"
)

func quoteComparisonToJSON(cmp catalog.Comparison) *quoteComparisonResponse {
	if cmp.Basis == "" {
		return nil
	}
	out := &quoteComparisonResponse{Basis: cmp.Basis}
	if len(cmp.Candidates) == 0 {
		return out
	}
	out.Candidates = make([]quoteComparisonCandidateResponse, 0, len(cmp.Candidates))
	for _, c := range cmp.Candidates {
		item := quoteComparisonCandidateResponse{
			Symbol:       c.Symbol,
			Issuer:       c.Issuer,
			IssuerName:   c.IssuerName,
			CostRatioBps: c.CostRatioBps,
			DeltaBps:     c.DeltaBps,
			Reason:       c.Reason,
		}
		if c.ExposureUsdcMicros > 0 {
			item.ExposureUsdcMicros = strconv.FormatInt(c.ExposureUsdcMicros, 10)
		}
		out.Candidates = append(out.Candidates, item)
	}
	return out
}
