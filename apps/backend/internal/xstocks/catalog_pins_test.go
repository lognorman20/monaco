package xstocks

import "testing"

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
