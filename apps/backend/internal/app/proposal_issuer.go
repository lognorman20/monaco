package app

import (
	"context"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func (g *GovernanceService) issuerFieldsForProposalSymbol(ctx context.Context, symbol string) IssuerFields {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" || g == nil || g.buy == nil {
		return IssuerFields{}
	}
	asset, err := g.buy.ResolveAsset(ctx, symbol)
	if err != nil {
		return IssuerFields{}
	}
	n := asset.Normalize()
	if n.Kind != xstocks.AssetKindPreIPO {
		return IssuerFields{}
	}
	return issuerFieldsFromAsset(n)
}
