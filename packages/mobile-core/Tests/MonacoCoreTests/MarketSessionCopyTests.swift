import XCTest
@testable import MonacoCore

/// What the chip under the hero price says in each session the calendar can produce.
///
/// The product argument lives in this copy — the stock stops, the B20 token does not —
/// so it is asserted rather than left to whoever edits the view next.
final class MarketSessionCopyTests: XCTestCase {
    private let easternUS = Locale(identifier: "en_US")
    private let newYork = TimeZone(identifier: "America/New_York")!

    private func chip(_ market: MarketStatusDTO?, tokenRoutable: Bool = true) -> MarketSessionChipCopy? {
        MarketSessionCopy.chip(for: market, tokenRoutable: tokenRoutable, locale: easternUS, timeZone: newYork)
    }

    func testOpenSaysNothingAboutBaseBecauseThereIsNothingToExplain() throws {
        let copy = try XCTUnwrap(chip(MarketSampleData.sessionOpen))

        XCTAssertEqual(copy.title, "Market open")
        XCTAssertTrue(copy.isLive)
        XCTAssertFalse(copy.spoken.contains("Base"))
    }

    func testOpenNamesTheBell() throws {
        let copy = try XCTUnwrap(chip(MarketSampleData.sessionOpen))
        let detail = try XCTUnwrap(copy.detail)

        // The sample's next transition is six hours after 10:00 ET. The space before
        // the meridiem is ICU's, narrow and non-breaking, so the assertion is on the
        // parts rather than on a literal nobody can see.
        XCTAssertTrue(detail.hasPrefix("Closes "), detail)
        XCTAssertTrue(detail.contains("4:00"), detail)
        XCTAssertTrue(detail.uppercased().contains("PM"), detail)
    }

    func testAHalfDaySaysSoInsteadOfNamingAnHour() throws {
        let copy = try XCTUnwrap(chip(MarketSampleData.sessionEarlyClose))

        XCTAssertEqual(copy.title, "Market open")
        XCTAssertEqual(copy.detail, "Closes early today")
    }

    func testAfterHoursIsTheStateTheWholeChipExistsFor() throws {
        let copy = try XCTUnwrap(chip(MarketSampleData.sessionAfterHours))

        XCTAssertEqual(copy.title, "After hours")
        XCTAssertEqual(copy.detail, "Token still trades on Base")
        XCTAssertFalse(copy.isLive)
    }

    func testPreMarketCountsDownToTheOpenWhenTheServerSaidWhen() throws {
        let copy = try XCTUnwrap(chip(MarketSampleData.sessionPreMarket))
        let detail = try XCTUnwrap(copy.detail)

        XCTAssertEqual(copy.title, "Pre-market")
        XCTAssertTrue(detail.hasPrefix("Opens "), detail)
        XCTAssertTrue(detail.contains("9:30"), detail)
        XCTAssertFalse(copy.isLive)
    }

    /// A transition already behind the instant the payload was priced at is a stale
    /// payload. "Opens half an hour ago" is worse than saying the token is trading.
    func testATransitionInThePastIsNotACountdown() throws {
        let market = MarketStatusDTO(
            session: .preMarket,
            isOpen: false,
            afterHours: true,
            nextSession: .open,
            nextTransition: Date(timeIntervalSince1970: 1_790_078_400),
            asOf: Date(timeIntervalSince1970: 1_790_085_600)
        )
        let copy = try XCTUnwrap(chip(market))

        XCTAssertEqual(copy.detail, "Token still trades on Base")
    }

    func testAHolidayIsNamed() throws {
        let copy = try XCTUnwrap(chip(MarketSampleData.sessionHoliday))

        XCTAssertEqual(copy.title, "Closed for Thanksgiving Day")
        XCTAssertEqual(copy.detail, "Token still trades on Base")
    }

    func testAnOrdinaryOvernightCloseIsJustClosed() throws {
        let copy = try XCTUnwrap(chip(MarketSampleData.sessionClosedOvernight))

        XCTAssertEqual(copy.title, "Market closed")
        XCTAssertFalse(copy.isLive)
    }

    /// An older backend sends no market block at all, and a session name added later
    /// decodes to `.unknown`. Both draw no chip rather than a guess.
    func testNoChipWithoutASession() {
        XCTAssertNil(chip(nil))
        XCTAssertNil(chip(MarketStatusDTO(session: .unknown, isOpen: false, afterHours: true)))
    }

    /// The pools are the claim. When the detail's Kyber probe found no route, the
    /// token is not trading anywhere, so the chip names the close and nothing else.
    func testWithoutARouteTheChipDoesNotSayTheTokenTrades() throws {
        for market in [
            MarketSampleData.sessionAfterHours,
            MarketSampleData.sessionWeekend,
            MarketSampleData.sessionHoliday,
            MarketSampleData.sessionClosedOvernight,
        ] {
            let copy = try XCTUnwrap(chip(market, tokenRoutable: false))
            XCTAssertNil(copy.detail, "\(market.session) said \(copy.detail ?? "")")
        }
        // A caller that says nothing about the pools gets no claim about them either.
        XCTAssertNil(MarketSessionCopy.chip(for: MarketSampleData.sessionAfterHours)?.detail)
    }

    /// No "24/7" and nothing about Solana: nothing checks a Base pool around the
    /// clock, and the token is not on Solana.
    func testNoClosedStateOverclaims() throws {
        for market in [
            MarketSampleData.sessionAfterHours,
            MarketSampleData.sessionWeekend,
            MarketSampleData.sessionHoliday,
            MarketSampleData.sessionClosedOvernight,
            MarketSampleData.sessionPreMarket,
        ] {
            let spoken = try XCTUnwrap(chip(market)).spoken
            XCTAssertFalse(spoken.contains("24/7"), spoken)
            XCTAssertFalse(spoken.contains("Solana"), spoken)
        }
    }

    func testSpokenFormJoinsBothHalves() {
        let copy = MarketSessionChipCopy(title: "After hours", detail: MarketSessionCopy.tokenTradesOnBase, isLive: false)

        XCTAssertEqual(copy.spoken, "After hours, Token still trades on Base")
        XCTAssertEqual(MarketSessionChipCopy(title: "Market open", detail: nil, isLive: true).spoken, "Market open")
    }

    func testA24HourLocaleGetsA24HourBell() throws {
        let copy = try XCTUnwrap(
            MarketSessionCopy.chip(
                for: MarketSampleData.sessionOpen,
                locale: Locale(identifier: "en_GB"),
                timeZone: TimeZone(identifier: "Europe/London")!
            )
        )
        let detail = try XCTUnwrap(copy.detail)

        XCTAssertTrue(detail.contains("21:00"), detail)
        XCTAssertFalse(detail.uppercased().contains("PM"), detail)
    }
}
