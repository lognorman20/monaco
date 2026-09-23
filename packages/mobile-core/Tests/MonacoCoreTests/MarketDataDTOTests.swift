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
            "confUsdcMicros":30000
          }
        }
        """)

        let stats = try XCTUnwrap(dto.stats)
        XCTAssertEqual(stats.openUsdcMicros, 229_000_000)
        XCTAssertEqual(stats.previousCloseUsdcMicros, 226_500_000)
        XCTAssertEqual(stats.week52LowUsdcMicros, 163_000_000)
        XCTAssertEqual(stats.confUsdcMicros, 30_000)
        XCTAssertFalse(stats.isEmpty)
        XCTAssertEqual(dto.marketSession, .open)
        XCTAssertFalse(dto.afterHours)
    }

    func testAssetDetail_partialStatsKeepTheCellsThatExist() throws {
        // A stock listed last month: a session, but no year behind it and no equity
        // quote. The cells that cannot be sourced are simply absent.
        let dto = try decode(AssetDetailDTO.self, """
        {
          "symbol":"SPCXc","name":"SpaceX","tokenAddress":"0xb2000000000000000000007b9fcbd005511acbd5","routable":true,
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000},
          "stats":{"openUsdcMicros":41200000,"previousCloseUsdcMicros":41000000}
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
          "symbol":"SPCXc","name":"SpaceX","tokenAddress":"0xb2000000000000000000007b9fcbd005511acbd5","routable":true,
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000},
          "stats":{}
        }
        """)
        XCTAssertNil(dto.stats, "an object with nothing in it must not become a grid of dashes")
    }

    func testAssetStats_aSpreadFromAnOlderBackendIsNotAGridCell() throws {
        // The spread is the token's round-trip cost, not a figure about the share
        // the grid is headed with. A backend that still sends it there is ignored,
        // and a grid holding only that is no grid.
        let dto = try decode(AssetDetailDTO.self, """
        {
          "symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000,"spreadBps":63},
          "stats":{"spreadBps":63,"basis":"underlying","basisSymbol":"AAPL"}
        }
        """)
        XCTAssertNil(dto.stats)
        XCTAssertEqual(dto.liquidity.spreadBps, 63)
    }

    // MARK: - Day move

    func testDayMove_isLabelledAsTheStocksWhenTheBackendSaysSo() throws {
        let dto = try decode(AssetDetailDTO.self, """
        {
          "symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
          "priceUsdcMicros":232050000,"change24h":"0.015000","change24hBasis":"underlying","change24hBasisSymbol":"AAPL",
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000}
        }
        """)
        let move = try XCTUnwrap(dto.stockDayMove)
        XCTAssertEqual(move.ratio, "0.015000")
        XCTAssertEqual(move.symbol, "AAPL")
        XCTAssertEqual(move.caption, "AAPL day move")

        let row = try decode(MarketAssetDTO.self, """
        {"symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
         "priceUsdcMicros":232050000,"change24h":"0.015000","change24hBasis":"underlying","change24hBasisSymbol":"AAPL"}
        """)
        XCTAssertEqual(row.stockDayMove, move)
    }

    func testDayMove_withoutABasisIsNotShown() throws {
        // A backend that sends the ratio without saying whose move it is: beside a
        // per-token price it would read as the token's move, so there is no label
        // to put on it and it is not rendered.
        let unlabelled = try decode(MarketAssetDTO.self, """
        {"symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
         "change24h":"0.015000"}
        """)
        XCTAssertEqual(unlabelled.change24h, "0.015000")
        XCTAssertNil(unlabelled.stockDayMove)

        // Nor a basis this build has never heard of. The decoder maps an
        // unrecognised string to .unknown on purpose, and an unknown instrument is
        // as unlabelled as none at all.
        let strange = try decode(MarketAssetDTO.self, """
        {"symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
         "change24h":"0.015000","change24hBasis":"moonbeam","change24hBasisSymbol":"AAPL"}
        """)
        XCTAssertNil(strange.stockDayMove)
    }

    /// This assertion used to be `XCTAssertNil`: only the underlying's move counted
    /// as a day move at all. Pyth Benchmarks' history endpoint 404s and our key is
    /// crypto-only, so for an equity it answers nothing and the backend now falls
    /// through to the token's own Chainlink feed. Refusing that move leaves every
    /// pill on the tab blank; it is a real move of the thing the member holds, in
    /// the same unit as the price beside it. So it is shown, and the caption says
    /// which instrument it is, because the display ticker is "AAPL" either way.
    func testDayMove_aTokenBasisIsTheTokensOwnMoveAndSaysSo() throws {
        let tokenBasis = try decode(MarketAssetDTO.self, """
        {"symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
         "change24h":"0.015000","change24hBasis":"token","change24hBasisSymbol":"AAPLc"}
        """)
        let move = try XCTUnwrap(tokenBasis.stockDayMove)
        XCTAssertEqual(move.basis, .token)
        XCTAssertEqual(move.ratio, "0.015000")
        XCTAssertEqual(move.caption, "AAPL token day move")
    }

    /// The footnote is the only place a sighted reader is told whose move the pill
    /// is, so it is read off the rows rather than fixed.
    func testFiguresFootnote_namesWhicheverInstrumentTheRowsCarry() {
        let equity = StockDayMove(ratio: "0.01", basis: .underlying, basisSymbol: "AAPL")
        let token = StockDayMove(ratio: "0.01", basis: .token, basisSymbol: "AAPLc")

        XCTAssertEqual(
            MarketFiguresFootnote.text(for: [equity, equity]),
            "Prices are per token on Base. The day move is the stock's own, on its exchange."
        )
        XCTAssertEqual(
            MarketFiguresFootnote.text(for: [token, token]),
            "Prices are per token on Base. The day move is the token's own, on Base."
        )
        XCTAssertEqual(
            MarketFiguresFootnote.text(for: [equity, token]),
            "Prices are per token on Base. Each day move is the stock's own where its exchange can be read, and the token's otherwise."
        )
        // Nothing to say about a move nobody shipped.
        XCTAssertEqual(MarketFiguresFootnote.text(for: [nil, nil]), "Prices are per token on Base.")
    }

    /// The dollar face is measured on the line's last close, and only when the line
    /// and the move are the same instrument. That used to be spelled "both are the
    /// underlying's", which was the only case that could arise then.
    func testDayChangeDollars_needTheLineAndTheMoveToBeOneInstrument() throws {
        let closes: [Int64] = [230_000_000, 232_050_000]
        let token = try XCTUnwrap(StockDayMove(ratio: "0.01", basis: .token, basisSymbol: "AAPLc"))
        let equity = try XCTUnwrap(StockDayMove(ratio: "0.01", basis: .underlying, basisSymbol: "AAPL"))

        XCTAssertEqual(
            DayChangeFigures.referencePrice(sparkUsdcMicros: closes, sparkBasis: .token, dayMove: token),
            232_050_000,
            "a token move on a token line is measured in the token's own unit"
        )
        XCTAssertNil(
            DayChangeFigures.referencePrice(sparkUsdcMicros: closes, sparkBasis: .token, dayMove: equity),
            "the share's move must never be priced at the token's price"
        )
        XCTAssertNil(
            DayChangeFigures.referencePrice(sparkUsdcMicros: closes, sparkBasis: nil, dayMove: token),
            "an unlabelled line cannot be shown to be the move's own unit"
        )
    }

    // MARK: - Stock vs token

    func testStockVsToken_decodesTheKyberLegTheMarkAndTheEquityLine() throws {
        let dto = try decode(AssetDetailDTO.self, """
        {
          "symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
          "priceUsdcMicros":232050000,
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000,"spreadBps":63},
          "stockVsToken":{
            "token":{"source":"dex_kyber","status":"live","priceUsdcMicros":231829069,"bidUsdcMicros":231100000,"askUsdcMicros":232558139,"probedAt":"2026-09-22T13:59:30Z"},
            "mark":{"source":"chainlink_trv","status":"live","priceUsdcMicros":232050000,"publishedAt":"2026-09-22T13:58:00Z"},
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
        XCTAssertEqual(card.token.probedAt?.timeIntervalSince1970, 1_790_085_570, "when we took the probes, which can trail asOf")
        XCTAssertEqual(card.mark.source, .chainlinkTRV)
        XCTAssertEqual(card.mark.publishedAt?.timeIntervalSince1970, 1_790_085_480, "the Chainlink round's updatedAt")
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
        XCTAssertFalse(card.token.isPriced, "a status this build does not know is not assumed to be a usable price")
    }

    func testStockVsToken_aWeekendMarkIsStaleWithItsRoundTimeAndNoPremium() throws {
        let card = try decode(StockVsTokenDTO.self, """
        {
          "token":{"source":"dex_kyber","status":"live","priceUsdcMicros":234100000,"bidUsdcMicros":233400000,"askUsdcMicros":234800000},
          "mark":{"source":"chainlink_trv","status":"stale","priceUsdcMicros":232050000,"publishedAt":"2026-09-25T23:59:00Z"},
          "equity":{"source":"pyth_equity","status":"stale","priceUsdcMicros":231400000,"confUsdcMicros":30000,"publishedAt":"2026-09-25T20:00:00Z"},
          "equitySymbol":"AAPL",
          "spreadBps":60,
          "asOf":"2026-09-26T16:00:00Z"
        }
        """)
        XCTAssertEqual(card.mark.status, .stale)
        XCTAssertTrue(card.mark.isPriced, "a held close is still a price to show, with its time")
        XCTAssertEqual(card.mark.publishedAt?.timeIntervalSince1970, 1_790_380_740)
        XCTAssertNil(card.premiumBps)
        // The harness's weekend card is the same payload.
        XCTAssertEqual(card.mark, MarketSampleData.stockVsTokenWeekend.mark)
        XCTAssertEqual(card.equity, MarketSampleData.stockVsTokenWeekend.equity)
        XCTAssertNil(MarketSampleData.stockVsTokenWeekend.premiumBps)
    }

    func testAssetDetail_aMalformedCardOrGridDoesNotTakeTheHeroDown() throws {
        // The hero price and the liquidity strip decoded fine. A card missing its
        // mark, or a grid with a string where a number goes, is dropped on its own.
        let dto = try decode(AssetDetailDTO.self, """
        {
          "symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
          "priceUsdcMicros":232050000,
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000},
          "stats":{"openUsdcMicros":"not a number"},
          "stockVsToken":{"token":{"source":"dex_kyber","status":"live","priceUsdcMicros":1},"asOf":"2026-09-22T14:00:00Z"}
        }
        """)
        XCTAssertEqual(dto.priceUsdcMicros, 232_050_000)
        XCTAssertNil(dto.stats)
        XCTAssertNil(dto.stockVsToken)
    }

    func testAssetDetail_aMalformedSessionChipDoesNotTakeTheHeroDown() throws {
        // The chip carries two timestamps and they throw on anything the shared
        // ISO8601 parser rejects. It is the same optional section as the grid and
        // the card, so it goes the same way: dropped on its own.
        let dto = try decode(AssetDetailDTO.self, """
        {
          "symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,
          "priceUsdcMicros":232050000,
          "liquidity":{"label":"Via DEX","routable":true,"buyProbeUsdcMicros":1000000},
          "market":{"session":"open","isOpen":true,"nextTransition":"the bell"}
        }
        """)
        XCTAssertEqual(dto.priceUsdcMicros, 232_050_000)
        XCTAssertNil(dto.market)
        XCTAssertNil(dto.marketSession)
    }

    func testListMarketAssets_aMalformedSessionChipDoesNotTakeTheRowsDown() throws {
        let dto = try decode(ListMarketAssetsResponseDTO.self, """
        {
          "assets":[{"symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,"priceUsdcMicros":232050000}],
          "hasMore":false,
          "market":{"session":"open","isOpen":true,"asOf":"never"}
        }
        """)
        XCTAssertEqual(dto.assets.count, 1)
        XCTAssertNil(dto.market)
    }

    func testPopularAssets_aMalformedSessionChipDoesNotTakeTheRowsDown() throws {
        let dto = try decode(PopularAssetsResponseDTO.self, """
        {
          "assets":[{"symbol":"AAPLc","name":"Apple","tokenAddress":"0xb200000000000000000000c2e324d24d7eecd1fb","routable":true,"priceUsdcMicros":232050000}],
          "market":{"session":"open","isOpen":true,"asOf":"never"}
        }
        """)
        XCTAssertEqual(dto.assets.count, 1)
        XCTAssertNil(dto.market)
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
        // No sell route: the backend sends no card and no spread.
        XCTAssertNil(MarketSampleData.liquidityNoSellRoute.spreadBps)
        XCTAssertNil(MarketSampleData.liquidityNoSellRoute.sellProbeOutAmount)
        XCTAssertEqual(MarketSampleData.stockVsTokenWeekend.mark.status, .stale)
        XCTAssertEqual(MarketSampleData.sessionWeekend.session, .closed)
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

    func testSampleData_theChartStaysInsideTheGridItIsShownWith() throws {
        // The chart and the grid under it describe the same session of the same
        // instrument, so a curve drawn above its own stated day high is a harness
        // screenshot of a contradiction. Both samples are checked against the grid
        // they ship beside.
        func assertWithin(
            _ chart: AssetChartDTO,
            _ stats: AssetStatsDTO,
            file: StaticString = #filePath,
            line: UInt = #line
        ) throws {
            let high = try XCTUnwrap(stats.highUsdcMicros, file: file, line: line)
            let low = try XCTUnwrap(stats.lowUsdcMicros, file: file, line: line)
            XCTAssertFalse(chart.points.isEmpty, file: file, line: line)
            for point in chart.points {
                XCTAssertLessThanOrEqual(point.highUsdcMicros, high, file: file, line: line)
                XCTAssertLessThanOrEqual(point.priceUsdcMicros, high, file: file, line: line)
                XCTAssertGreaterThanOrEqual(point.lowUsdcMicros, low, file: file, line: line)
                XCTAssertGreaterThanOrEqual(point.openUsdcMicros, low, file: file, line: line)
            }
            // And the grid's own extremes are reached, or the cells would be
            // describing a session the curve never had.
            XCTAssertEqual(chart.points.map(\.highUsdcMicros).max(), high, file: file, line: line)
            XCTAssertEqual(chart.points.map(\.lowUsdcMicros).min(), low, file: file, line: line)
        }

        for range in AssetChartRange.allCases {
            try assertWithin(MarketSampleData.chart(range: range), MarketSampleData.statsComplete)
        }
        for range in [AssetChartRange.oneDay, .oneWeek, .oneMonth] {
            try assertWithin(MarketSampleData.chartRecentListing(range: range), MarketSampleData.statsPartial)
        }
        // The baseline the chart draws is the grid's previous close, for both.
        XCTAssertEqual(
            MarketSampleData.chart(range: .oneDay).previousCloseUsdcMicros,
            MarketSampleData.statsComplete.previousCloseUsdcMicros
        )
        XCTAssertEqual(
            MarketSampleData.chartRecentListing(range: .oneDay).previousCloseUsdcMicros,
            MarketSampleData.statsPartial.previousCloseUsdcMicros
        )
    }

    func testSampleData_theSparseListingIsOneTheBackendCouldProduce() throws {
        let sparse = MarketSampleData.sparseDetail()
        // The pinned SPCXc contract (apps/backend/internal/b20/pinned.go).
        XCTAssertEqual(sparse.tokenAddress, "0xb2000000000000000000007b9fcbd005511acbd5")
        // Session cells exist only because a 1D Benchmarks series does, so the 1D
        // chart the harness serves beside them is not empty.
        XCTAssertNotNil(sparse.stats?.openUsdcMicros)
        let day = MarketSampleData.chartRecentListing(range: .oneDay)
        XCTAssertFalse(day.points.isEmpty)
        XCTAssertEqual(day.previousCloseUsdcMicros, sparse.stats?.previousCloseUsdcMicros)
        XCTAssertEqual(day.basisSymbol, "SPCX")
        // No year of candles, so no 52-week range and no long chart.
        XCTAssertNil(sparse.stats?.week52HighUsdcMicros)
        XCTAssertTrue(MarketSampleData.chartRecentListing(range: .oneYear).points.isEmpty)
        // One probe unrouted: no spread, no card.
        XCTAssertNil(sparse.liquidity.spreadBps)
        XCTAssertNil(sparse.stockVsToken)
        XCTAssertEqual(sparse.stockDayMove?.caption, "SPCX day move")
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
        // The rounds do know a previous close: the last round of the session before
        // the window, which is not a point the curve draws.
        XCTAssertNotNil(sample.previousCloseUsdcMicros)
    }

    func testAssetChart_emptyMessageOnlySurfacesAReasonWorthReading() throws {
        // The catch-all repeats what an empty chart already shows.
        let generic = try decode(AssetChartDTO.self, """
        {"points":[],"emptyReason":"price history unavailable","range":"1Y"}
        """)
        XCTAssertNil(generic.emptyMessage)

        // A reason that names the day the feed starts answers the question an
        // empty 1Y chip raises: whether the app is broken.
        let dated = try decode(AssetChartDTO.self, """
        {"points":[],"emptyReason":"Only on-chain since 5 Aug 2026","range":"1Y","source":"chainlink"}
        """)
        XCTAssertEqual(dated.emptyMessage, "Only on-chain since 5 Aug 2026")

        let silent = try decode(AssetChartDTO.self, """
        {"points":[],"range":"1Y"}
        """)
        XCTAssertNil(silent.emptyMessage)

        XCTAssertEqual(
            MarketSampleData.chartBeforeTheFeedExisted(range: .oneYear).emptyMessage,
            "Only on-chain since 5 Aug 2026"
        )
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
