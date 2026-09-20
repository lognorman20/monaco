package pyth

import (
	"fmt"
	"strings"
	"sync"
)

// FeedMeta is a Hermes feed lookup result. IsOpen is not cached across marks.
type FeedMeta struct {
	ID     string
	IsOpen bool
}

// Feed ids are cached per Hermes query string rather than per symbol, because one
// xStock has two feeds behind it: the underlying equity and the token itself.
var (
	feedRegistryMu sync.RWMutex
	feedIDRegistry = map[string]string{}
)

// RegisterFeedID registers a cached Hermes equity feed id for tests.
func RegisterFeedID(symbol, id string) {
	registerFeedID(EquityQuerySymbol(symbol), id)
}

// RegisterCryptoFeedID registers a cached Hermes crypto (xStock) feed id for tests.
func RegisterCryptoFeedID(symbol, id string) {
	registerFeedID(CryptoQuerySymbol(symbol), id)
}

// ClearFeedRegistry clears cached feed ids between tests.
func ClearFeedRegistry() {
	feedRegistryMu.Lock()
	feedIDRegistry = map[string]string{}
	feedRegistryMu.Unlock()
}

func lookupFeedID(query string) (string, bool) {
	key := normalizeSymbol(query)
	feedRegistryMu.RLock()
	id, ok := feedIDRegistry[key]
	feedRegistryMu.RUnlock()
	return id, ok
}

func registerFeedID(query, id string) {
	key := normalizeSymbol(query)
	feedRegistryMu.Lock()
	feedIDRegistry[key] = id
	feedRegistryMu.Unlock()
}

func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

// EquityQuerySymbol maps an xStock symbol to the underlying equity's Hermes query
// string. Verified via Pyth MCP get_symbols: AAPLx -> Equity.US.AAPL/USD.
func EquityQuerySymbol(symbol string) string {
	return fmt.Sprintf("Equity.US.%s/USD", underlyingTicker(symbol))
}

// CryptoQuerySymbol maps an xStock symbol to the tokenized asset's own Hermes
// query string: AAPLx -> Crypto.AAPLX/USD. This is the on-chain side of the
// stock-vs-token comparison; not every xStock has one, and a missing feed is an
// honest "unavailable", never a silent substitution of the equity price.
func CryptoQuerySymbol(symbol string) string {
	return fmt.Sprintf("Crypto.%sX/USD", underlyingTicker(symbol))
}

// underlyingTicker strips the xStock suffix: AAPLx -> AAPL.
func underlyingTicker(symbol string) string {
	return strings.TrimSuffix(normalizeSymbol(symbol), "X")
}
