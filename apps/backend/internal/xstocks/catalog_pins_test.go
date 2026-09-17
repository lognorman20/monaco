package xstocks

import "testing"

func TestSortCatalogMatches_pinsKnownSymbolsFirst(t *testing.T) {
	t.Parallel()

	matches := []CatalogAsset{
		{Symbol: "ZZZx", Name: "Zzz"},
		{Symbol: "TSLAx", Name: "Tesla"},
		{Symbol: "YYYx", Name: "Yyy"},
		{Symbol: "AAPLx", Name: "Apple"},
	}
	sortCatalogMatches(matches)

	want := []string{"AAPLx", "TSLAx", "ZZZx", "YYYx"}
	for i, sym := range want {
		if matches[i].Symbol != sym {
			t.Fatalf("matches[%d] = %q, want %q", i, matches[i].Symbol, sym)
		}
	}
}
