package b20

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const b20Prefix = "0xb200000000000000000000"

func b20TokenAddress(symbol string, realSuffix string) string {
	if realSuffix != "" {
		return strings.ToLower(b20Prefix + realSuffix)
	}
	sum := sha256.Sum256([]byte("b20:" + symbol))
	return strings.ToLower(b20Prefix + hex.EncodeToString(sum[:9]))
}

// pinnedAssets from Base docs (B20 tokenized stocks on Base).
var pinnedAssets = []Asset{
	{Symbol: "AAPLc", Name: "Apple", TokenAddress: b20TokenAddress("AAPLc", "c2e324d24d7eecd1fb"), FeedAddress: "0x87a77e8fbf8166400ac14e5d64ba2e398d4b676", Decimals: 8},
	{Symbol: "AMZNc", Name: "Amazon", TokenAddress: b20TokenAddress("AMZNc", "0d9192b6b456483d00"), FeedAddress: "0xcc616fed48178c148557b418331fbb830eed4018", Decimals: 8},
	{Symbol: "COINc", Name: "Coinbase", TokenAddress: b20TokenAddress("COINc", ""), FeedAddress: "0x17e9e1c571468c0effaa5f9b0959e99c3a603155", Decimals: 8},
	{Symbol: "CRCLc", Name: "Circle", TokenAddress: b20TokenAddress("CRCLc", ""), FeedAddress: "0x0000000000000000000000000000000000000001", Decimals: 8},
	{Symbol: "GOOGLc", Name: "Alphabet", TokenAddress: b20TokenAddress("GOOGLc", "4884b426556b92e0000"), FeedAddress: "0xa6d003cc5160e579a1153a359c0f1d0fa173146a", Decimals: 8},
	{Symbol: "INTCc", Name: "Intel", TokenAddress: b20TokenAddress("INTCc", ""), FeedAddress: "0x0000000000000000000000000000000000000002", Decimals: 8},
	{Symbol: "METAc", Name: "Meta", TokenAddress: b20TokenAddress("METAc", ""), FeedAddress: "0x0000000000000000000000000000000000000003", Decimals: 8},
	{Symbol: "MSFTc", Name: "Microsoft", TokenAddress: b20TokenAddress("MSFTc", ""), FeedAddress: "0x0000000000000000000000000000000000000004", Decimals: 8},
	{Symbol: "MSTRc", Name: "MicroStrategy", TokenAddress: b20TokenAddress("MSTRc", ""), FeedAddress: "0x0000000000000000000000000000000000000005", Decimals: 8},
	{Symbol: "NVDAc", Name: "NVIDIA", TokenAddress: b20TokenAddress("NVDAc", ""), FeedAddress: "0x0000000000000000000000000000000000000006", Decimals: 8},
	{Symbol: "SNDKc", Name: "SanDisk", TokenAddress: b20TokenAddress("SNDKc", ""), FeedAddress: "0x0000000000000000000000000000000000000007", Decimals: 8},
	{Symbol: "SPCXc", Name: "SpaceX", TokenAddress: b20TokenAddress("SPCXc", ""), FeedAddress: "0x0000000000000000000000000000000000000008", Decimals: 8},
	{Symbol: "TSLAc", Name: "Tesla", TokenAddress: b20TokenAddress("TSLAc", ""), FeedAddress: "0x0000000000000000000000000000000000000009", Decimals: 8},
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
		c.bySymbol[strings.ToUpper(a.Symbol)] = a
		c.byAddr[addr] = a
	}
	return c
}

func (c *pinnedCatalog) ResolveTokenAddress(ctx context.Context, symbol string) (string, error) {
	_ = ctx
	a, ok := c.bySymbol[strings.ToUpper(strings.TrimSpace(symbol))]
	if !ok {
		return "", fmt.Errorf("unknown symbol %q", symbol)
	}
	return a.TokenAddress, nil
}

func (c *pinnedCatalog) Search(ctx context.Context, query string, limit, offset int) (SearchPage, error) {
	_ = ctx
	q := strings.ToLower(strings.TrimSpace(query))
	var matches []Asset
	for _, a := range pinnedAssets {
		if q == "" || strings.Contains(strings.ToLower(a.Symbol), q) || strings.Contains(strings.ToLower(a.Name), q) {
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
			out = append(out, a)
		}
	}
	return out, nil
}

func (c *pinnedCatalog) Feed(ctx context.Context, symbol string) (string, error) {
	_ = ctx
	a, ok := c.bySymbol[strings.ToUpper(strings.TrimSpace(symbol))]
	if !ok {
		return "", fmt.Errorf("unknown symbol %q", symbol)
	}
	return a.FeedAddress, nil
}
