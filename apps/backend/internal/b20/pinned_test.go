package b20

import (
	"context"
	"testing"
)

func TestPinnedCatalog_containsAAPLcWithFeed(t *testing.T) {
	c := NewPinnedCatalog()
	addr, err := c.ResolveTokenAddress(context.Background(), "AAPLc")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "0xb200000000000000000000c2e324d24d7eecd1fb" {
		t.Fatalf("token %s", addr)
	}
	feed, err := c.Feed(context.Background(), "AAPLc")
	if err != nil || feed == "" {
		t.Fatalf("feed %q err %v", feed, err)
	}
}

func TestPinnedCatalog_legacyXSuffixResolvesToC(t *testing.T) {
	c := NewPinnedCatalog()
	addr, err := c.ResolveTokenAddress(context.Background(), "AAPLx")
	if err != nil {
		t.Fatal(err)
	}
	want, err := c.ResolveTokenAddress(context.Background(), "AAPLc")
	if err != nil {
		t.Fatal(err)
	}
	if addr != want {
		t.Fatalf("AAPLx token %s, want %s", addr, want)
	}
	page, err := c.Search(context.Background(), "AAPLx", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Assets) == 0 || page.Assets[0].Symbol != "AAPLc" {
		t.Fatalf("search AAPLx = %+v", page.Assets)
	}
}

func TestPinnedCatalog_popularAssetsAreRoutable(t *testing.T) {
	c := NewPinnedCatalog()
	assets, err := c.Popular(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) == 0 || !assets[0].Routable {
		t.Fatalf("popular routable = %+v", assets)
	}
}

func TestPinnedCatalog_Search_caseInsensitive(t *testing.T) {
	c := NewPinnedCatalog()
	page, err := c.Search(context.Background(), "aapl", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Assets) == 0 || page.Assets[0].Symbol != "AAPLc" {
		t.Fatalf("search = %+v", page.Assets)
	}
}
