package prestocks

import (
	"net/url"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const (
	prestocksDecimals       = 9
	prestocksTransferFeeBps = 100
)

type apiRow struct {
	Name             string  `json:"name"`
	Symbol           string  `json:"symbol"`
	Description      string  `json:"description"`
	Image            string  `json:"image"`
	ExternalURL      string  `json:"external_url"`
	ContractAddress  string  `json:"contract_address"`
	MarkPrice        float64 `json:"markPrice"`
	MarkValuation    int64   `json:"markValuation"`
	TokenPrice       float64 `json:"tokenPrice"`
	ImpliedValuation int64   `json:"impliedValuation"`
}

// StaticFallback returns the eight known PreStocks mints when the API is unavailable.
func StaticFallback() []xstocks.CatalogAsset {
	return []xstocks.CatalogAsset{
		staticRow("ANDURIL", "Anduril", "PresTj4Yc2bAR197Er7wz4UUKSfqt6FryBEdAriBoQB", "anduril"),
		staticRow("ANTHROPIC", "Anthropic", "Pren1FvFX6J3E4kXhJuCiAD5aDmGEb7qJRncwA8Lkhw", "anthropic"),
		staticRow("FIGUREAI", "Figure AI", "PreZad18qfPtbxNpMtMuAuX2zVpvkEU8DnJx56faCWd", "figureai"),
		staticRow("KALSHI", "Kalshi", "PreLWGkkeqG1s4HEfFZSy9moCrJ7btsHuUtfcCeoRua", "kalshi"),
		staticRow("NEURALINK", "Neuralink", "PrekqLJvJ3qVdXmBGDiexvwUTF4rLFDa6HWS4HJbw9S", "neuralink"),
		staticRow("OPENAI", "OpenAI", "PreweJYECqtQwBtpxHL171nL2K6umo692gTm7Q3rpgF", "openai"),
		staticRow("POLYMARKET", "Polymarket", "Pre8AREmFPtoJFT8mQSXQLh56cwJmM7CFDRuoGBZiUP", "polymarket"),
		staticRow("SPACEX", "SpaceX", "PreANxuXjsy2pvisWWMNB6YaJNzr7681wJJr2rHsfTh", "spacex"),
	}
}

func staticRow(symbol, name, mint, underlyingID string) xstocks.CatalogAsset {
	return xstocks.CatalogAsset{
		Symbol:         symbol,
		Name:           name,
		SolanaMint:     mint,
		Kind:           xstocks.AssetKindPreIPO,
		Source:         xstocks.AssetSourcePreStocks,
		Issuer:         "prestocks",
		UnderlyingID:   underlyingID,
		Decimals:       prestocksDecimals,
		TransferFeeBps: prestocksTransferFeeBps,
		Sector:         "",
	}
}

func catalogAssetsFromRows(rows []apiRow) []xstocks.CatalogAsset {
	out := make([]xstocks.CatalogAsset, 0, len(rows))
	for _, row := range rows {
		if asset, ok := catalogAssetFromRow(row); ok {
			out = append(out, asset)
		}
	}
	return out
}

func catalogAssetFromRow(row apiRow) (xstocks.CatalogAsset, bool) {
	symbol := strings.TrimSpace(row.Symbol)
	mint := strings.TrimSpace(row.ContractAddress)
	if symbol == "" || !isValidSolanaMint(mint) {
		return xstocks.CatalogAsset{}, false
	}

	name := strings.TrimSpace(row.Name)
	name = strings.TrimSuffix(name, " PreStocks")

	return xstocks.CatalogAsset{
		Symbol:         symbol,
		Name:           name,
		SolanaMint:     mint,
		Kind:           xstocks.AssetKindPreIPO,
		Source:         xstocks.AssetSourcePreStocks,
		Issuer:         "prestocks",
		UnderlyingID:   underlyingIDFromExternalURL(row.ExternalURL),
		Decimals:       prestocksDecimals,
		TransferFeeBps: prestocksTransferFeeBps,
		LogoURL:        strings.TrimSpace(row.Image),
		Sector:         "",
	}, true
}

func underlyingIDFromExternalURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return ""
	}
	seg := path[strings.LastIndex(path, "/")+1:]
	return strings.ToLower(seg)
}
