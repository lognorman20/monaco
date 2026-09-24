package pyth

import (
	"context"
	"math"
	"sync"
	"time"
)

// The "Stock vs token" comparison: what Apple costs on NASDAQ against what AAPLx
// costs on Solana, straight from two Pyth feeds. It is read-only and display-only —
// pot valuation keeps its own mark policy and is untouched by anything here.
//
// The rule this file exists to enforce: never invent a number. A feed that does not
// exist, a key that is not entitled to it, or an upstream that timed out all come
// back as an explicit unavailable with a reason. A frozen publish time comes back as
// stale with the time it froze at. The app shows what happened; it never shows the
// equity price wearing the token's label.

// QuoteStatus says how much a quote can be trusted right now.
type QuoteStatus string

const (
	// QuoteStatusLive means the feed published recently.
	QuoteStatusLive QuoteStatus = "live"
	// QuoteStatusStale means the price is real but frozen — an equity feed outside
	// the cash session, or a feed that has stopped publishing. PublishedAt says when.
	QuoteStatusStale QuoteStatus = "stale"
	// QuoteStatusUnavailable means there is no price to show at all.
	QuoteStatusUnavailable QuoteStatus = "unavailable"
)

// QuoteSource names which feed produced a quote.
type QuoteSource string

const (
	QuoteSourcePythEquity QuoteSource = "pyth_equity"
	QuoteSourcePythCrypto QuoteSource = "pyth_crypto"
	// QuoteSourceJupiter is the on-chain fallback for an xStock with no Pyth crypto
	// feed. It is labelled differently in the app so nobody reads it as a Pyth price.
	QuoteSourceJupiter QuoteSource = "jupiter"
)

// Reasons a quote is unavailable. These are stable strings the app maps to copy.
const (
	QuoteReasonNoFeed      = "no_feed"
	QuoteReasonNotEntitled = "not_entitled"
	QuoteReasonUpstream    = "upstream_error"
)

// ReferenceQuoteMaxAge is how old a publish time may be before a quote is called
// stale. Pyth equity and crypto feeds both publish every few seconds while they are
// publishing at all, so a minute of silence already means something stopped.
const ReferenceQuoteMaxAge = 5 * time.Minute

// ReferenceQuote is one side of the comparison.
type ReferenceQuote struct {
	Source QuoteSource
	Status QuoteStatus
	// PriceUsdcMicros is zero when Status is unavailable.
	PriceUsdcMicros int64
	// ConfUsdcMicros is Pyth's confidence interval; zero when not published.
	ConfUsdcMicros int64
	// PublishedAt is the feed's publish time in UTC; zero when unknown.
	PublishedAt time.Time
	FeedID      string
	// Reason is one of the QuoteReason constants when Status is unavailable.
	Reason string
}

// Priced reports whether this quote carries a number worth comparing.
func (q ReferenceQuote) Priced() bool {
	return q.Status != QuoteStatusUnavailable && q.PriceUsdcMicros > 0
}

// ReferenceQuotes is the stock-vs-token pair for one symbol.
type ReferenceQuotes struct {
	Symbol string
	Equity ReferenceQuote
	Token  ReferenceQuote
	// AsOf is when the pair was fetched, in UTC.
	AsOf time.Time
}

// PremiumBps is how far the token trades above (positive) or below (negative) the
// underlying equity, in basis points. Nil unless both sides carry a real price —
// a premium against a missing leg would be a fabricated number.
func (r ReferenceQuotes) PremiumBps() *int {
	if !r.Equity.Priced() || !r.Token.Priced() {
		return nil
	}
	ratio := float64(r.Token.PriceUsdcMicros-r.Equity.PriceUsdcMicros) / float64(r.Equity.PriceUsdcMicros)
	bps := int(math.Round(ratio * 10_000))
	return &bps
}

// ReferenceQuoteClient fetches the stock-vs-token pair for a symbol.
type ReferenceQuoteClient interface {
	ReferenceQuotes(ctx context.Context, symbol string) (ReferenceQuotes, error)
}

// ReferenceQuoteTTL is how long a fetched pair is shared between callers. The
// asset screen polls, and two feeds per poll per viewer adds up fast; a few
// seconds of reuse is invisible on screen and keeps Hermes quiet.
const ReferenceQuoteTTL = 10 * time.Second

type referenceQuoteEntry struct {
	quotes    ReferenceQuotes
	expiresAt time.Time
}

// ReferenceQuotes fetches both feeds concurrently. Neither leg failing is an error:
// the pair always comes back, with the failing side marked unavailable and why.
func (c *HermesClient) ReferenceQuotes(ctx context.Context, symbol string) (ReferenceQuotes, error) {
	if cached, ok := c.cachedReferenceQuotes(symbol); ok {
		return cached, nil
	}

	now := time.Now().UTC()
	var (
		equity ReferenceQuote
		token  ReferenceQuote
		wg     sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		equity = c.quoteFromFeed(ctx, symbol, EquityQuerySymbol(symbol), QuoteSourcePythEquity, now)
	}()
	go func() {
		defer wg.Done()
		token = c.quoteFromFeed(ctx, symbol, CryptoQuerySymbol(symbol), QuoteSourcePythCrypto, now)
	}()
	wg.Wait()

	logReferenceQuotes(symbol, equity.Status, token.Status)
	quotes := ReferenceQuotes{Symbol: symbol, Equity: equity, Token: token, AsOf: now}
	c.storeReferenceQuotes(symbol, quotes, now)
	return quotes, nil
}

func (c *HermesClient) cachedReferenceQuotes(symbol string) (ReferenceQuotes, bool) {
	c.quoteCacheMu.RLock()
	entry, found := c.quoteCache[normalizeSymbol(symbol)]
	c.quoteCacheMu.RUnlock()
	if !found || time.Now().After(entry.expiresAt) {
		return ReferenceQuotes{}, false
	}
	return entry.quotes, true
}

func (c *HermesClient) storeReferenceQuotes(symbol string, quotes ReferenceQuotes, now time.Time) {
	c.quoteCacheMu.Lock()
	if c.quoteCache == nil {
		c.quoteCache = make(map[string]referenceQuoteEntry)
	}
	c.quoteCache[normalizeSymbol(symbol)] = referenceQuoteEntry{
		quotes:    quotes,
		expiresAt: now.Add(ReferenceQuoteTTL),
	}
	c.quoteCacheMu.Unlock()
}

func (c *HermesClient) quoteFromFeed(ctx context.Context, symbol, query string, source QuoteSource, now time.Time) ReferenceQuote {
	// Only the id is needed here: staleness comes from the publish time on the price
	// itself, not from market_hours.is_open, so a feed lookup for a known id would
	// be a round trip whose whole answer gets discarded.
	feedID, err := c.resolveFeedID(ctx, symbol, query)
	if err != nil {
		return unavailableQuote(source, quoteFailureReason(err))
	}

	latest, err := c.fetchLatestPrice(ctx, feedID)
	if err != nil {
		return unavailableQuote(source, quoteFailureReason(err))
	}

	price, err := priceToUSDCMicros(latest.Price.Price, latest.Price.Expo)
	if err != nil || price <= 0 {
		return unavailableQuote(source, QuoteReasonUpstream)
	}

	quote := ReferenceQuote{
		Source:          source,
		Status:          QuoteStatusLive,
		PriceUsdcMicros: price,
		ConfUsdcMicros:  confToUSDCMicros(latest.Price.Conf, latest.Price.Expo),
		FeedID:          feedID,
	}
	if latest.Price.PublishTime > 0 {
		quote.PublishedAt = time.Unix(latest.Price.PublishTime, 0).UTC()
	}
	if isStaleQuote(latest, quote.PublishedAt, now) {
		quote.Status = QuoteStatusStale
	}
	return quote
}

// isStaleQuote treats a frozen publish time the same as an old one. Hermes repeats
// the last price with publish_time == prev_publish_time when a feed stops moving,
// which is exactly what an equity feed does once the bell rings.
func isStaleQuote(latest parsedPriceUpdate, publishedAt, now time.Time) bool {
	if isFrozenEquityMark(latest.Price.PublishTime, latest.Metadata.PrevPublishTime) {
		return true
	}
	if publishedAt.IsZero() {
		return true
	}
	return now.Sub(publishedAt) > ReferenceQuoteMaxAge
}

func quoteFailureReason(err error) string {
	switch {
	case IsEntitlementError(err):
		return QuoteReasonNotEntitled
	case isFeedNotFound(err):
		return QuoteReasonNoFeed
	default:
		return QuoteReasonUpstream
	}
}

func unavailableQuote(source QuoteSource, reason string) ReferenceQuote {
	return ReferenceQuote{Source: source, Status: QuoteStatusUnavailable, Reason: reason}
}

// JupiterFallbackQuote wraps an on-chain price as the token leg for an xStock with
// no Pyth crypto feed. The source is recorded as Jupiter so the app can label it
// "on-chain price" instead of implying Pyth published it.
//
// It carries no publish time. Jupiter's Price API does not say when the price it
// returns was struck, and stamping the server's own clock on it claimed a precision
// nobody has — the app would have rendered "updated 2 seconds ago" about an instant
// that means nothing. A nil publishedAt is the honest answer, and the app has copy
// for a leg that does not know its own age.
//
// The premium against this leg stands: Jupiter prices the xStock mint, so it is
// still the token against the underlying, just from a different venue than Pyth.
// The card says which venue.
func JupiterFallbackQuote(priceUsdcMicros int64) ReferenceQuote {
	if priceUsdcMicros <= 0 {
		return unavailableQuote(QuoteSourceJupiter, QuoteReasonUpstream)
	}
	return ReferenceQuote{
		Source:          QuoteSourceJupiter,
		Status:          QuoteStatusLive,
		PriceUsdcMicros: priceUsdcMicros,
	}
}
