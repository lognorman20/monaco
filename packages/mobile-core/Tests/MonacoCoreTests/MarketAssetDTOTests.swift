import XCTest
@testable import MonacoCore

final class MarketAssetDTOTests: XCTestCase {
    func testMarketAssetDTO_decodesTokenAddress() throws {
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_assets", withExtension: "json")
        )
        let dto = try JSONDecoder().decode(
            ListMarketAssetsResponseDTO.self,
            from: Data(contentsOf: fixtureURL)
        )
        XCTAssertNotNil(dto.assets[0].tokenAddress)
        XCTAssertFalse(dto.assets[0].tokenAddress.isEmpty)
    }

    func testListMarketAssets_decodesCatalogPage() throws {
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_assets", withExtension: "json")
        )
        let dto = try JSONDecoder().decode(
            ListMarketAssetsResponseDTO.self,
            from: Data(contentsOf: fixtureURL)
        )

        XCTAssertEqual(dto.assets.count, 1)
        XCTAssertEqual(dto.assets[0].symbol, "AAPLc")
        XCTAssertFalse(dto.assets[0].tokenAddress.isEmpty)
        XCTAssertEqual(dto.assets[0].priceUsdcMicros, 185_000_000)
        XCTAssertTrue(dto.hasMore)
        XCTAssertEqual(dto.market?.session, .open)
    }

    func testAssetDetail_decodesLiquiditySnippet() throws {
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_asset_detail", withExtension: "json")
        )
        let dto = try JSONDecoder().decode(AssetDetailDTO.self, from: Data(contentsOf: fixtureURL))

        XCTAssertEqual(dto.liquidity.label, "Via DEX")
        XCTAssertEqual(dto.liquidity.buyProbeOutAmount, "427533")
        XCTAssertEqual(dto.liquidity.spreadBps, 59)
        // The fixture is a response the server could actually emit: the backend
        // derives the card's bid, ask and spread from these same two probes, so
        // 1_000_000 USDC micros buying 427_533 atomics is the ask on the card.
        XCTAssertEqual(dto.stockVsToken?.token.askUsdcMicros, 233_900_073)
        XCTAssertEqual(dto.stockVsToken?.token.bidUsdcMicros, 232_520_000)
        // Stale equity, so no confidence interval in the grid.
        XCTAssertNil(dto.stats?.confUsdcMicros)
        XCTAssertTrue(dto.routable)
        XCTAssertEqual(dto.marketSession, .afterHours)
        XCTAssertTrue(dto.afterHours)
        XCTAssertEqual(dto.stats?.previousCloseUsdcMicros, 226_500_000)
        XCTAssertEqual(dto.stats?.basisCaption, "AAPL on its home exchange")
        XCTAssertEqual(dto.stockVsToken?.equity.status, .stale)
        XCTAssertEqual(dto.stockVsToken?.token.source, .dexKyber)
        XCTAssertEqual(dto.stockVsToken?.mark.source, .chainlinkTRV)
        XCTAssertEqual(dto.stockVsToken?.premiumBps, 50)
        XCTAssertEqual(dto.stockVsToken?.spreadBps, dto.liquidity.spreadBps)
        XCTAssertNotNil(dto.stockVsToken?.mark.publishedAt)
        XCTAssertNotNil(dto.stockVsToken?.token.probedAt)
        XCTAssertEqual(dto.stockDayMove?.caption, "AAPL day move")
    }

    func testAssetChart_decodesPoints() throws {
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_asset_chart", withExtension: "json")
        )
        let dto = try JSONDecoder().decode(AssetChartDTO.self, from: Data(contentsOf: fixtureURL))

        XCTAssertEqual(dto.points.count, 2)
        XCTAssertNil(dto.emptyReason)
        XCTAssertEqual(dto.points[1].priceUsdcMicros, 185_000_000)
        XCTAssertEqual(dto.previousCloseUsdcMicros, 179_000_000)
        XCTAssertEqual(dto.range, .oneDay)
        XCTAssertEqual(dto.source, .benchmarks)
        XCTAssertEqual(dto.basis, .underlying)
        XCTAssertTrue(dto.points.allSatisfy(\.hasCandle))
    }

    func testAssetChart_decodesEmptySeriesWithReason() throws {
        let json = """
        {
          "points": [],
          "emptyReason": "price history unavailable"
        }
        """
        let dto = try JSONDecoder().decode(AssetChartDTO.self, from: Data(json.utf8))
        XCTAssertEqual(dto.points, [])
        XCTAssertEqual(dto.emptyReason, "price history unavailable")
    }

    // MARK: - Whether a Kyber probe actually pays out

    /// The backend writes `sellProbeOutAmount` for any routable quote that carried an
    /// amount out, so "0" is a real value on the wire. Its own pricing step rejects it
    /// (`microsPerToken` wants `Sign() > 0`) and reports the token leg unavailable, so a
    /// caller that only checks the field is present would claim a market the same
    /// response has already priced at nothing.
    func testSellRoutePaysOut_rejectsAZeroPayoutFromARoutableQuote() {
        let zero = AssetLiquidityDTO(
            label: "Kyber",
            routable: true,
            buyProbeUsdcMicros: 100_000_000,
            buyProbeOutAmount: "100000000",
            sellProbeInAmount: "100000000",
            sellProbeOutAmount: "0"
        )
        XCTAssertFalse(zero.sellRoutePaysOut)
        XCTAssertFalse(zero.routesBothWays)
    }

    func testSellRoutePaysOut_acceptsAPositivePayout() {
        let paid = AssetLiquidityDTO(
            label: "Kyber",
            routable: true,
            buyProbeUsdcMicros: 100_000_000,
            buyProbeOutAmount: "100000000",
            sellProbeInAmount: "100000000",
            sellProbeOutAmount: "185000000"
        )
        XCTAssertTrue(paid.sellRoutePaysOut)
        XCTAssertTrue(paid.routesBothWays)
    }

    /// The buy side is `routable`'s own business; with no sell route at all there is no
    /// way out and the pair is still half a market.
    func testRoutesBothWays_needsASellSide() {
        let buyOnly = AssetLiquidityDTO(
            label: "Kyber",
            routable: true,
            buyProbeUsdcMicros: 100_000_000,
            buyProbeOutAmount: "100000000",
            sellProbeInAmount: nil,
            sellProbeOutAmount: nil
        )
        XCTAssertFalse(buyOnly.routesBothWays)
    }

    /// Atomic amounts come off a `big.Int`, so they are matched digit-wise. A value past
    /// `Int64.max` is a large payout, not a parse failure to be read as "no route".
    func testIsPositiveAtomicAmount_handlesTheWireFormat() {
        XCTAssertTrue(AssetLiquidityDTO.isPositiveAtomicAmount("1"))
        XCTAssertTrue(AssetLiquidityDTO.isPositiveAtomicAmount("  185000000  "))
        XCTAssertTrue(AssetLiquidityDTO.isPositiveAtomicAmount("+42"))
        XCTAssertTrue(AssetLiquidityDTO.isPositiveAtomicAmount("99999999999999999999999999"))
        XCTAssertFalse(AssetLiquidityDTO.isPositiveAtomicAmount("0"))
        XCTAssertFalse(AssetLiquidityDTO.isPositiveAtomicAmount("000"))
        XCTAssertFalse(AssetLiquidityDTO.isPositiveAtomicAmount("-5"))
        XCTAssertFalse(AssetLiquidityDTO.isPositiveAtomicAmount("1.5"))
        XCTAssertFalse(AssetLiquidityDTO.isPositiveAtomicAmount(""))
        XCTAssertFalse(AssetLiquidityDTO.isPositiveAtomicAmount("   "))
        XCTAssertFalse(AssetLiquidityDTO.isPositiveAtomicAmount(nil))
        XCTAssertFalse(AssetLiquidityDTO.isPositiveAtomicAmount("1e9"))
    }

    // MARK: - The as-of line under a held mark

    /// Fixed locale and zone, so this asserts the shape of the line rather than the
    /// machine's regional settings.
    private static let easternUS = TimeZone(identifier: "America/New_York")!
    private static let enUS = Locale(identifier: "en_US")

    /// ICU puts a narrow no-break space (U+202F) before the AM/PM marker, which is right
    /// on screen and invisible in a test failure. Compare on plain spaces so a mismatch
    /// reads as a mismatch.
    private func plainSpaces(_ text: String?) -> String? {
        text?
            .replacingOccurrences(of: "\u{202F}", with: " ")
            .replacingOccurrences(of: "\u{00A0}", with: " ")
    }

    /// The weekend sample is the case this exists for: the Chainlink total-return mark
    /// holding Friday's last round while the screen keeps rolling its digits.
    func testStaleMarkCaption_namesTheDayAndTimeTheMarkLastPrinted() {
        let mark = MarketSampleData.stockVsTokenWeekend.mark
        XCTAssertEqual(mark.status, .stale)
        let caption = StaleMarkCaption.caption(
            status: mark.status,
            publishedAt: mark.publishedAt,
            locale: Self.enUS,
            timeZone: Self.easternUS
        )
        XCTAssertEqual(plainSpaces(caption), "As of Fri 7:59 PM")
    }

    /// A live mark needs no qualifier — the price is the price.
    func testStaleMarkCaption_isSilentForALiveMark() {
        XCTAssertNil(
            StaleMarkCaption.caption(
                status: .live,
                publishedAt: Date(timeIntervalSince1970: 1_790_380_740),
                locale: Self.enUS,
                timeZone: Self.easternUS
            )
        )
    }

    /// A time the source did not send is not guessed at: better no line than a made-up
    /// one under a real price.
    func testStaleMarkCaption_needsATimeItCanName() {
        XCTAssertNil(
            StaleMarkCaption.caption(
                status: .stale,
                publishedAt: nil,
                locale: Self.enUS,
                timeZone: Self.easternUS
            )
        )
        XCTAssertNil(
            StaleMarkCaption.caption(
                status: .unavailable,
                publishedAt: Date(timeIntervalSince1970: 1_790_380_740),
                locale: Self.enUS,
                timeZone: Self.easternUS
            )
        )
        XCTAssertNil(
            StaleMarkCaption.caption(
                status: nil,
                publishedAt: Date(timeIntervalSince1970: 1_790_380_740),
                locale: Self.enUS,
                timeZone: Self.easternUS
            )
        )
    }

    /// Timestamps travel as UTC and are converted at the point of display, so the same
    /// instant reads as a different local clock time — and the line has to be the
    /// reader's, not the exchange's.
    func testStaleMarkCaption_isRenderedInTheReadersZone() {
        let instant = Date(timeIntervalSince1970: 1_790_380_740)
        let newYork = StaleMarkCaption.caption(
            status: .stale, publishedAt: instant, locale: Self.enUS, timeZone: Self.easternUS
        )
        let tokyo = StaleMarkCaption.caption(
            status: .stale,
            publishedAt: instant,
            locale: Self.enUS,
            timeZone: TimeZone(identifier: "Asia/Tokyo")!
        )
        XCTAssertEqual(plainSpaces(newYork), "As of Fri 7:59 PM")
        XCTAssertEqual(plainSpaces(tokyo), "As of Sat 8:59 AM")
    }
}
