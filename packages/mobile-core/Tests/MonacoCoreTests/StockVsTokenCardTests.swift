import Foundation
import XCTest
@testable import MonacoCore

/// The stock-vs-token card's rules, each one a thing the card must never say.
final class StockVsTokenCardTests: XCTestCase {
    private let posix = Locale(identifier: "en_US_POSIX")
    private let newYork = TimeZone(identifier: "America/New_York")!

    private func card(
        _ quotes: StockVsTokenDTO?,
        market: MarketStatusDTO? = MarketSampleData.sessionOpen,
        symbol: String = "AAPLc",
        name: String = "Apple"
    ) -> StockVsTokenCard? {
        StockVsTokenCard.make(
            symbol: symbol,
            name: name,
            quotes: quotes,
            market: market,
            locale: posix,
            timeZone: newYork
        )
    }

    func testNoQuotes_makesNoCard() {
        XCTAssertNil(card(nil))
    }

    func testLiveComparison_namesEachLegsVenueAndFeed() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenLive))

        XCTAssertEqual(card.token.ticker, "AAPLc")
        XCTAssertEqual(card.token.venue, "Tokenized, trading on Base")
        XCTAssertEqual(card.token.source, "Kyber quote")
        XCTAssertEqual(card.token.priceUsdcMicros, 231_829_069)

        XCTAssertEqual(card.mark.ticker, "AAPLc")
        XCTAssertEqual(card.mark.source, "Chainlink total-return feed")
        XCTAssertEqual(card.mark.priceUsdcMicros, 232_050_000)

        XCTAssertEqual(card.equity.ticker, "AAPL")
        XCTAssertEqual(card.equity.venue, "Apple on its home exchange")
        XCTAssertEqual(card.equity.source, "Pyth equity feed")
        XCTAssertEqual(card.equity.priceUsdcMicros, 231_400_000)
        XCTAssertTrue(card.equity.isReferenceOnly, "the equity line is per share and nothing is measured against it")
    }

    /// Pyth publishes no feed for a B20 token, and the Solana xStocks are a
    /// different instrument on a different chain. Nothing on this card may say
    /// either.
    func testCopy_neverNamesAPythTokenFeedOrSolana() throws {
        let cards = [
            try XCTUnwrap(card(MarketSampleData.stockVsTokenLive)),
            try XCTUnwrap(card(MarketSampleData.stockVsTokenAfterHours, market: MarketSampleData.sessionAfterHours)),
            try XCTUnwrap(card(MarketSampleData.stockVsTokenWeekend, market: MarketSampleData.sessionWeekend)),
            try XCTUnwrap(card(MarketSampleData.stockVsTokenEquityUnavailable)),
        ]
        let banned = ["solana", "jupiter", "xstock", "crypto feed", "on-chain price"]
        for card in cards {
            var strings = [card.footnote]
            strings += card.legs.flatMap { [$0.source, $0.venue, $0.freshness, $0.spoken, $0.unavailableReason ?? ""] }
            strings += [card.premium?.caption ?? "", card.spread ?? ""]
            for text in strings {
                for word in banned {
                    XCTAssertFalse(
                        text.lowercased().contains(word),
                        "card copy must not say \"\(word)\": \(text)"
                    )
                }
            }
        }
        // And the one label that is easiest to get wrong: Pyth may only ever be
        // named on the equity line.
        let live = try XCTUnwrap(card(MarketSampleData.stockVsTokenLive))
        XCTAssertFalse(live.token.source.lowercased().contains("pyth"))
        XCTAssertFalse(live.mark.source.lowercased().contains("pyth"))
        XCTAssertTrue(live.equity.source.lowercased().contains("pyth"))
    }

    /// Pyth's confidence interval is the card's bounty-relevant number. It is shown
    /// as a band, only on the leg that publishes one, and never as "±$0.00".
    func testConfidence_onlyOnTheEquityLegAndNeverAsPerfectCertainty() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenLive))
        XCTAssertEqual(card.equity.confidence, "±$0.03")
        XCTAssertNil(card.token.confidence, "Kyber publishes no interval")
        XCTAssertNil(card.mark.confidence, "a Chainlink round publishes no interval")

        let zeroConf = StockVsTokenDTO(
            token: MarketSampleData.stockVsTokenLive.token,
            mark: MarketSampleData.appleMark,
            equity: ReferenceQuoteDTO(source: .pythEquity, status: .live, priceUsdcMicros: 231_400_000, confUsdcMicros: 0),
            equitySymbol: "AAPL"
        )
        let bare = try XCTUnwrap(self.card(zeroConf))
        XCTAssertNil(bare.equity.confidence, "a zero interval is no interval, not a claim of certainty")
    }

    /// A stray interval on a leg whose source publishes none is dropped rather than
    /// drawn: a band nobody measured is worse than no band.
    func testNonPythLeg_dropsAStrayConfidenceInterval() throws {
        let odd = StockVsTokenDTO(
            token: ReferenceQuoteDTO(source: .dexKyber, status: .live, priceUsdcMicros: 179_050_000, confUsdcMicros: 40_000),
            mark: ReferenceQuoteDTO(source: .chainlinkTRV, status: .live, priceUsdcMicros: 179_000_000, confUsdcMicros: 90_000),
            equity: ReferenceQuoteDTO(source: .pythEquity, status: .live, priceUsdcMicros: 178_200_000),
            equitySymbol: "AAPL"
        )
        let card = try XCTUnwrap(card(odd))
        XCTAssertNil(card.token.confidence)
        XCTAssertNil(card.mark.confidence)
    }

    /// The state the whole card exists for: after the bell the equity print is the
    /// closing price, not an error, and the token keeps moving on Base.
    func testAfterHours_equityReadsAsClosedNotAsStale() throws {
        let card = try XCTUnwrap(card(
            MarketSampleData.stockVsTokenAfterHours,
            market: MarketSampleData.sessionAfterHours
        ))

        // The meridiem separator is ICU's — a narrow no-break space, not a space —
        // so the assertion is on the parts rather than on a literal nobody can type.
        XCTAssertTrue(card.equity.freshness.hasPrefix("Closed 4:00"), card.equity.freshness)
        XCTAssertTrue(card.equity.freshness.uppercased().hasSuffix("PM"), card.equity.freshness)
        XCTAssertFalse(card.equity.isLive)
        XCTAssertTrue(card.token.isLive)
        XCTAssertTrue(
            card.footnote.contains("keeps trading on Base"),
            "the footnote is the product's argument; got \(card.footnote)"
        )
    }

    /// A Kyber leg has no publish time, only a probe time, and says so rather than
    /// borrowing the word "Live" for a quote taken a minute ago.
    func testKyberLeg_readsAsAQuoteTimeNotAPublishTime() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenLive))
        XCTAssertTrue(card.token.freshness.hasPrefix("Quoted 10:00"), card.token.freshness)
    }

    /// A frozen mark while the exchange is *open* is a real fault and must not borrow
    /// the reassuring after-hours wording.
    func testStaleDuringTheSession_saysWhenThePriceWasLastSeen() throws {
        let stalled = StockVsTokenDTO(
            token: MarketSampleData.stockVsTokenLive.token,
            mark: ReferenceQuoteDTO(
                source: .chainlinkTRV,
                status: .stale,
                priceUsdcMicros: 232_050_000,
                publishedAt: MarketSampleData.tradingTuesday.addingTimeInterval(-25 * 60)
            ),
            equity: MarketSampleData.appleEquityLive,
            equitySymbol: "AAPL"
        )
        let card = try XCTUnwrap(card(stalled, market: MarketSampleData.sessionOpen))
        XCTAssertTrue(card.mark.freshness.hasPrefix("Last price 9:35"), card.mark.freshness)
        XCTAssertTrue(card.mark.freshness.uppercased().hasSuffix("AM"), card.mark.freshness)
    }

    // MARK: - The premium's basis

    /// The premium is the token against its own mark. Both are per token and both
    /// carry the B20 multiplier; the equity is per share. Measured against the
    /// equity, every reinvested dividend would read as a premium that never closes.
    func testPremium_isMeasuredAgainstTheMarkAndNotAgainstTheEquity() throws {
        // The server's figure is -10 bps: the pools are under the mark, while the
        // token is *above* the equity. If the card ever recomputed against the
        // equity line this would come back positive.
        let live = MarketSampleData.stockVsTokenLive
        XCTAssertGreaterThan(live.token.priceUsdcMicros ?? 0, live.equity.priceUsdcMicros ?? 0,
                             "fixture must have the token above the equity for this test to bite")

        let premium = try XCTUnwrap(card(live)?.premium)
        XCTAssertEqual(premium.basisPoints, -10)
        XCTAssertEqual(premium.direction, .below)
        XCTAssertEqual(premium.label, "\u{2212}0.10%", "a discount uses a typographic minus, never a hyphen")
        XCTAssertEqual(premium.caption, "AAPLc is trading 0.10% below its mark")
        XCTAssertFalse(premium.caption.contains("AAPL "), "the caption must not name the share: \(premium.caption)")
    }

    /// The card consumes the server's basis points and never derives its own. A
    /// client-side recompute is how the basis drifts back onto the equity leg.
    func testPremium_usesTheServersFigureAndDoesNotRecompute() throws {
        let contradictory = StockVsTokenDTO(
            token: ReferenceQuoteDTO(source: .dexKyber, status: .live, priceUsdcMicros: 300_000_000, probedAt: MarketSampleData.tradingTuesday),
            mark: ReferenceQuoteDTO(source: .chainlinkTRV, status: .live, priceUsdcMicros: 100_000_000),
            equity: MarketSampleData.appleEquityLive,
            equitySymbol: "AAPL",
            // The prices imply +20000 bps. The server says 42, and the server is the
            // one place the basis is decided.
            premiumBps: 42
        )
        let premium = try XCTUnwrap(card(contradictory)?.premium)
        XCTAssertEqual(premium.basisPoints, 42)
    }

    func testPremiumOfZero_readsAsInLineRatherThanAsAGain() throws {
        let flat = StockVsTokenDTO(
            token: ReferenceQuoteDTO(source: .dexKyber, status: .live, priceUsdcMicros: 232_050_000, probedAt: MarketSampleData.tradingTuesday),
            mark: MarketSampleData.appleMark,
            equity: MarketSampleData.appleEquityLive,
            equitySymbol: "AAPL",
            premiumBps: 0
        )
        let premium = try XCTUnwrap(card(flat)?.premium)
        XCTAssertEqual(premium.label, "0.00%")
        XCTAssertEqual(premium.direction, .inline)
        XCTAssertEqual(premium.caption, "AAPLc is trading in line with its mark")
    }

    /// The footnote explains why the two units differ. It must not go further and
    /// claim the token is worth *more* than a share: the multiplier starts at one and
    /// only moves on a split or a reinvested dividend, so for a stock that has had
    /// neither the two are the same number.
    func testFootnote_doesNotClaimTheTokenIsWorthMoreThanAShare() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenLive))
        let text = card.footnote.lowercased()
        XCTAssertFalse(text.contains("worth more than"), card.footnote)
        XCTAssertTrue(
            card.footnote.contains("need not be the same number"),
            "the footnote should say the two units can differ, without saying which way: \(card.footnote)"
        )
    }

    /// Over a weekend the mark holds Friday's close. The gap to the pools is the
    /// market's move since then, not a premium, and no pill may claim otherwise.
    func testStaleMark_hasNoPremiumAndSaysWhy() throws {
        let card = try XCTUnwrap(card(
            MarketSampleData.stockVsTokenWeekend,
            market: MarketSampleData.sessionWeekend
        ))
        XCTAssertNil(card.premium, "a mark holding the last close cannot carry a premium")
        XCTAssertTrue(card.footnote.contains("holding its last print"), card.footnote)
    }

    /// The backend refuses a premium against a missing leg; this is the second lock
    /// on the same door, because the pill is the element that would silently read as
    /// measured.
    func testPremiumIsRefusedWhenALegHasNoPrice_evenIfTheServerSentOne() {
        let contradictory = StockVsTokenDTO(
            token: ReferenceQuoteDTO(source: .dexKyber, status: .unavailable, reason: ReferenceQuoteReason.noRoute.rawValue),
            mark: MarketSampleData.appleMark,
            equity: MarketSampleData.appleEquityLive,
            equitySymbol: "AAPL",
            premiumBps: 28
        )
        XCTAssertNil(card(contradictory)?.premium)
    }

    /// The equity line is a reference. Losing it costs the card its reference, not
    /// its premium: the comparison that matters is between the two per-token legs.
    func testUnavailableEquity_keepsThePremiumAndExplainsTheMissingLine() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenEquityUnavailable))

        XCTAssertNil(card.equity.priceUsdcMicros)
        XCTAssertEqual(card.equity.unavailableReason, "This feed is not part of our plan")
        XCTAssertNotNil(card.premium, "the token and its mark are both priced; the premium stands")
    }

    func testUnavailableReasons_eachGetTheirOwnSentence() throws {
        func equityReason(_ raw: ReferenceQuoteReason) throws -> String {
            let quotes = StockVsTokenDTO(
                token: MarketSampleData.stockVsTokenLive.token,
                mark: MarketSampleData.appleMark,
                equity: ReferenceQuoteDTO(source: .pythEquity, status: .unavailable, reason: raw.rawValue),
                equitySymbol: "AAPL"
            )
            return try XCTUnwrap(card(quotes)?.equity.unavailableReason)
        }
        XCTAssertEqual(try equityReason(.noFeed), "No Pyth feed for this stock yet")
        XCTAssertEqual(try equityReason(.notEntitled), "This feed is not part of our plan")
        XCTAssertEqual(try equityReason(.notConfigured), "This feed is not set up yet")
        XCTAssertEqual(try equityReason(.upstreamError), "Price feed did not answer")

        let noRoute = StockVsTokenDTO(
            token: ReferenceQuoteDTO(source: .dexKyber, status: .unavailable, reason: ReferenceQuoteReason.noRoute.rawValue),
            mark: MarketSampleData.appleMark,
            equity: MarketSampleData.appleEquityLive,
            equitySymbol: "AAPL"
        )
        XCTAssertEqual(try XCTUnwrap(card(noRoute)?.token.unavailableReason), "No route on Base for this token")
    }

    func testEveryLegMissing_makesNoCard() {
        let unavailable = ReferenceQuoteReason.noFeed.rawValue
        let nothing = StockVsTokenDTO(
            token: ReferenceQuoteDTO(source: .dexKyber, status: .unavailable, reason: unavailable),
            mark: ReferenceQuoteDTO(source: .chainlinkTRV, status: .unavailable, reason: unavailable),
            equity: ReferenceQuoteDTO(source: .pythEquity, status: .unavailable, reason: unavailable)
        )
        XCTAssertNil(card(nothing), "three dashes and a disclaimer is not a card")
    }

    // MARK: - Spread

    /// The spread is named as the two sides it sits between, never as a fee: it is
    /// one buy probe against one sell probe at one size.
    func testSpread_isNamedAsTheGapBetweenTheProbes() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenLive))
        XCTAssertEqual(card.spread, "Ask 0.63% over bid")
        XCTAssertFalse(card.spread?.lowercased().contains("fee") ?? false)
    }

    func testSpread_absentWhenTheBackendDidNotSendOne() throws {
        let noSpread = StockVsTokenDTO(
            token: MarketSampleData.stockVsTokenLive.token,
            mark: MarketSampleData.appleMark,
            equity: MarketSampleData.appleEquityLive,
            equitySymbol: "AAPL",
            premiumBps: -10
        )
        XCTAssertNil(try XCTUnwrap(card(noSpread)).spread)
    }

    // MARK: - VoiceOver and locale

    func testSpokenLeg_readsAsOneSentence() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenLive))
        XCTAssertEqual(
            card.equity.spoken,
            "AAPL, Apple on its home exchange, $231.40, Live, give or take $0.03"
        )
    }

    func testSpokenLeg_withNoPrice_saysWhyRatherThanReadingNothing() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenEquityUnavailable))
        XCTAssertEqual(card.equity.spoken, "AAPL, Apple on its home exchange. This feed is not part of our plan")
    }

    /// A 24-hour locale must not be handed "4:00 PM".
    func testClosingTime_followsTheReadersClock() throws {
        let card = StockVsTokenCard.make(
            symbol: "AAPLc",
            name: "Apple",
            quotes: MarketSampleData.stockVsTokenAfterHours,
            market: MarketSampleData.sessionAfterHours,
            locale: Locale(identifier: "en_GB"),
            timeZone: newYork
        )
        XCTAssertEqual(card?.equity.freshness, "Closed 16:00")
    }

    func testMissingCompanyName_stillNamesTheVenue() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenLive, name: "   "))
        XCTAssertEqual(card.equity.venue, "On its home exchange")
    }

    /// The underlying's ticker comes from the backend, which knows which feed it
    /// read, rather than from stripping a letter off the token's symbol.
    func testEquityTicker_comesFromTheBackendsOwnSymbol() throws {
        let renamed = StockVsTokenDTO(
            token: MarketSampleData.stockVsTokenLive.token,
            mark: MarketSampleData.appleMark,
            equity: MarketSampleData.appleEquityLive,
            equitySymbol: "SPCX"
        )
        let card = try XCTUnwrap(card(renamed, symbol: "SPCXc"))
        XCTAssertEqual(card.equity.ticker, "SPCX", "SPCXc's underlying is SPCX, not SPC")
    }
}
