package pyth

import (
	"context"
	"math"
	"math/big"
	"strings"
	"time"
)

// The "stock vs token" comparison on the asset screen. It is read-only and
// display-only: pot valuation keeps its Chainlink marks and nothing here touches it.
//
// Three prices sit on the card, and only two of them are the same unit:
//
//   - the token leg, what the B20 token costs in its pools on Base right now: the
//     mid of a Kyber buy probe and a Kyber sell probe. Pyth publishes no feed for a
//     B20 token (verified against Hermes: a query for AAPLC returns nothing), so a
//     quote-implied price is the only market price of the token there is;
//   - the mark, the token's Chainlink total-return value (TRV). Coinbase's B20 docs
//     define it as the underlying's price times the token's multiplier, which is how
//     splits and reinvested cash dividends reach the holder;
//   - the equity reference, Pyth's price for the underlying on its exchange, per
//     share.
//
// The premium is the token leg against the mark. Both are per token and both carry
// the multiplier, so the difference is what the pools charge over the mark. Against
// the raw equity price the multiplier itself would read as a premium, growing with
// every dividend, so no premium is ever computed against the equity leg; the app
// shows it as its own labelled line.
//
// The rule this file enforces: never invent a number. A feed that does not exist, a
// key that is not entitled to it, a missing key, an upstream that timed out and a
// pool with no route all come back as an explicit unavailable with a reason.

// QuoteStatus says how much a quote can be trusted right now.
type QuoteStatus string

const (
	// QuoteStatusLive means the price is current.
	QuoteStatusLive QuoteStatus = "live"
	// QuoteStatusStale means the price is real but frozen — an equity feed outside
	// the cash session, or a feed that has stopped publishing. PublishedAt says when.
	QuoteStatusStale QuoteStatus = "stale"
	// QuoteStatusUnavailable means there is no price to show at all.
	QuoteStatusUnavailable QuoteStatus = "unavailable"
)

// QuoteSource names which venue or feed produced a quote.
type QuoteSource string

const (
	// QuoteSourcePythEquity is Pyth's feed for the underlying equity, per share.
	QuoteSourcePythEquity QuoteSource = "pyth_equity"
	// QuoteSourceDexKyber is the mid implied by a Kyber buy and sell probe on Base,
	// per token.
	QuoteSourceDexKyber QuoteSource = "dex_kyber"
	// QuoteSourceChainlinkTRV is the token's Chainlink total-return mark, per token.
	QuoteSourceChainlinkTRV QuoteSource = "chainlink_trv"
)

// Reasons a quote is unavailable. These are stable strings the app maps to copy.
const (
	QuoteReasonNoFeed        = "no_feed"
	QuoteReasonNotEntitled   = "not_entitled"
	QuoteReasonUpstream      = "upstream_error"
	QuoteReasonNotConfigured = "not_configured"
	// QuoteReasonNoRoute is Kyber finding no route for one of the two probes.
	QuoteReasonNoRoute = "no_route"
)

// ReferenceQuoteMaxAge is how old a Pyth publish time may be before a quote is
// called stale. Pyth feeds publish every few seconds while they are publishing at
// all, so minutes of silence already mean something stopped.
const ReferenceQuoteMaxAge = 5 * time.Minute

// ReferenceQuote is one line of the comparison.
type ReferenceQuote struct {
	Source QuoteSource
	Status QuoteStatus
	// PriceUsdcMicros is zero when Status is unavailable.
	PriceUsdcMicros int64
	// ConfUsdcMicros is Pyth's confidence interval; zero when not published, and
	// always zero for a venue that does not publish one (Kyber, Chainlink).
	ConfUsdcMicros int64
	// PublishedAt is the feed's publish time in UTC; zero when the source does not
	// say when its price was struck (a Kyber quote does not).
	PublishedAt time.Time
	FeedID      string
	// Reason is one of the QuoteReason constants when Status is unavailable.
	Reason string
}

// Priced reports whether this quote carries a number worth comparing.
func (q ReferenceQuote) Priced() bool {
	return q.Status != QuoteStatusUnavailable && q.PriceUsdcMicros > 0
}

func unavailableQuote(source QuoteSource, reason string) ReferenceQuote {
	return ReferenceQuote{Source: source, Status: QuoteStatusUnavailable, Reason: reason}
}

// PremiumBps is how far the token leg trades above (positive) or below (negative)
// the mark, in basis points. Nil unless both carry a real price: a premium against
// a missing leg would be a fabricated number.
func PremiumBps(token, mark ReferenceQuote) *int {
	if !token.Priced() || !mark.Priced() {
		return nil
	}
	ratio := float64(token.PriceUsdcMicros-mark.PriceUsdcMicros) / float64(mark.PriceUsdcMicros)
	bps := int(math.Round(ratio * 10_000))
	return &bps
}

// MarkQuote wraps the token's Chainlink total-return mark as the line the premium
// is measured against.
func MarkQuote(markUsdcMicros *int64) ReferenceQuote {
	if markUsdcMicros == nil || *markUsdcMicros <= 0 {
		return unavailableQuote(QuoteSourceChainlinkTRV, QuoteReasonUpstream)
	}
	return ReferenceQuote{Source: QuoteSourceChainlinkTRV, Status: QuoteStatusLive, PriceUsdcMicros: *markUsdcMicros}
}

// KyberProbe is the pair of quotes the asset screen already takes for liquidity:
// a buy of BuyInUsdcMicros returning BuyOutAtomics of the token, and a sell of
// SellInAtomics returning SellOutUsdcMicros. An empty out amount is a probe that
// found no route. TokenAtomicScale is atomics per whole token (1e8 for B20).
type KyberProbe struct {
	BuyInUsdcMicros   int64
	BuyOutAtomics     string
	SellInAtomics     int64
	SellOutUsdcMicros string
	TokenAtomicScale  int64
}

// KyberQuote is the token leg: the probes' ask, bid, mid and spread.
type KyberQuote struct {
	Quote         ReferenceQuote
	AskUsdcMicros int64
	BidUsdcMicros int64
	// SpreadBps is (ask - bid) / mid in basis points. A 1 USDC probe includes pool
	// fees and price impact, so on a thin pool this reads wide — which is the real
	// round-trip cost at that size.
	SpreadBps *int
}

// KyberTokenQuote turns the two probes into the token leg. It needs both: an ask
// with no bid, or the reverse, is half a market and its "mid" would be a guess.
func KyberTokenQuote(probe KyberProbe) KyberQuote {
	ask, askOK := microsPerToken(probe.BuyInUsdcMicros, probe.BuyOutAtomics, probe.TokenAtomicScale, true)
	bid, bidOK := microsPerToken(probe.SellInAtomics, probe.SellOutUsdcMicros, probe.TokenAtomicScale, false)
	if !askOK || !bidOK {
		return KyberQuote{Quote: unavailableQuote(QuoteSourceDexKyber, QuoteReasonNoRoute)}
	}
	mid := (ask + bid) / 2
	if mid <= 0 {
		return KyberQuote{Quote: unavailableQuote(QuoteSourceDexKyber, QuoteReasonUpstream)}
	}
	spread := int(math.Round(float64(ask-bid) / float64(mid) * 10_000))
	return KyberQuote{
		Quote: ReferenceQuote{
			Source:          QuoteSourceDexKyber,
			Status:          QuoteStatusLive,
			PriceUsdcMicros: mid,
		},
		AskUsdcMicros: ask,
		BidUsdcMicros: bid,
		SpreadBps:     &spread,
	}
}

// microsPerToken converts one probe into USDC micros per whole token. For a buy the
// input is USDC and the output token atomics (price = in * scale / out); for a sell
// the input is token atomics and the output USDC (price = out * scale / in). Kyber
// amounts are untrusted decimal strings, so the arithmetic is done in big.Int and
// anything that does not fit an int64 price is rejected rather than wrapped.
func microsPerToken(in int64, out string, scale int64, buy bool) (int64, bool) {
	if in <= 0 || scale <= 0 {
		return 0, false
	}
	outAmount, ok := new(big.Int).SetString(strings.TrimSpace(out), 10)
	if !ok || outAmount.Sign() <= 0 {
		return 0, false
	}
	price := new(big.Int)
	if buy {
		price.Mul(big.NewInt(in), big.NewInt(scale))
		price.Quo(price, outAmount)
	} else {
		price.Mul(outAmount, big.NewInt(scale))
		price.Quo(price, big.NewInt(in))
	}
	if !price.IsInt64() || price.Sign() <= 0 {
		return 0, false
	}
	return price.Int64(), true
}

// EquityQuoteClient fetches Pyth's latest price for the equity a token tracks.
type EquityQuoteClient interface {
	EquityQuote(ctx context.Context, symbol string) ReferenceQuote
}

// EquityQuoteTTL is how long a fetched equity quote is shared between callers. The
// asset screen polls, and a few seconds of reuse is invisible on screen.
const EquityQuoteTTL = 10 * time.Second

type equityQuoteEntry struct {
	quote     ReferenceQuote
	expiresAt time.Time
}

// EquityQuote returns the underlying's latest Hermes price as a reference line. It
// never errors: every failure is an unavailable quote with a reason.
func (c *HermesClient) EquityQuote(ctx context.Context, symbol string) ReferenceQuote {
	if !c.HasAPIKey() {
		return unavailableQuote(QuoteSourcePythEquity, QuoteReasonNotConfigured)
	}
	now := c.clock()
	key := normalizeSymbol(UnderlyingTicker(symbol))
	c.quoteMu.Lock()
	entry, found := c.quoteCache[key]
	c.quoteMu.Unlock()
	if found && now.Before(entry.expiresAt) {
		return entry.quote
	}

	quote := c.fetchEquityQuote(ctx, symbol, now)
	// An upstream failure is not cached: it is not an answer about the feed, and the
	// next poll should try again.
	if quote.Status != QuoteStatusUnavailable || quote.Reason != QuoteReasonUpstream {
		c.quoteMu.Lock()
		if c.quoteCache == nil {
			c.quoteCache = make(map[string]equityQuoteEntry)
		}
		c.quoteCache[key] = equityQuoteEntry{quote: quote, expiresAt: now.Add(EquityQuoteTTL)}
		c.quoteMu.Unlock()
	}
	return quote
}

func (c *HermesClient) fetchEquityQuote(ctx context.Context, symbol string, now time.Time) ReferenceQuote {
	// Only the id is needed: staleness comes from the publish time on the price
	// itself, not from market_hours.is_open.
	feedID, err := c.resolveFeedID(ctx, symbol, EquityQuerySymbol(symbol))
	if err != nil {
		return unavailableQuote(QuoteSourcePythEquity, quoteFailureReason(err))
	}
	latest, err := c.fetchLatestPrice(ctx, feedID)
	if err != nil {
		return unavailableQuote(QuoteSourcePythEquity, quoteFailureReason(err))
	}
	price, err := priceToUSDCMicros(latest.Price.Price, latest.Price.Expo)
	if err != nil || price <= 0 {
		return unavailableQuote(QuoteSourcePythEquity, QuoteReasonUpstream)
	}

	quote := ReferenceQuote{
		Source:          QuoteSourcePythEquity,
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
