package xstocks

import "testing"

// TestPinnedCatalogSymbols_noDuplicates guards against a repeat of the "GOOGx" bug:
// a pinned ticker that doesn't exist in the upstream catalog forces every Popular()
// call to fall through to a full paginated catalog scan hunting for a match that
// will never come, adding many seconds of serial latency to /v1/assets/popular.
// This can't be caught by a fake catalog searcher (the cost only shows up against
// the real xstocks.fi API), so this test only guards the input list shape.
func TestPinnedCatalogSymbols_noDuplicates(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, len(pinnedCatalogSymbols))
	for _, symbol := range pinnedCatalogSymbols {
		if seen[symbol] {
			t.Fatalf("duplicate pinned symbol %q", symbol)
		}
		seen[symbol] = true
	}
}

func TestSortCatalogMatches_pinsKnownSymbolsFirst(t *testing.T) {
	t.Parallel()

	matches := []CatalogAsset{
		{Symbol: "ZZZx", Name: "Zzz", Routable: true},
		{Symbol: "TSLAx", Name: "Tesla", Routable: true},
		{Symbol: "YYYx", Name: "Yyy", Routable: true},
		{Symbol: "AAPLx", Name: "Apple", Routable: true},
	}
	sortCatalogMatches(matches)

	want := []string{"AAPLx", "TSLAx", "YYYx", "ZZZx"}
	for i, sym := range want {
		if matches[i].Symbol != sym {
			t.Fatalf("matches[%d] = %q, want %q", i, matches[i].Symbol, sym)
		}
	}
}
