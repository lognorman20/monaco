package xstocks

import (
	"sort"
	"strings"
)

// pinnedCatalogSymbols lists well-known xStock tickers shown first when browsing
// or when they match a search query. Order is display priority.
var pinnedCatalogSymbols = []string{
	"AAPLx",  // Apple
	"MSFTx",  // Microsoft
	"GOOGLx", // Alphabet (Class A)
	"GOOGx",  // Alphabet (Class C)
	"AMZNx",  // Amazon
	"NVDAx",  // NVIDIA
	"METAx",  // Meta
	"TSLAx",  // Tesla
	"NFLXx",  // Netflix
	"COINx",  // Coinbase
	"JPMx",   // JPMorgan
	"DISx",   // Disney
	"WMTx",   // Walmart
	"AMDx",   // AMD
	"INTCx",  // Intel
	"PYPLx",  // PayPal
	"CRMx",   // Salesforce
	"ORCLx",  // Oracle
}

var pinnedCatalogRank = func() map[string]int {
	ranks := make(map[string]int, len(pinnedCatalogSymbols))
	for i, symbol := range pinnedCatalogSymbols {
		ranks[strings.ToUpper(symbol)] = i
	}
	return ranks
}()

func pinnedCatalogOrder(symbol string) (rank int, pinned bool) {
	rank, pinned = pinnedCatalogRank[strings.ToUpper(strings.TrimSpace(symbol))]
	return rank, pinned
}

// sortCatalogMatches orders by routability, pinned popularity proxy, then symbol.
func sortCatalogMatches(matches []CatalogAsset) {
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Routable != matches[j].Routable {
			return matches[i].Routable
		}
		ri, iPinned := pinnedCatalogOrder(matches[i].Symbol)
		rj, jPinned := pinnedCatalogOrder(matches[j].Symbol)
		if iPinned && jPinned {
			return ri < rj
		}
		if iPinned != jPinned {
			return iPinned
		}
		si := strings.ToLower(strings.TrimSpace(matches[i].Symbol))
		sj := strings.ToLower(strings.TrimSpace(matches[j].Symbol))
		return si < sj
	})
}
