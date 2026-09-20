package b20

import (
	"context"
	"strings"
	"testing"
)

func TestPinnedCatalog_containsAAPLcWithFeed(t *testing.T) {
	c := NewPinnedCatalog()
	addr, err := c.ResolveTokenAddress(context.Background(), "AAPLc")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(addr, "0x") {
		t.Fatalf("token %s", addr)
	}
	feed, err := c.Feed(context.Background(), "AAPLc")
	if err != nil || feed == "" {
		t.Fatalf("feed %q err %v", feed, err)
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
