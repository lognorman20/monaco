package xstocks

import (
	"context"
	"testing"
	"time"
)

func TestSortCatalogMatches_routableBeforePinned(t *testing.T) {
	t.Parallel()

	matches := []CatalogAsset{
		{Symbol: "AAPLx", Name: "Apple", Routable: false},
		{Symbol: "ZZZx", Name: "Zzz", Routable: true},
		{Symbol: "TSLAx", Name: "Tesla", Routable: true},
	}
	sortCatalogMatches(matches)

	want := []string{"TSLAx", "ZZZx", "AAPLx"}
	for i, sym := range want {
		if matches[i].Symbol != sym {
			t.Fatalf("matches[%d] = %q, want %q", i, matches[i].Symbol, sym)
		}
	}
}

func TestRankCatalogAssets_usesFakeProber(t *testing.T) {
	t.Parallel()

	prober := NewFakeRoutabilityProber(true)
	SetRoutable(prober, "MintDead", false)

	matches := []CatalogAsset{
		{Symbol: "DEADx", Name: "Dead", SolanaMint: "MintDead"},
		{Symbol: "AAPLx", Name: "Apple", SolanaMint: "MintAAPL"},
	}
	rankCatalogAssets(context.Background(), prober, matches)

	if !matches[0].Routable || matches[0].Symbol != "AAPLx" {
		t.Fatalf("first = %+v, want routable AAPLx", matches[0])
	}
	if matches[1].Routable || matches[1].Symbol != "DEADx" {
		t.Fatalf("second = %+v, want non-routable DEADx", matches[1])
	}
}

func TestRoutabilityCache_respectsTTL(t *testing.T) {
	t.Parallel()

	cache := NewRoutabilityCache(10 * time.Millisecond)
	cache.Set("MintA", true)

	if routable, ok := cache.Get("MintA"); !ok || !routable {
		t.Fatalf("Get() = (%v, %v), want (true, true)", routable, ok)
	}

	time.Sleep(15 * time.Millisecond)

	if _, ok := cache.Get("MintA"); ok {
		t.Fatal("expected cache entry to expire")
	}
}
