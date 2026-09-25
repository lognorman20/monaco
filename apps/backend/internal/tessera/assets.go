package tessera

import (
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const (
	tesseraDecimals       = 9
	tesseraTransferFeeBps = 20
)

// StaticFallback returns the three known Tessera mints when the API is unavailable.
func StaticFallback() []xstocks.CatalogAsset {
	return []xstocks.CatalogAsset{
		staticRow("tSpaceX", "T-SpaceX", "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v", "spacex", "Aerospace"),
		staticRow("tKalshi", "T-Kalshi", "TKLSidmLVt3cqGaaodG8tyRzoANfQwoh67AccjmubeZ", "kalshi", "Prediction Markets"),
		staticRow("tOpenAI", "T-OpenAI", "oPAiAikWTaFj9RYoRFD35ccfwhnMcB3ThgBZRHSkjTZ", "openai", "Artificial Intelligence"),
	}
}

func staticRow(symbol, name, mint, underlyingID, sector string) xstocks.CatalogAsset {
	return xstocks.CatalogAsset{
		Symbol:         symbol,
		Name:           name,
		SolanaMint:     mint,
		Kind:           xstocks.AssetKindPreIPO,
		Source:         xstocks.AssetSourceTessera,
		Issuer:         string(xstocks.AssetSourceTessera),
		UnderlyingID:   underlyingID,
		Decimals:       tesseraDecimals,
		TransferFeeBps: tesseraTransferFeeBps,
		Sector:         sector,
	}
}

func underlyingIDFromName(name string) string {
	s := strings.TrimSpace(name)
	s = strings.TrimPrefix(s, "T-")
	return strings.ToLower(s)
}
