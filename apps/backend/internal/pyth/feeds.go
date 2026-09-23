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

// Feed ids are cached per Hermes query string rather than per symbol, so the key
// says exactly which feed it is ("Equity.US.AAPL/USD"), not which asset asked.
var (
	feedRegistryMu sync.RWMutex
	feedIDRegistry = map[string]string{}
)

// RegisterFeedID registers a cached Hermes equity feed id for tests.
func RegisterFeedID(symbol, id string) {
	registerFeedID(EquityQuerySymbol(symbol), id)
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

// EquityQuerySymbol maps a B20 token symbol to the Hermes query string of the
// equity it tracks: AAPLc -> Equity.US.AAPL/USD.
func EquityQuerySymbol(symbol string) string {
	return fmt.Sprintf("Equity.US.%s/USD", underlyingTicker(symbol))
}

// underlyingTicker strips the token suffix and upper-cases what is left:
// AAPLc -> AAPL, SPCXc -> SPCX.
//
// The suffix is exactly one trailing lowercase "c" (a Coinbase B20 token on Base)
// or, for a symbol an old client may still send, one trailing lowercase "x" (the
// Solana xStock naming). The case is the only thing that tells the suffix apart
// from the ticker: SPCX ends in an upper-case X that is part of the ticker, so
// upper-casing first and then trimming C and X turned SPCXc into SPC and priced
// SpaceX off a different company's feed. Case-folding happens after the strip,
// never before it.
func underlyingTicker(symbol string) string {
	trimmed := strings.TrimSpace(symbol)
	if strings.HasSuffix(trimmed, "c") {
		trimmed = strings.TrimSuffix(trimmed, "c")
	} else if strings.HasSuffix(trimmed, "x") {
		trimmed = strings.TrimSuffix(trimmed, "x")
	}
	return strings.ToUpper(trimmed)
}

// UnderlyingTicker is underlyingTicker for callers outside the package: AAPLc -> AAPL.
func UnderlyingTicker(symbol string) string { return underlyingTicker(symbol) }

// Which instrument a price or a derived figure is about. A B20 token and the
// equity it tracks are different instruments: the token's Chainlink total-return
// mark folds splits and dividends into a multiplier, and its pools trade when the
// exchange is shut. Anything derived from Pyth candles is about the equity, and
// has to say so.
const (
	// PriceBasisUnderlying is the equity on its home exchange (Equity.US.AAPL/USD),
	// per share.
	PriceBasisUnderlying = "underlying"
	// PriceBasisToken is the B20 token itself, per token (the Chainlink
	// total-return mark, or a Kyber quote).
	PriceBasisToken = "token"
)
