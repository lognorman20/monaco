import XCTest
@testable import MonacoCore

/// Decoding tests for the market data the stock screens are built on: the stats
/// grid, the stock-vs-token pair and the denser chart series.
///
/// Most of these are about what the app does with a payload that is missing
/// something. The backend deliberately omits what it could not source, so
/// "half the fields are absent" is a normal response, not an error case.
final class MarketDataDTOTests: XCTestCase {
    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try JSONDecoder().decode(type, from: Data(json.utf8))
    }

    // MARK: - Stats

    func testAssetDetail_decodesTheFullStatsGrid() throws {
        let dto = try decode(AssetDetailDTO.self, """
        {
          "symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
          "priceUsdcMicros":232050000,
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000},
          "marketSession":"open","afterHours":false,
          "stats":{
            "openUsdcMicros":229000000,
            "highUsdcMicros":231800000,
            "lowUsdcMicros":228200000,
            "previousCloseUsdcMicros":226500000,
            "week52HighUsdcMicros":262000000,
            "week52LowUsdcMicros":163000000,
            "spreadBps":12,
            "confUsdcMicros":30000
          }
        }
        """)

        let stats = try XCTUnwrap(dto.stats)
        XCTAssertEqual(stats.openUsdcMicros, 229_000_000)
        XCTAssertEqual(stats.previousCloseUsdcMicros, 226_500_000)
        XCTAssertEqual(stats.week52LowUsdcMicros, 163_000_000)
        XCTAssertEqual(stats.spreadBps, 12)
        XCTAssertEqual(stats.confUsdcMicros, 30_000)
        XCTAssertFalse(stats.isEmpty)
        XCTAssertEqual(dto.marketSession, .open)
        XCTAssertFalse(dto.afterHours)
    }

    func testAssetDetail_partialStatsKeepTheCellsThatExist() throws {
        // A stock listed last month: a session, but no year behind it and no Pyth
        // feed. The cells that cannot be sourced are simply absent.
        let dto = try decode(AssetDetailDTO.self, """
        {
          "symbol":"SPCXc","name":"SpaceX","tokenAddress":"0xb2000000000000000000000000000000000005cc","routable":true,
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000},
          "stats":{"openUsdcMicros":41200000,"previousCloseUsdcMicros":41000000,"spreadBps":48}
        }
        """)

        let stats = try XCTUnwrap(dto.stats)
        XCTAssertEqual(stats.openUsdcMicros, 41_200_000)
        XCTAssertNil(stats.week52HighUsdcMicros)
        XCTAssertNil(stats.confUsdcMicros)
        XCTAssertFalse(stats.isEmpty)
    }

    func testAssetDetail_anEmptyStatsObjectCollapsesToNoGrid() throws {
        let dto = try decode(AssetDetailDTO.self, """
        {
          "symbol":"SPCXc","name":"SpaceX","tokenAddress":"0xb2000000000000000000000000000000000005cc","routable":true,
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000},
          "stats":{}
        }
        """)
        XCTAssertNil(dto.stats, "an object with nothing in it must not become a grid of dashes")
    }

    // MARK: - Stock vs token

    func testStockVsToken_decodesTheKyberLegTheMarkAndTheEquityLine() throws {
        let dto = try decode(AssetDetailDTO.self, """
        {
          "symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
          "priceUsdcMicros":232050000,
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000,"spreadBps":63},
          "stockVsToken":{
            "token":{"source":"dex_kyber","status":"live","priceUsdcMicros":231829069,"bidUsdcMicros":231100000,"askUsdcMicros":232558139},
            "mark":{"source":"chainlink_trv","status":"live","priceUsdcMicros":232050000},
            "equity":{"source":"pyth_equity","status":"live","priceUsdcMicros":231400000,"confUsdcMicros":30000,"publishedAt":"2026-09-22T13:59:52Z"},
            "equitySymbol":"AAPL",
            "premiumBps":-10,
            "spreadBps":63,
            "asOf":"2026-09-22T14:00:00Z"
          }
        }
        """)

        let card = try XCTUnwrap(dto.stockVsToken)
        XCTAssertEqual(card.token.source, .dexKyber)
        XCTAssertEqual(card.token.priceUsdcMicros, 231_829_069)
        XCTAssertEqual(card.token.bidUsdcMicros, 231_100_000)
        XCTAssertEqual(card.token.askUsdcMicros, 232_558_139)
        XCTAssertNil(card.token.publishedAt, "a Kyber quote does not say when it was struck")
        XCTAssertNil(card.token.confUsdcMicros, "Kyber publishes no confidence interval")
        XCTAssertEqual(card.mark.source, .chainlinkTRV)
        XCTAssertEqual(card.mark.priceUsdcMicros, dto.priceUsdcMicros, "the mark is the hero price")
        XCTAssertEqual(card.equity.source, .pythEquity)
        XCTAssertEqual(card.equitySymbol, "AAPL")
        XCTAssertEqual(card.premiumBps, -10)
        XCTAssertEqual(card.spreadBps, 63)
        XCTAssertTrue(card.token.isPriced)
        XCTAssertTrue(card.equity.isPriced)
        XCTAssertEqual(card.asOf?.timeIntervalSince1970, 1_790_085_600)
    }

    func testStockVsToken_staleEquityKeepsItsPriceAndItsPublishTime() throws {
        let card = try decode(StockVsTokenDTO.self, """
        {
          "token":{"source":"dex_kyber","status":"live","priceUsdcMicros":233210000},
          "mark":{"source":"chainlink_trv","status":"live","priceUsdcMicros":232050000},
          "equity":{"source":"pyth_equity","status":"stale","priceUsdcMicros":231400000,"publishedAt":"2026-09-22T20:00:00Z"},
          "premiumBps":50,
          "asOf":"2026-09-22T23:00:00Z"
        }
        """)

        XCTAssertEqual(card.equity.status, .stale)
        XCTAssertTrue(card.equity.isPriced, "a frozen print is still a real price")
        XCTAssertEqual(card.equity.publishedAt?.timeIntervalSince1970, 1_790_107_200)
        XCTAssertEqual(card.premiumBps, 50)
    }

    func testStockVsToken_unavailableLinesCarryAReasonAndNoPrice() throws {
        let card = try decode(StockVsTokenDTO.self, """
        {
          "token":{"source":"dex_kyber","status":"unavailable","reason":"no_route"},
          "mark":{"source":"chainlink_trv","status":"live","priceUsdcMicros":232050000},
          "equity":{"source":"pyth_equity","status":"unavailable","reason":"not_configured"},
          "asOf":"2026-09-22T14:00:00Z"
        }
        """)

        XCTAssertEqual(card.token.unavailableReason, .noRoute)
        XCTAssertEqual(card.equity.unavailableReason, .notConfigured)
        XCTAssertNil(card.token.priceUsdcMicros)
        XCTAssertFalse(card.token.isPriced)
        XCTAssertNil(card.premiumBps, "a premium against a missing leg would be invented")
        XCTAssertNil(card.spreadBps)
    }

    func testStockVsToken_retiredSolanaSourcesDecodeAsUnknown() throws {
        // pyth_crypto (a Solana xStock feed) and jupiter are gone. A stale backend
        // sending them must not be labelled as anything on Base.
        let card = try decode(StockVsTokenDTO.self, """
        {
          "token":{"source":"pyth_crypto","status":"live","priceUsdcMicros":179050000},
          "mark":{"source":"jupiter","status":"live","priceUsdcMicros":179050000},
          "equity":{"source":"pyth_equity","status":"live","priceUsdcMicros":178200000},
          "asOf":"2026-09-22T14:00:00Z"
        }
        """)
        XCTAssertEqual(card.token.source, .unknown)
        XCTAssertEqual(card.mark.source, .unknown)
    }

    func testStockVsToken_unknownSourceAndStatusDoNotFailDecoding() throws {
        let card = try decode(StockVsTokenDTO.self, """
        {
          "token":{"source":"dex_kyber","status":"degraded","priceUsdcMicros":179050000},
          "mark":{"source":"chainlink_trv","status":"live","priceUsdcMicros":179050000},
          "equity":{"source":"something_new","status":"live","priceUsdcMicros":178200000},
          "asOf":"2026-09-22T14:00:00Z"
        }
        """)

        XCTAssertEqual(card.equity.source, .unknown)
        XCTAssertEqual(card.token.status, .unknown)
    }

    func testStockVsToken_unknownReasonStringSurvivesAsRawText() throws {
        let quote = try decode(ReferenceQuoteDTO.self, """
        {"source":"pyth_equity","status":"unavailable","reason":"feed_retired"}
        """)
        XCTAssertNil(quote.unavailableReason)
        XCTAssertEqual(quote.reason, "feed_retired")
    }

    func testStockVsToken_missingALineThrows() {
        // The card is a comparison; a card without its mark is not a partial card, it
        // is a malformed response the screen should treat as no card.
        XCTAssertThrowsError(try decode(StockVsTokenDTO.self, """
        {"token":{"source":"dex_kyber","status":"live","priceUsdcMicros":178200000},
         "equity":{"source":"pyth_equity","status":"live","priceUsdcMicros":178200000},
         "asOf":"2026-09-22T14:00:00Z"}
        """))
    }

    // MARK: - Charts

    func testAssetChart_decodesCandlesPreviousCloseAndSource() throws {
        let dto = try decode(AssetChartDTO.self, """
        {
          "points":[
            {"timestamp":1790410000,"priceUsdcMicros":229400000,"openUsdcMicros":229000000,"highUsdcMicros":229500000,"lowUsdcMicros":228200000},
            {"timestamp":1790410300,"priceUsdcMicros":231400000,"openUsdcMicros":231000000,"highUsdcMicros":231800000,"lowUsdcMicros":230400000}
          ],
          "previousCloseUsdcMicros":226500000,
          "range":"1D",
          "source":"benchmarks",
          "market":{"session":"open","isOpen":true,"afterHours":false,"asOf":"2026-09-22T14:00:00Z"}
        }
        """)

        XCTAssertEqual(dto.points.count, 2)
        XCTAssertEqual(dto.previousCloseUsdcMicros, 226_500_000)
        XCTAssertEqual(dto.range, .oneDay)
        XCTAssertEqual(dto.source, .benchmarks)
        XCTAssertEqual(dto.market?.session, .open)
        XCTAssertTrue(dto.points[0].hasCandle)
        XCTAssertEqual(dto.points[1].highUsdcMicros, 231_800_000)
    }

    func testAssetChart_closeOnlyFallbackHasNoCandles() throws {
        let dto = try decode(AssetChartDTO.self, """
        {"points":[{"timestamp":1790410000,"priceUsdcMicros":229400000}],"range":"1M","source":"hermes"}
        """)

        XCTAssertEqual(dto.source, .hermes)
        XCTAssertFalse(dto.points[0].hasCandle)
        XCTAssertEqual(dto.points[0].openUsdcMicros, 0)
        XCTAssertNil(dto.previousCloseUsdcMicros)
    }

    func testAssetChart_everyRangeRoundTrips() throws {
        for range in AssetChartRange.allCases {
            let dto = try decode(AssetChartDTO.self, """
            {"points":[],"emptyReason":"price history unavailable","range":"\(range.rawValue)"}
            """)
            XCTAssertEqual(dto.range, range)
        }
        XCTAssertEqual(AssetChartRange.allCases.map(\.rawValue), ["1D", "1W", "1M", "3M", "1Y", "ALL"])
        XCTAssertTrue(AssetChartRange.oneDay.showsPreviousCloseBaseline)
        XCTAssertFalse(AssetChartRange.oneYear.showsPreviousCloseBaseline)
    }

    func testAssetChart_missingPointsArrayDecodesToEmpty() throws {
        // A response without the key at all should read as "no history", not throw.
        let dto = try decode(AssetChartDTO.self, """
        {"emptyReason":"price history unavailable","range":"ALL"}
        """)
        XCTAssertTrue(dto.points.isEmpty)
        XCTAssertEqual(dto.emptyReason, "price history unavailable")
    }

    func testAssetChart_unknownRangeNameDecodesToNilNotAThrow() throws {
        let dto = try decode(AssetChartDTO.self, """
        {"points":[],"range":"5Y"}
        """)
        XCTAssertNil(dto.range)
    }

    func testAssetChart_pointMissingItsPriceThrows() {
        // A point with a timestamp and no price cannot be plotted; silently
        // dropping it would move the line without saying so.
        XCTAssertThrowsError(try decode(AssetChartDTO.self, """
        {"points":[{"timestamp":1790410000}]}
        """))
    }

    // MARK: - Envelopes

    func testListAndPopular_carryTheMarketStatus() throws {
        let listed = try decode(ListMarketAssetsResponseDTO.self, """
        {
          "assets":[{"symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,"priceUsdcMicros":232050000}],
          "hasMore":false,
          "market":{"session":"after_hours","isOpen":false,"afterHours":true,"nextSession":"closed","asOf":"2026-09-22T21:00:00Z"}
        }
        """)
        XCTAssertEqual(listed.market?.session, .afterHours)
        XCTAssertTrue(listed.market?.afterHours ?? false)

        let popular = try decode(PopularAssetsResponseDTO.self, """
        {"assets":[],"market":{"session":"open","isOpen":true,"afterHours":false,"asOf":"2026-09-22T14:00:00Z"}}
        """)
        XCTAssertEqual(popular.market?.session, .open)
    }

    // MARK: - Backward compatibility

    func testAssetDetail_decodesAResponseFromABackendWithoutAnyOfTheNewFields() throws {
        let dto = try decode(AssetDetailDTO.self, """
        {
          "symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
          "priceUsdcMicros":185000000,"change24h":"0.027027",
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000,"spreadBps":12}
        }
        """)

        XCTAssertEqual(dto.priceUsdcMicros, 185_000_000)
        XCTAssertNil(dto.marketSession)
        XCTAssertFalse(dto.afterHours)
        XCTAssertNil(dto.market)
        XCTAssertNil(dto.stats)
        XCTAssertNil(dto.stockVsToken)
    }

    func testAssetDetail_marketSessionFallsBackToTheEnvelope() throws {
        // If a backend ships the envelope but not the mirrored fields, the header
        // still knows what session it is.
        let dto = try decode(AssetDetailDTO.self, """
        {
          "symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000},
          "market":{"session":"after_hours","isOpen":false,"afterHours":true,"asOf":"2026-09-22T21:00:00Z"}
        }
        """)
        XCTAssertEqual(dto.marketSession, .afterHours)
        XCTAssertTrue(dto.afterHours)
    }

    func testListResponse_withoutMarketStatusStillDecodes() throws {
        let dto = try decode(ListMarketAssetsResponseDTO.self, """
        {"assets":[],"hasMore":true}
        """)
        XCTAssertNil(dto.market)
        XCTAssertTrue(dto.hasMore)
    }

    // MARK: - Sample data

    func testSampleData_coversEveryStateTheHarnessNeedsToScreenshot() throws {
        let sessions: [MarketStatusDTO] = [
            MarketSampleData.sessionOpen,
            MarketSampleData.sessionPreMarket,
            MarketSampleData.sessionAfterHours,
            MarketSampleData.sessionClosedOvernight,
            MarketSampleData.sessionHoliday,
            MarketSampleData.sessionEarlyClose,
        ]
        XCTAssertEqual(Set(sessions.map(\.session)).count, 4, "every session name should be represented")
        XCTAssertEqual(MarketSampleData.sessionHoliday.holiday, "Thanksgiving Day")
        XCTAssertTrue(MarketSampleData.sessionEarlyClose.earlyClose)

        XCTAssertFalse(MarketSampleData.statsComplete.isEmpty)
        XCTAssertNil(MarketSampleData.statsPartial.week52HighUsdcMicros)

        XCTAssertEqual(MarketSampleData.stockVsTokenAfterHours.equity.status, .stale)
        XCTAssertEqual(MarketSampleData.stockVsTokenLive.token.source, .dexKyber)
        XCTAssertEqual(MarketSampleData.stockVsTokenLive.mark.source, .chainlinkTRV)
        XCTAssertEqual(MarketSampleData.stockVsTokenNoRoute.token.unavailableReason, .noRoute)
        XCTAssertNil(MarketSampleData.stockVsTokenNoRoute.premiumBps)
        // The premium is token against mark, so it survives a missing equity line.
        XCTAssertEqual(MarketSampleData.stockVsTokenEquityUnavailable.equity.unavailableReason, .notEntitled)
        XCTAssertNotNil(MarketSampleData.stockVsTokenEquityUnavailable.premiumBps)
        XCTAssertEqual(MarketSampleData.detail().priceUsdcMicros, MarketSampleData.stockVsTokenLive.mark.priceUsdcMicros)

        for range in AssetChartRange.allCases {
            let dense = MarketSampleData.chart(range: range)
            XCTAssertEqual(dense.range, range)
            XCTAssertEqual(dense.points.count, 78)
            XCTAssertTrue(dense.points.allSatisfy(\.hasCandle))
            // Strictly increasing timestamps, or Swift Charts draws a loop.
            XCTAssertTrue(zip(dense.points, dense.points.dropFirst()).allSatisfy { $0.timestamp < $1.timestamp })
            XCTAssertTrue(dense.points.allSatisfy { $0.priceUsdcMicros > 0 })

            XCTAssertFalse(MarketSampleData.chartFromFallback(range: range).points.contains(where: \.hasCandle))
            XCTAssertTrue(MarketSampleData.chartEmpty(range: range).points.isEmpty)
        }

        XCTAssertNil(MarketSampleData.sparseDetail().stockVsToken)
        XCTAssertEqual(MarketSampleData.detail().marketSession, .open)
    }

    func testSampleData_survivesARoundTripThroughJSON() throws {
        let encoded = try JSONEncoder().encode(MarketSampleData.detail())
        let decoded = try JSONDecoder().decode(AssetDetailDTO.self, from: encoded)
        XCTAssertEqual(decoded, MarketSampleData.detail())
    }

    // MARK: - Price basis

    func testAssetStats_carryTheInstrumentTheirCandlesCameFrom() throws {
        // The grid is the underlying equity per share while the hero price is the
        // token's total-return mark per token, which carries the multiplier. The
        // token price can sit above "the day's high" — which reads as a bug unless
        // the grid says which instrument it is about.
        let dto = try decode(AssetDetailDTO.self, """
        {
          "symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
          "priceUsdcMicros":232050000,
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000},
          "stats":{"highUsdcMicros":231800000,"basis":"underlying","basisSymbol":"AAPL"}
        }
        """)

        let stats = try XCTUnwrap(dto.stats)
        XCTAssertEqual(stats.basis, .underlying)
        XCTAssertEqual(stats.basisCaption, "AAPL on its home exchange")
        // The hero price really is above the grid's high. That is the two
        // instruments disagreeing, which is the thing the caption explains.
        XCTAssertGreaterThan(try XCTUnwrap(dto.priceUsdcMicros), try XCTUnwrap(stats.highUsdcMicros))
    }

    func testAssetStats_unlabelledGridIsDrawnUnheadedRatherThanUnderAGuess() throws {
        // A backend that has not shipped the label yet, or a source that could not
        // say. Guessing "AAPL" here would be inventing the one fact the caption is
        // supposed to carry.
        let stats = try decode(AssetStatsDTO.self, #"{"highUsdcMicros":231800000}"#)
        XCTAssertNil(stats.basis)
        XCTAssertNil(stats.basisCaption)

        let futureBasis = try decode(AssetStatsDTO.self, #"{"highUsdcMicros":1,"basis":"something_new","basisSymbol":"AAPL"}"#)
        XCTAssertEqual(futureBasis.basis, .unknown, "a basis added server-side must not take the screen down")
        XCTAssertNil(futureBasis.basisCaption)
    }

    func testAssetStats_aBasisLabelAloneIsNotAGrid() throws {
        let stats = try decode(AssetStatsDTO.self, #"{"basis":"underlying","basisSymbol":"AAPL"}"#)
        XCTAssertTrue(stats.isEmpty, "a grid with a label and no figures must not be drawn")
    }

    func testAssetChart_carriesTheInstrumentTheCurveIs() throws {
        let chart = try decode(AssetChartDTO.self, """
        {"points":[{"timestamp":1,"priceUsdcMicros":2}],"range":"1D","source":"benchmarks",
         "basis":"underlying","basisSymbol":"AAPL"}
        """)
        XCTAssertEqual(chart.basis, .underlying)
        XCTAssertEqual(chart.basisSymbol, "AAPL")
        XCTAssertEqual(chart.basisCaption, "AAPL on its home exchange")
    }

    func testAssetChart_chainlinkFallbackIsTheTokenOnBase() throws {
        let chart = try decode(AssetChartDTO.self, """
        {"points":[{"timestamp":1,"priceUsdcMicros":2},{"timestamp":2,"priceUsdcMicros":3}],
         "range":"1W","source":"chainlink","basis":"token","basisSymbol":"AAPLc"}
        """)
        XCTAssertEqual(chart.source, .chainlink)
        XCTAssertEqual(chart.basis, .token)
        XCTAssertNil(chart.previousCloseUsdcMicros)
        XCTAssertEqual(chart.basisCaption, "AAPL token on Base")

        let sample = MarketSampleData.chartFromChainlink(range: .oneWeek)
        XCTAssertEqual(sample.source, .chainlink)
        XCTAssertEqual(sample.basis, .token)
        XCTAssertFalse(sample.points.contains(where: \.hasCandle))
    }

    func testSampleData_premiumIsTheKyberMidAgainstTheMark() throws {
        // The sample card must obey the rule the backend does: token against mark,
        // never against the per-share equity price.
        for card in [MarketSampleData.stockVsTokenLive, MarketSampleData.stockVsTokenAfterHours] {
            let token = Double(try XCTUnwrap(card.token.priceUsdcMicros))
            let mark = Double(try XCTUnwrap(card.mark.priceUsdcMicros))
            XCTAssertEqual(card.premiumBps, Int(((token - mark) / mark * 10_000).rounded()))
            let ask = Double(try XCTUnwrap(card.token.askUsdcMicros))
            let bid = Double(try XCTUnwrap(card.token.bidUsdcMicros))
            XCTAssertEqual(card.spreadBps, Int(((ask - bid) / token * 10_000).rounded()))
        }
    }

    func testAssetChart_sampledFallbackHasNoPreviousCloseBaseline() throws {
        // The sampler's grid starts inside the window, so its first point is drawn
        // in the series itself. A baseline sitting exactly on the curve's first
        // point is not a baseline, and the day change would read 0% at t0.
        let fallback = MarketSampleData.chartFromFallback(range: .oneDay)
        XCTAssertEqual(fallback.source, .hermes)
        XCTAssertNil(fallback.previousCloseUsdcMicros)

        let dense = MarketSampleData.chart(range: .oneDay)
        XCTAssertEqual(dense.source, .benchmarks)
        XCTAssertNotNil(dense.previousCloseUsdcMicros)
        XCTAssertFalse(
            dense.points.contains { $0.priceUsdcMicros == dense.previousCloseUsdcMicros },
            "a genuine previous close comes from before the window, so it is not one of its points"
        )
    }
}
