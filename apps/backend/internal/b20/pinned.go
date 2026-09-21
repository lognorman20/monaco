package b20

import (
	"context"
	"fmt"
	"strings"
)

// pinnedAssets from Base docs (B20 tokenized stocks + Chainlink TRV feeds).
var pinnedAssets = []Asset{
	{Symbol: "AAPLc", Name: "Apple", TokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb", FeedAddress: "0x787f13dea48db0897cbcdd985de77809d837f988", Decimals: 8},
	{Symbol: "AMZNc", Name: "Amazon", TokenAddress: "0xb200000000000000000000d9192b6b456483c2e8", FeedAddress: "0x06a8e4b3abb3b7543d8396fb2b763d22820cb295", Decimals: 8},
	{Symbol: "COINc", Name: "Coinbase", TokenAddress: "0xb200000000000000000000c85a31389d71f3ecfb", FeedAddress: "0x408e44f504a7371a345f03a73ddc96a4b48e8aa7", Decimals: 8},
	{Symbol: "CRCLc", Name: "Circle", TokenAddress: "0xb20000000000000000000019f6e7c675b73c2e4d", FeedAddress: "0x0231cf2635d1e17bb5c2462cc7504ba1fbd61f33", Decimals: 8},
	{Symbol: "GOOGLc", Name: "Alphabet", TokenAddress: "0xb2000000000000000000002d0ba3164cc74f58b7", FeedAddress: "0x5bf49e0ffa937ce2fff033c739ad7c634c4d34f2", Decimals: 8},
	{Symbol: "INTCc", Name: "Intel", TokenAddress: "0xb2000000000000000000004aff16039ba04bdfbc", FeedAddress: "0xab657c39bac0d5886250d70849e2e3e008f2eecb", Decimals: 8},
	{Symbol: "METAc", Name: "Meta", TokenAddress: "0xb2000000000000000000008bc8786b856e61707c", FeedAddress: "0x6526ae6797a76123638b863aee4dd27ba4e4b27d", Decimals: 8},
	{Symbol: "MSFTc", Name: "Microsoft", TokenAddress: "0xb200000000000000000000ab99cfa739e253872b", FeedAddress: "0xeb10a6c9aa7e537aed766c08c35dae35b321b18c", Decimals: 8},
	{Symbol: "MSTRc", Name: "MicroStrategy", TokenAddress: "0xb2000000000000000000004884b426556b92883d", FeedAddress: "0xb3ce282cd188b35da0e38d8bc7d58e33173d202a", Decimals: 8},
	{Symbol: "NVDAc", Name: "NVIDIA", TokenAddress: "0xb20000000000000000000078ee7ce2fe4908108c", FeedAddress: "0x04689a41629776563e6822f76f2e57d148d28513", Decimals: 8},
	{Symbol: "SNDKc", Name: "SanDisk", TokenAddress: "0xb200000000000000000000397293cb8cda9a10c5", FeedAddress: "0x388b0dc46c0fb05a74bee0994fa5b02c6fcca2ea", Decimals: 8},
	{Symbol: "SPCXc", Name: "SpaceX", TokenAddress: "0xb2000000000000000000007b9fcbd005511acbd5", FeedAddress: "0x6a634b235903c4ad6376892180d6ff8612e3fa68", Decimals: 8},
	{Symbol: "TSLAc", Name: "Tesla", TokenAddress: "0xb2000000000000000000001e800a7f5189430cd0", FeedAddress: "0xfaf869185383a24f8cb00e27bda6b63b9905dcb4", Decimals: 8},
}

type pinnedCatalog struct {
	bySymbol map[string]Asset
	byAddr   map[string]Asset
}

// NewPinnedCatalog returns the pinned B20 catalog.
func NewPinnedCatalog() Catalog {
	c := &pinnedCatalog{
		bySymbol: make(map[string]Asset),
		byAddr:   make(map[string]Asset),
	}
	for _, a := range pinnedAssets {
		addr := strings.ToLower(a.TokenAddress)
		a.TokenAddress = addr
		a.Routable = true
		c.bySymbol[strings.ToUpper(a.Symbol)] = a
		c.byAddr[addr] = a
	}
	return c
}

func (c *pinnedCatalog) lookup(symbol string) (Asset, bool) {
	key := strings.ToUpper(strings.TrimSpace(symbol))
	if a, ok := c.bySymbol[key]; ok {
		return a, true
	}
	if len(key) > 1 && strings.HasSuffix(key, "X") {
		if a, ok := c.bySymbol[key[:len(key)-1]+"C"]; ok {
			return a, true
		}
	}
	return Asset{}, false
}

func (c *pinnedCatalog) ResolveTokenAddress(ctx context.Context, symbol string) (string, error) {
	_ = ctx
	a, ok := c.lookup(symbol)
	if !ok {
		return "", fmt.Errorf("%w: unknown symbol %q", ErrNotFound, symbol)
	}
	return a.TokenAddress, nil
}

func (c *pinnedCatalog) Search(ctx context.Context, query string, limit, offset int) (SearchPage, error) {
	_ = ctx
	q := strings.ToLower(strings.TrimSpace(query))
	aliased, hasAlias := c.lookup(query)
	var matches []Asset
	for _, a := range pinnedAssets {
		sym := strings.ToLower(a.Symbol)
		name := strings.ToLower(a.Name)
		if q == "" || strings.Contains(sym, q) || strings.Contains(name, q) || (hasAlias && a.Symbol == aliased.Symbol) {
			a.Routable = true
			a.TokenAddress = strings.ToLower(a.TokenAddress)
			matches = append(matches, a)
		}
	}
	if offset > len(matches) {
		return SearchPage{}, nil
	}
	end := offset + limit
	if limit <= 0 || end > len(matches) {
		end = len(matches)
	}
	page := matches[offset:end]
	return SearchPage{Assets: page, HasMore: end < len(matches)}, nil
}

func (c *pinnedCatalog) LookupByAddress(ctx context.Context, addr string) (Asset, bool, error) {
	_ = ctx
	a, ok := c.byAddr[strings.ToLower(strings.TrimSpace(addr))]
	return a, ok, nil
}

func (c *pinnedCatalog) Popular(ctx context.Context) ([]Asset, error) {
	_ = ctx
	order := []string{"AAPLc", "NVDAc", "TSLAc", "MSFTc", "AMZNc", "GOOGLc", "METAc", "COINc"}
	out := make([]Asset, 0, len(order))
	for _, sym := range order {
		if a, ok := c.bySymbol[strings.ToUpper(sym)]; ok {
			a.Routable = true
			out = append(out, a)
		}
	}
	return out, nil
}

func (c *pinnedCatalog) Feed(ctx context.Context, symbol string) (string, error) {
	_ = ctx
	a, ok := c.lookup(symbol)
	if !ok {
		return "", fmt.Errorf("%w: unknown symbol %q", ErrNotFound, symbol)
	}
	return a.FeedAddress, nil
}
