import Foundation

/// The stock against the token that tracks it: what the underlying costs on its home
/// exchange, what the xStock costs on Solana, and how far apart they are.
///
/// All of the meaning lives here rather than in the view, because almost none of it is
/// layout. Which of the two legs may show a number, what a frozen equity print is
/// called after the bell, whether a premium may be drawn at all, and what the card
/// says when a feed is missing are all decisions about honesty, and each one is
/// pinned by a test.
///
/// Three rules the copy must never break:
///
/// 1. A Jupiter-sourced leg is *never* called a Pyth price. It is the on-chain price,
///    and it carries no confidence interval because Jupiter publishes none.
/// 2. A stale equity leg outside the cash session is not an error. It is the closing
///    print, and it reads as "Closed 4:00 PM" — the fact the whole product is about.
/// 3. A premium is only drawn when both legs carry a real price. A "0.00%" pill
///    against a missing leg would be a number nobody measured.
public struct StockVsTokenCard: Equatable, Sendable {
    /// One side of the comparison.
    public struct Leg: Equatable, Sendable {
        /// "AAPL" / "AAPLx".
        public let ticker: String
        /// "Apple on its home exchange" / "Tokenized, trading on Solana".
        public let venue: String
        /// Where the number came from, in the member's language: "Pyth equity feed",
        /// "Pyth crypto feed", "On-chain price".
        public let source: String
        /// The price in USDC micros, or nil when this leg has none to show.
        public let priceUsdcMicros: Int64?
        /// "±$0.03" — Pyth's own confidence interval around the price. Nil when the
        /// source publishes none, which is the Jupiter case.
        public let confidence: String?
        /// "Live", "Closed 4:00 PM", "Last price 11:41 AM", or the reason there is
        /// no price at all.
        public let freshness: String
        /// True only while this leg is actually printing. The pulsing dot reads it.
        public let isLive: Bool
        /// Set when there is no price: the card shows this instead of a figure.
        public let unavailableReason: String?

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

    /// How far the token trades from the stock.
    public struct Premium: Equatable, Sendable {
        public enum Direction: Equatable, Sendable {
            case above
            case below
            case inline
        }

        public let basisPoints: Int
        /// "+0.28%" / "−1.47%" / "0.00%".
        public let label: String
        /// "AAPLx is trading 0.28% above Apple" — the pill's own sentence.
        public let caption: String
        public let direction: Direction
    }

    public let equity: Leg
    public let token: Leg
    /// Nil unless both legs carry a real price.
    public let premium: Premium?
    /// The line under the card: what a premium actually is, or why one is missing.
    public let footnote: String

    /// True when neither leg has a price. The card hides itself rather than drawing
    /// two dashes and a disclaimer.
    public var isEmpty: Bool { !equity.isPriced && !token.isPriced }

    /// Builds the card, or nil when the backend sent no comparison at all.
    ///
    /// - Parameters:
    ///   - symbol: the xStock's own symbol, "AAPLx".
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

        let underlying = AssetSymbolFormatter.display(symbol)
        let tokenTicker = AssetSymbolFormatter.format(symbol)
        let company = name.trimmingCharacters(in: .whitespacesAndNewlines)

        let equity = leg(
            quotes.equity,
            ticker: underlying,
            venue: company.isEmpty ? "On its home exchange" : "\(company) on its home exchange",
            isEquity: true,
            market: market,
            locale: locale,
            timeZone: timeZone
        )
        let token = leg(
            quotes.token,
            ticker: tokenTicker,
            venue: "Tokenized, trading on Solana",
            isEquity: false,
            market: market,
            locale: locale,
            timeZone: timeZone
        )

        let card = StockVsTokenCard(
            equity: equity,
            token: token,
            premium: premium(quotes, tokenTicker: tokenTicker, underlying: underlying),
            footnote: footnote(quotes, equity: equity, token: token, underlying: underlying, tokenTicker: tokenTicker)
        )
        return card.isEmpty ? nil : card
    }

    // MARK: - Legs

    private static func leg(
        _ quote: ReferenceQuoteDTO,
        ticker: String,
        venue: String,
        isEquity: Bool,
        market: MarketStatusDTO?,
        locale: Locale,
        timeZone: TimeZone
    ) -> Leg {
        let source = sourceLabel(quote.source)
        guard quote.isPriced else {
            let reason = unavailableCopy(quote, isEquity: isEquity)
            return Leg(
                ticker: ticker,
                venue: venue,
                source: source,
                priceUsdcMicros: nil,
                confidence: nil,
                freshness: reason,
                isLive: false,
                unavailableReason: reason
            )
        }

        let isLive = quote.status == .live
        return Leg(
            ticker: ticker,
            venue: venue,
            source: source,
            priceUsdcMicros: quote.priceUsdcMicros,
            // Jupiter publishes no interval, so a leg it sourced never claims one
            // even if a future payload carries a stray number.
            confidence: quote.source == .jupiter ? nil : confidenceCopy(quote.confUsdcMicros),
            freshness: freshnessCopy(quote, isEquity: isEquity, market: market, locale: locale, timeZone: timeZone),
            isLive: isLive,
            unavailableReason: nil
        )
    }

    /// Never "Pyth" for a price Pyth did not publish. The Jupiter fallback is the
    /// on-chain price and says exactly that.
    private static func sourceLabel(_ source: ReferenceQuoteSource) -> String {
        switch source {
        case .pythEquity: return "Pyth equity feed"
        case .pythCrypto: return "Pyth crypto feed"
        case .jupiter: return "On-chain price"
        case .yahoo: return "Yahoo Finance"
        case .unknown: return "Reference price"
        }
    }

    /// "±$0.03". Pyth's confidence is the single most bounty-relevant number on this
    /// card, so a zero interval is shown as no interval rather than as "±$0.00",
    /// which would read as a claim of perfect certainty.
    private static func confidenceCopy(_ micros: Int64?) -> String? {
        guard let micros, micros > 0 else { return nil }
        return "±\(UsdAmountFormatter.format(micros: micros))"
    }

    /// What the time on this leg means.
    ///
    /// A frozen equity print while the exchange is shut is the closing price, not a
    /// fault — that is the product's whole argument, and calling it stale here would
    /// argue against ourselves. A frozen print *during* the session is a real
    /// problem and says when the feed was last seen.
    private static func freshnessCopy(
        _ quote: ReferenceQuoteDTO,
        isEquity: Bool,
        market: MarketStatusDTO?,
        locale: Locale,
        timeZone: TimeZone
    ) -> String {
        if quote.status == .live { return "Live" }
        guard let publishedAt = quote.publishedAt else {
            return isEquity && marketIsShut(market) ? "Closed" : "Last price unknown"
        }
        let time = clockTime(publishedAt, locale: locale, timeZone: timeZone)
        if isEquity && marketIsShut(market) {
            return "Closed \(time)"
        }
        return "Last price \(time)"
    }

    private static func marketIsShut(_ market: MarketStatusDTO?) -> Bool {
        guard let market else { return false }
        return !market.isOpen && market.session != .unknown
    }

    /// Why there is no number, in words a member can act on.
    private static func unavailableCopy(_ quote: ReferenceQuoteDTO, isEquity: Bool) -> String {
        let instrument = isEquity ? "this stock" : "this token"
        switch quote.unavailableReason {
        case .noFeed:
            return "No Pyth feed for \(instrument) yet"
        case .notEntitled:
            return "This feed is not part of our plan"
        case .upstreamError:
            return "Price feed did not answer"
        case nil:
            return "No price right now"
        }
    }

    // MARK: - Premium

    private static func premium(_ quotes: StockVsTokenDTO, tokenTicker: String, underlying: String) -> Premium? {
        // The backend already refuses to compute one against a missing leg; this is
        // the second lock on the same door, because the pill is the one element on
        // the card that would silently read as measured if it were not.
        guard let basisPoints = quotes.premiumBps, quotes.equity.isPriced, quotes.token.isPriced else { return nil }

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
            caption = "\(tokenTicker) is trading in line with \(underlying)"
        case .above:
            caption = "\(tokenTicker) is trading \(magnitude)% above \(underlying)"
        case .below:
            caption = "\(tokenTicker) is trading \(magnitude)% below \(underlying)"
        }

        return Premium(basisPoints: basisPoints, label: label, caption: caption, direction: direction)
    }

    // MARK: - Footnote

    private static func footnote(
        _ quotes: StockVsTokenDTO,
        equity: Leg,
        token: Leg,
        underlying: String,
        tokenTicker: String
    ) -> String {
        if !equity.isPriced {
            return "Without a \(underlying) price there is nothing to compare \(tokenTicker) against."
        }
        if !token.isPriced {
            return "Without a \(tokenTicker) price there is nothing to compare against \(underlying)."
        }
        if !equity.isLive && token.isLive {
            return "\(underlying) stopped printing at the bell. \(tokenTicker) keeps trading on Solana, which is why the two can drift apart."
        }
        return "\(tokenTicker) is a token that tracks \(underlying). Supply and demand on Solana move it a little either side of the stock."
    }

    // MARK: - Time

    /// The publish instant in the reader's own clock. `j` is the locale's hour field,
    /// so a 24-hour locale reads "16:00" and never "4:00 PM".
    private static func clockTime(_ date: Date, locale: Locale, timeZone: TimeZone) -> String {
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
