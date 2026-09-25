import Foundation
import XCTest
@testable import MonacoCore

/// The Pyth card's rules, each one a thing the card must never say.
final class StockVsTokenCardTests: XCTestCase {
    private let posix = Locale(identifier: "en_US_POSIX")
    private let newYork = TimeZone(identifier: "America/New_York")!

    private func card(
        _ quotes: StockVsTokenDTO?,
        market: MarketStatusDTO? = MarketSampleData.sessionOpen,
        symbol: String = "AAPLx",
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

    func testLiveComparison_namesBothVenuesAndFeeds() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenLive))

        XCTAssertEqual(card.equity.ticker, "AAPL")
        XCTAssertEqual(card.equity.venue, "Apple on its home exchange")
        XCTAssertEqual(card.equity.source, "Pyth equity feed")
        XCTAssertEqual(card.equity.priceUsdcMicros, 231_400_000)
        XCTAssertEqual(card.equity.freshness, "Live")
        XCTAssertTrue(card.equity.isLive)

        XCTAssertEqual(card.token.ticker, "AAPLx")
        XCTAssertEqual(card.token.venue, "Tokenized, trading on Solana")
        XCTAssertEqual(card.token.source, "Pyth crypto feed")
        XCTAssertEqual(card.token.priceUsdcMicros, 232_050_000)
    }

    /// Pyth's confidence interval is the card's bounty-relevant number. It is shown
    /// as a band, never as a bare number, and never as "±$0.00".
    func testConfidence_readsAsABandAndNeverAsPerfectCertainty() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenLive))
        XCTAssertEqual(card.equity.confidence, "±$0.03")
        XCTAssertEqual(card.token.confidence, "±$0.05")

        let zeroConf = StockVsTokenDTO(
            equity: ReferenceQuoteDTO(source: .pythEquity, status: .live, priceUsdcMicros: 231_400_000, confUsdcMicros: 0),
            token: ReferenceQuoteDTO(source: .pythCrypto, status: .live, priceUsdcMicros: 232_050_000)
        )
        let bare = try XCTUnwrap(self.card(zeroConf))
        XCTAssertNil(bare.equity.confidence, "a zero interval is no interval, not a claim of certainty")
    }

    /// The state the whole card exists for: after the bell the equity print is the
    /// closing price, not an error, and the token keeps moving.
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
        XCTAssertEqual(card.token.freshness, "Live")
        XCTAssertTrue(card.token.isLive)
        XCTAssertTrue(
            card.footnote.contains("keeps trading on Solana"),
            "the footnote is the product's argument; got \(card.footnote)"
        )
    }

    /// A frozen equity print while the exchange is *open* is a real fault and must
    /// not borrow the reassuring after-hours wording.
    func testStaleDuringTheSession_saysWhenThePriceWasLastSeen() throws {
        let stalled = StockVsTokenDTO(
            equity: ReferenceQuoteDTO(
                source: .pythEquity,
                status: .stale,
                priceUsdcMicros: 231_400_000,
                publishedAt: MarketSampleData.tradingTuesday.addingTimeInterval(-25 * 60)
            ),
            token: ReferenceQuoteDTO(source: .pythCrypto, status: .live, priceUsdcMicros: 232_050_000),
            premiumBps: 28
        )
        let card = try XCTUnwrap(card(stalled, market: MarketSampleData.sessionOpen))
        XCTAssertTrue(card.equity.freshness.hasPrefix("Last price 9:35"), card.equity.freshness)
        XCTAssertTrue(card.equity.freshness.uppercased().hasSuffix("AM"), card.equity.freshness)
    }

    /// A Jupiter price is the on-chain price. It is never labelled Pyth, and it never
    /// carries a confidence interval, because Jupiter publishes none.
    func testJupiterFallback_isNeverCalledPyth() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenJupiterFallback))
        XCTAssertEqual(card.token.source, "On-chain price")
        XCTAssertFalse(card.token.source.lowercased().contains("pyth"))
        XCTAssertNil(card.token.confidence)
    }

    /// The equity leg off Yahoo's day chart is a stock price and says whose it is;
    /// it is never labelled Pyth either.
    func testYahooEquityLeg_isLabelledYahoo() throws {
        let fromChart = StockVsTokenDTO(
            equity: ReferenceQuoteDTO(source: .yahoo, status: .stale, priceUsdcMicros: 178_200_000),
            token: ReferenceQuoteDTO(source: .jupiter, status: .live, priceUsdcMicros: 179_050_000)
        )
        let card = try XCTUnwrap(card(fromChart))
        XCTAssertEqual(card.equity.source, "Yahoo Finance")
        XCTAssertFalse(card.equity.source.lowercased().contains("pyth"))
        XCTAssertEqual(card.equity.priceUsdcMicros, 178_200_000)
    }

    func testJupiterLeg_dropsAStrayConfidenceInterval() throws {
        let odd = StockVsTokenDTO(
            equity: ReferenceQuoteDTO(source: .pythEquity, status: .live, priceUsdcMicros: 178_200_000),
            token: ReferenceQuoteDTO(source: .jupiter, status: .live, priceUsdcMicros: 179_050_000, confUsdcMicros: 40_000)
        )
        let card = try XCTUnwrap(card(odd))
        XCTAssertNil(card.token.confidence, "Jupiter publishes no interval; a number here would be invented")
    }

    /// An unavailable leg shows the reason and no number at all.
    func testUnavailableEquity_showsTheReasonAndNoPriceAndNoPremium() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenEquityUnavailable))

        XCTAssertNil(card.equity.priceUsdcMicros)
        XCTAssertEqual(card.equity.unavailableReason, "This feed is not part of our plan")
        XCTAssertNil(card.premium, "a premium against a missing leg is a number nobody measured")
        XCTAssertTrue(card.footnote.contains("nothing to compare"), "got \(card.footnote)")
    }

    func testUnavailableReasons_eachGetTheirOwnSentence() throws {
        func reason(_ raw: ReferenceQuoteReason) throws -> String {
            let quotes = StockVsTokenDTO(
                equity: ReferenceQuoteDTO(source: .pythEquity, status: .unavailable, reason: raw.rawValue),
                token: ReferenceQuoteDTO(source: .pythCrypto, status: .live, priceUsdcMicros: 232_050_000)
            )
            return try XCTUnwrap(card(quotes)?.equity.unavailableReason)
        }
        XCTAssertEqual(try reason(.noFeed), "No Pyth feed for this stock yet")
        XCTAssertEqual(try reason(.notEntitled), "This feed is not part of our plan")
        XCTAssertEqual(try reason(.upstreamError), "Price feed did not answer")
    }

    func testBothLegsMissing_makesNoCard() {
        let nothing = StockVsTokenDTO(
            equity: ReferenceQuoteDTO(source: .pythEquity, status: .unavailable, reason: ReferenceQuoteReason.noFeed.rawValue),
            token: ReferenceQuoteDTO(source: .pythCrypto, status: .unavailable, reason: ReferenceQuoteReason.noFeed.rawValue)
        )
        XCTAssertNil(card(nothing), "two dashes and a disclaimer is not a card")
    }

    func testPremium_readsAsASentenceAndKeepsItsSign() throws {
        let above = try XCTUnwrap(card(MarketSampleData.stockVsTokenLive)?.premium)
        XCTAssertEqual(above.label, "+0.28%")
        XCTAssertEqual(above.direction, .above)
        XCTAssertEqual(above.caption, "AAPLx is trading 0.28% above AAPL")

        let discounted = StockVsTokenDTO(
            equity: ReferenceQuoteDTO(source: .pythEquity, status: .live, priceUsdcMicros: 231_400_000),
            token: ReferenceQuoteDTO(source: .pythCrypto, status: .live, priceUsdcMicros: 228_000_000),
            premiumBps: -147
        )
        let below = try XCTUnwrap(card(discounted)?.premium)
        XCTAssertEqual(below.label, "\u{2212}1.47%", "a loss uses a typographic minus, never a hyphen")
        XCTAssertEqual(below.direction, .below)
        XCTAssertEqual(below.caption, "AAPLx is trading 1.47% below AAPL")
    }

    func testPremiumOfZero_readsAsInLineRatherThanAsAGain() throws {
        let flat = StockVsTokenDTO(
            equity: ReferenceQuoteDTO(source: .pythEquity, status: .live, priceUsdcMicros: 231_400_000),
            token: ReferenceQuoteDTO(source: .pythCrypto, status: .live, priceUsdcMicros: 231_400_000),
            premiumBps: 0
        )
        let premium = try XCTUnwrap(card(flat)?.premium)
        XCTAssertEqual(premium.label, "0.00%")
        XCTAssertEqual(premium.direction, .inline)
        XCTAssertEqual(premium.caption, "AAPLx is trading in line with AAPL")
    }

    /// The backend refuses a premium against a missing leg; this is the second lock
    /// on the same door, because the pill is the element that would silently read as
    /// measured.
    func testPremiumIsRefusedWhenALegHasNoPrice_evenIfTheServerSentOne() {
        let contradictory = StockVsTokenDTO(
            equity: ReferenceQuoteDTO(source: .pythEquity, status: .unavailable, reason: ReferenceQuoteReason.noFeed.rawValue),
            token: ReferenceQuoteDTO(source: .pythCrypto, status: .live, priceUsdcMicros: 232_050_000),
            premiumBps: 28
        )
        XCTAssertNil(card(contradictory)?.premium)
    }

    func testSpokenLeg_readsAsOneSentence() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenLive))
        XCTAssertEqual(card.equity.spoken, "AAPL, Apple on its home exchange, $231.40, Live, give or take $0.03")

    }

    func testSpokenLeg_withNoPrice_saysWhyRatherThanReadingNothing() throws {
        let card = try XCTUnwrap(card(MarketSampleData.stockVsTokenEquityUnavailable))
        XCTAssertEqual(card.equity.spoken, "AAPL, Apple on its home exchange. This feed is not part of our plan")
    }

    /// A 24-hour locale must not be handed "4:00 PM".
    func testClosingTime_followsTheReadersClock() throws {
        let card = StockVsTokenCard.make(
            symbol: "AAPLx",
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
}
