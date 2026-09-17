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

var (
	feedRegistryMu sync.RWMutex
	feedIDRegistry = map[string]string{}
)

// RegisterFeedID registers a cached Hermes feed id for tests.
func RegisterFeedID(symbol, id string) {
	key := normalizeSymbol(symbol)
	feedRegistryMu.Lock()
	feedIDRegistry[key] = id
	feedRegistryMu.Unlock()
}

// ClearFeedRegistry clears cached feed ids between tests.
func ClearFeedRegistry() {
	feedRegistryMu.Lock()
	feedIDRegistry = map[string]string{}
	feedRegistryMu.Unlock()
}

func lookupFeedID(symbol string) (string, bool) {
	key := normalizeSymbol(symbol)
	feedRegistryMu.RLock()
	id, ok := feedIDRegistry[key]
	feedRegistryMu.RUnlock()
	return id, ok
}

func registerFeedID(symbol, id string) {
	key := normalizeSymbol(symbol)
	feedRegistryMu.Lock()
	feedIDRegistry[key] = id
	feedRegistryMu.Unlock()
}

func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

// EquityQuerySymbol maps an xStock symbol to the Pyth Hermes query string.
// Verified via Pyth MCP get_symbols: AAPLx -> Equity.US.AAPL/USD.
func EquityQuerySymbol(symbol string) string {
	base := normalizeSymbol(symbol)
	base = strings.TrimSuffix(base, "X")
	return fmt.Sprintf("Equity.US.%s/USD", base)
}
