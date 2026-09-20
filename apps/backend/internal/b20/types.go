package b20

import "context"

// Asset is a Coinbase B20 tokenized stock on Base.
type Asset struct {
	Symbol       string
	Name         string
	TokenAddress string
	FeedAddress  string
	Decimals     int
}

// SearchPage is one page of catalog search results.
type SearchPage struct {
	Assets  []Asset
	HasMore bool
}

// Catalog resolves B20 symbols and feeds.
type Catalog interface {
	ResolveTokenAddress(ctx context.Context, symbol string) (string, error)
	Search(ctx context.Context, query string, limit, offset int) (SearchPage, error)
	LookupByAddress(ctx context.Context, addr string) (Asset, bool, error)
	Popular(ctx context.Context) ([]Asset, error)
	Feed(ctx context.Context, symbol string) (string, error)
}
