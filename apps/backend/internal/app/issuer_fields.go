package app

import (
	"context"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// IssuerFields holds display metadata for a catalog row.
type IssuerFields struct {
	Issuer     string
	IssuerName string
}

func issuerFieldsFromAsset(asset xstocks.CatalogAsset) IssuerFields {
	n := asset.Normalize()
	if n.Issuer != "" || n.IssuerName != "" {
		return IssuerFields{Issuer: n.Issuer, IssuerName: n.IssuerName}
	}
	if n.Source == xstocks.AssetSourceXStocks || n.Kind == xstocks.AssetKindStock {
		return IssuerFields{Issuer: "xstocks", IssuerName: "xStocks"}
	}
	return IssuerFields{}
}

func (r *SymbolResolver) IssuerFieldsForSymbol(ctx context.Context, symbol string) IssuerFields {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" || r == nil || r.catalog == nil {
		return IssuerFields{}
	}
	page, err := r.catalog.Search(ctx, symbol, 20, 0)
	if err != nil {
		return IssuerFields{}
	}
	for _, asset := range page.Assets {
		if strings.EqualFold(strings.TrimSpace(asset.Symbol), symbol) {
			return issuerFieldsFromAsset(asset)
		}
	}
	return IssuerFields{}
}

func (r *SymbolResolver) IssuerFieldsForMint(ctx context.Context, mint string) IssuerFields {
	mint = strings.TrimSpace(mint)
	if mint == "" {
		return IssuerFields{}
	}
	if r != nil && r.catalog != nil {
		if asset, found, err := r.catalog.LookupByMint(ctx, mint); err == nil && found {
			return issuerFieldsFromAsset(asset)
		}
	}
	return IssuerFields{}
}
