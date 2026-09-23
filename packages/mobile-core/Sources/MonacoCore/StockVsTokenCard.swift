import Foundation

/// The token in its pools against its own mark, with the equity it tracks beside it:
/// what a B20 token is changing hands for on Base, what the mark says it is worth,
/// and what the share costs on its home exchange.
///
/// All of the meaning lives here rather than in the view, because almost none of it is
/// layout. Which leg may show a number, which two legs a premium may be measured
/// between, what a frozen equity print is called after the bell, and what the card
/// says when a feed is missing are all decisions about honesty, and each one is
/// pinned by a test.
///
/// Four rules the copy must never break:
///
/// 1. **The premium is the token against its own mark**, never against the equity.
///    Both of those are per token and both carry the B20 multiplier; the equity is
///    per share. Measuring the token against the share would turn every reinvested
///    dividend into a premium that grows forever and never closes.
/// 2. **A Kyber-sourced leg is never called a Pyth price.** It is the quote-implied
///    mid of two probes, it carries no confidence interval because Kyber publishes
///    none, and it has no publish time — only a `probedAt`.
/// 3. **Pyth has no feed for a B20 token.** No leg on this card may ever be labelled
///    as a Pyth price for the token, and no leg may be labelled as a Solana xStock
///    feed, which is a different instrument on a different chain.
/// 4. **A stale equity leg outside the cash session is not an error.** It is the
///    closing print, and it reads as "Closed 4:00 PM" — the fact the whole product
///    is about.
public struct StockVsTokenCard: Equatable, Sendable {
    /// One line of the comparison.
    public struct Leg: Equatable, Sendable, Identifiable {
        /// A stable key, so a UI test can ask for one line by name.
        public let id: String
        /// "AAPL" / "AAPLc".
        public let ticker: String
        /// "Apple on its home exchange" / "Tokenized, trading on Base".
        public let venue: String
        /// Where the number came from, in the member's language: "Pyth equity feed",
        /// "Kyber quote", "Chainlink total-return feed".
        public let source: String
        /// The price in USDC micros, or nil when this leg has none to show.
        public let priceUsdcMicros: Int64?
        /// "±$0.03" — Pyth's own confidence interval around the price. Nil when the
        /// source publishes none, which is every leg but the equity.
        public let confidence: String?
        /// "Live", "Closed 4:00 PM", "Last price 11:41 AM", "Quoted 11:40 AM", or
        /// the reason there is no price at all.
        public let freshness: String
        /// True only while this leg is actually printing. The pulsing dot reads it.
        public let isLive: Bool
        /// Set when there is no price: the card shows this instead of a figure.
        public let unavailableReason: String?
        /// True for the equity line, which is a labelled reference and is not what
        /// the premium is measured against.
        public let isReferenceOnly: Bool

        public var isPriced: Bool { priceUsdcMicros != nil }

        /// One sentence for VoiceOver, so the leg is not read as four fragments.
        public var spoken: String {
            guard let unavailableReason else {
                var sentence = "\(ticker), \(venue)"
                if let priceUsdcMicros {
                    sentence += ", \(UsdAmountFormatter.format(micros: priceUsdcMicros))"
                }
                sentence += ", \(freshness)"
                if let confidence { sentence += ", give or take \(confidence.dropFirst())" }
                return sentence
            }
            return "\(ticker), \(venue). \(unavailableReason)"
        }
    }

    /// How far the token trades from its own mark.
    public struct Premium: Equatable, Sendable {
        public enum Direction: Equatable, Sendable {
            case above
            case below
            case inline
        }

        public let basisPoints: Int
        /// "+0.28%" / "−1.47%" / "0.00%".
        public let label: String
        /// "AAPLc is trading 0.28% above its mark" — the pill's own sentence.
        public let caption: String
        public let direction: Direction
    }

    /// The Kyber quote-implied mid: what the token is changing hands for.
    public let token: Leg
    /// The Chainlink total-return mark, the same per-token mark the hero price and
    /// pot NAV use. The premium is measured against this one.
    public let mark: Leg
    /// The underlying share on its home exchange, from Pyth. A labelled reference
    /// line: it is a different unit and nothing is measured against it.
    public let equity: Leg
    /// Nil unless the token and the mark both carry a real, live price.
    public let premium: Premium?
    /// "Ask 0.12% over bid" — the cost of the round trip on Base, when both probes
    /// routed. Nil when the backend did not send one.
    public let spread: String?
    /// The line under the card: what a premium actually is, or why one is missing.
    public let footnote: String

    /// Every line, in the order they are drawn.
    public var legs: [Leg] { [token, mark, equity] }

    /// True when no line has a price. The card hides itself rather than drawing
    /// three dashes and a disclaimer.
    public var isEmpty: Bool { !token.isPriced && !mark.isPriced && !equity.isPriced }

    /// Builds the card, or nil when the backend sent no comparison at all.
    ///
    /// - Parameters:
    ///   - symbol: the token's own symbol, "AAPLc".
    ///   - name: the underlying's company name from the catalog, "Apple".
    ///   - market: the exchange session, which is what turns a frozen equity print
    ///     from a fault into a closing price.
    public static func make(
        symbol: String,
        name: String,
        quotes: StockVsTokenDTO?,
        market: MarketStatusDTO?,
        locale: Locale = .autoupdatingCurrent,
        timeZone: TimeZone = .autoupdatingCurrent
    ) -> StockVsTokenCard? {
        guard let quotes else { return nil }

        let tokenTicker = AssetSymbolFormatter.format(symbol)
        // The backend names the underlying; falling back to stripping the token's
        // suffix keeps an older response readable rather than unlabelled.
        let underlying = quotes.equitySymbol.map(AssetSymbolFormatter.display)
            ?? AssetSymbolFormatter.display(symbol)
        let company = name.trimmingCharacters(in: .whitespacesAndNewlines)

        let token = leg(
            quotes.token,
            id: "token",
            ticker: tokenTicker,
            venue: "Tokenized, trading on Base",
            isEquity: false,
            isReferenceOnly: false,
            market: market,
            locale: locale,
            timeZone: timeZone
        )
        let mark = leg(
            quotes.mark,
            id: "mark",
            ticker: tokenTicker,
            venue: "Its mark, the price holdings carry",
            isEquity: false,
            isReferenceOnly: false,
            market: market,
            locale: locale,
            timeZone: timeZone
        )
        let equity = leg(
            quotes.equity,
            id: "equity",
            ticker: underlying,
            venue: company.isEmpty ? "On its home exchange" : "\(company) on its home exchange",
            isEquity: true,
            isReferenceOnly: true,
            market: market,
            locale: locale,
            timeZone: timeZone
        )

        let card = StockVsTokenCard(
            token: token,
            mark: mark,
            equity: equity,
            premium: premium(quotes, tokenTicker: tokenTicker),
            spread: spreadCopy(quotes.spreadBps),
            footnote: footnote(token: token, mark: mark, equity: equity, underlying: underlying, tokenTicker: tokenTicker)
        )
        return card.isEmpty ? nil : card
    }

    // MARK: - Legs

    private static func leg(
        _ quote: ReferenceQuoteDTO,
        id: String,
        ticker: String,
        venue: String,
        isEquity: Bool,
        isReferenceOnly: Bool,
        market: MarketStatusDTO?,
        locale: Locale,
        timeZone: TimeZone
    ) -> Leg {
        let source = sourceLabel(quote.source)
        guard quote.isPriced else {
            let reason = unavailableCopy(quote, isEquity: isEquity)
            return Leg(
                id: id,
                ticker: ticker,
                venue: venue,
                source: source,
                priceUsdcMicros: nil,
                confidence: nil,
                freshness: reason,
                isLive: false,
                unavailableReason: reason,
                isReferenceOnly: isReferenceOnly
            )
        }

        return Leg(
            id: id,
            ticker: ticker,
            venue: venue,
            source: source,
            priceUsdcMicros: quote.priceUsdcMicros,
            // Only Pyth publishes an interval, so a leg from any other source never
            // claims one even if a future payload carries a stray number.
            confidence: quote.source == .pythEquity ? confidenceCopy(quote.confUsdcMicros) : nil,
            freshness: freshnessCopy(quote, isEquity: isEquity, market: market, locale: locale, timeZone: timeZone),
            isLive: quote.status == .live,
            unavailableReason: nil,
            isReferenceOnly: isReferenceOnly
        )
    }

    /// Never "Pyth" for a price Pyth did not publish, and never a chain this token
    /// does not live on. Pyth has no feed for a B20 token, so no case here can
    /// produce one.
    static func sourceLabel(_ source: ReferenceQuoteSource) -> String {
        switch source {
        case .pythEquity: return "Pyth equity feed"
        case .dexKyber: return "Kyber quote"
        case .chainlinkTRV: return "Chainlink total-return feed"
        case .unknown: return "Reference price"
        }
    }

    /// "±$0.03". Pyth's confidence is the single most bounty-relevant number on this
    /// card, so a zero interval is shown as no interval rather than as "±$0.00",
    /// which would read as a claim of perfect certainty.
    static func confidenceCopy(_ micros: Int64?) -> String? {
        guard let micros, micros > 0 else { return nil }
        return "±\(UsdAmountFormatter.format(micros: micros))"
    }

    /// "Ask 0.12% over bid": the gap between the two Kyber probes.
    ///
    /// Named as the two sides it is measured between, not as a fee: it is a 1 USDC
    /// buy probe against a one-token sell probe, so it carries pool fees and price
    /// impact at that size and is not what any particular trade will cost.
    static func spreadCopy(_ bps: Int?) -> String? {
        guard let bps, bps > 0 else { return nil }
        let percent = Double(bps) / 100
        if percent < 0.01 { return "Ask under 0.01% over bid" }
        return "Ask \(String(format: "%.2f", percent))% over bid"
    }

    /// What the time on this leg means.
    ///
    /// A frozen equity print while the exchange is shut is the closing price, not a
    /// fault — that is the product's whole argument, and calling it stale here would
    /// argue against ourselves. A frozen print *during* the session is a real
    /// problem and says when the feed was last seen.
    static func freshnessCopy(
        _ quote: ReferenceQuoteDTO,
        isEquity: Bool,
        market: MarketStatusDTO?,
        locale: Locale,
        timeZone: TimeZone
    ) -> String {
        // A Kyber quote has no publish time at all; what it has is when we probed.
        if quote.source == .dexKyber {
            guard let probedAt = quote.probedAt else { return quote.status == .live ? "Live" : "Last quote" }
            return "Quoted \(clockTime(probedAt, locale: locale, timeZone: timeZone))"
        }
        if quote.status == .live { return "Live" }
        guard let publishedAt = quote.publishedAt else {
            return marketIsShut(market) ? "Closed" : "Last price unknown"
        }
        let time = clockTime(publishedAt, locale: locale, timeZone: timeZone)
        // The mark holds the last close over a weekend exactly as the equity feed
        // does, so both read as a close rather than as a fault.
        if marketIsShut(market) {
            return "Closed \(time)"
        }
        _ = isEquity
        return "Last price \(time)"
    }

    static func marketIsShut(_ market: MarketStatusDTO?) -> Bool {
        guard let market else { return false }
        return !market.isOpen && market.session != .unknown
    }

    /// Why there is no number, in words a member can act on.
    static func unavailableCopy(_ quote: ReferenceQuoteDTO, isEquity: Bool) -> String {
        switch quote.unavailableReason {
        case .noFeed:
            return isEquity ? "No Pyth feed for this stock yet" : "No feed for this token yet"
        case .notEntitled:
            return "This feed is not part of our plan"
        case .notConfigured:
            return "This feed is not set up yet"
        case .noRoute:
            return "No route on Base for this token"
        case .upstreamError:
            return "Price feed did not answer"
        case nil:
            return "No price right now"
        }
    }

    // MARK: - Premium

    /// The token against its own mark, and against nothing else.
    ///
    /// The backend already refuses to compute one against a missing or stale leg;
    /// this is the second lock on the same door, because the pill is the one element
    /// on the card that would silently read as measured if it were not. The equity
    /// leg is deliberately not consulted: a premium that disappeared whenever Pyth
    /// was down would imply the two are related, and they are not — the premium is
    /// between two per-token prices.
    static func premium(_ quotes: StockVsTokenDTO, tokenTicker: String) -> Premium? {
        guard let basisPoints = quotes.premiumBps,
              quotes.token.isPriced, quotes.mark.isPriced,
              quotes.token.status == .live, quotes.mark.status == .live
        else { return nil }

        let percent = Double(basisPoints) / 100
        let magnitude = String(format: "%.2f", abs(percent))
        let direction: Premium.Direction = magnitude == "0.00" ? .inline : (basisPoints > 0 ? .above : .below)

        let label: String
        switch direction {
        case .inline: label = "0.00%"
        case .above: label = "+\(magnitude)%"
        case .below: label = "\(typographicMinus)\(magnitude)%"
        }

        let caption: String
        switch direction {
        case .inline:
            caption = "\(tokenTicker) is trading in line with its mark"
        case .above:
            caption = "\(tokenTicker) is trading \(magnitude)% above its mark"
        case .below:
            caption = "\(tokenTicker) is trading \(magnitude)% below its mark"
        }

        return Premium(basisPoints: basisPoints, label: label, caption: caption, direction: direction)
    }

    // MARK: - Footnote

    static func footnote(
        token: Leg,
        mark: Leg,
        equity: Leg,
        underlying: String,
        tokenTicker: String
    ) -> String {
        if !token.isPriced {
            return "Without a \(tokenTicker) quote on Base there is nothing to compare against its mark."
        }
        if !mark.isPriced {
            return "Without \(tokenTicker)'s mark there is nothing to compare the quote against."
        }
        if !mark.isLive {
            return "\(tokenTicker)'s mark is holding its last print, so the gap below it is the market's "
                + "move since then, not a premium."
        }
        if equity.isPriced && !equity.isLive && token.isLive {
            return "\(underlying) stopped printing at the bell. \(tokenTicker) keeps trading on Base, which "
                + "is why the two can drift apart. The percentage is against \(tokenTicker)'s own mark, not "
                + "against \(underlying)."
        }
        return "The percentage is \(tokenTicker) against its own mark — both per token. \(underlying) is "
            + "per share and is shown for reference: one \(tokenTicker) is worth more than one share by the "
            + "dividends reinvested into it."
    }

    // MARK: - Time

    /// The publish instant in the reader's own clock. `j` is the locale's hour field,
    /// so a 24-hour locale reads "16:00" and never "4:00 PM".
    static func clockTime(_ date: Date, locale: Locale, timeZone: TimeZone) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.locale = locale
        calendar.timeZone = timeZone
        return SharedFormatters.string(
            from: date,
            pattern: .template("jmm"),
            locale: locale,
            calendar: calendar,
            timeZone: timeZone
        )
    }
}
