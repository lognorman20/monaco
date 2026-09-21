import Foundation

/// Canned market payloads for the sample harness and for previews.
///
/// Every state the market fields can be in is named here, so the screens built on
/// them can be driven — and screenshotted — with no backend and no network. The
/// harness flags in the app pick a scenario by name; this is the data behind them.
///
/// The awkward states are the point. A stats grid with half its cells missing, an
/// equity feed our Pyth key is not entitled to, a pool with no sell route, a mark
/// holding Friday's close: the backend really produces these, so the UI has to be
/// built against them rather than against a happy path that always has every number.
///
/// Units, as on the wire: the hero price and `mark` are the AAPLc token's Chainlink
/// total-return mark, per token; the chart, the grid and `equity` are Apple per share
/// from Pyth; `token` is the Kyber quote-implied mid, per token. The per-token figures
/// sit a little above the per-share ones because the token's multiplier has grown with
/// reinvested dividends.
public enum MarketSampleData {
    /// 2026-09-22 14:00 UTC — 10:00 ET on an ordinary Tuesday.
    public static let tradingTuesday = Date(timeIntervalSince1970: 1_790_085_600)

    /// Saturday 2026-09-26 16:00 UTC — noon ET, the exchange and the 24/5 feeds shut.
    public static let saturdayNoon = Date(timeIntervalSince1970: 1_790_438_400)

    /// The pinned AAPLc token contract on Base.
    public static let appleTokenAddress = "0xb200000000000000000000c2e324d24d7eecd1fb"

    /// The pinned SPCXc token contract on Base.
    public static let spaceXTokenAddress = "0xb2000000000000000000007b9fcbd005511acbd5"

    // MARK: - Market status

    public static let sessionOpen = MarketStatusDTO(
        session: .open,
        isOpen: true,
        afterHours: false,
        nextSession: .afterHours,
        nextTransition: tradingTuesday.addingTimeInterval(6 * 3600),
        asOf: tradingTuesday
    )

    public static let sessionPreMarket = MarketStatusDTO(
        session: .preMarket,
        isOpen: false,
        afterHours: true,
        nextSession: .open,
        nextTransition: tradingTuesday.addingTimeInterval(-30 * 60),
        asOf: tradingTuesday.addingTimeInterval(-2 * 3600)
    )

    public static let sessionAfterHours = MarketStatusDTO(
        session: .afterHours,
        isOpen: false,
        afterHours: true,
        nextSession: .closed,
        nextTransition: tradingTuesday.addingTimeInterval(10 * 3600),
        asOf: tradingTuesday.addingTimeInterval(7 * 3600)
    )

    public static let sessionClosedOvernight = MarketStatusDTO(
        session: .closed,
        isOpen: false,
        afterHours: true,
        nextSession: .preMarket,
        nextTransition: tradingTuesday.addingTimeInterval(22 * 3600),
        asOf: tradingTuesday.addingTimeInterval(12 * 3600)
    )

    /// Saturday: closed until Monday's pre-market.
    public static let sessionWeekend = MarketStatusDTO(
        session: .closed,
        isOpen: false,
        afterHours: true,
        nextSession: .preMarket,
        // Monday 2026-09-28 04:00 ET.
        nextTransition: Date(timeIntervalSince1970: 1_790_582_400),
        asOf: saturdayNoon
    )

    /// Thanksgiving: closed all day, and the next session is a half day.
    public static let sessionHoliday = MarketStatusDTO(
        session: .closed,
        isOpen: false,
        afterHours: true,
        nextSession: .preMarket,
        // Thanksgiving noon ET; pre-market opens the next morning.
        nextTransition: Date(timeIntervalSince1970: 1_795_770_000),
        asOf: Date(timeIntervalSince1970: 1_795_708_800),
        holiday: "Thanksgiving Day"
    )

    /// The Friday after Thanksgiving: open, but the bell is at 13:00 ET.
    public static let sessionEarlyClose = MarketStatusDTO(
        session: .open,
        isOpen: true,
        afterHours: false,
        nextSession: .afterHours,
        // The half-day bell is 13:00 ET, which is 18:00 UTC in November.
        nextTransition: Date(timeIntervalSince1970: 1_795_802_400),
        asOf: Date(timeIntervalSince1970: 1_795_780_800),
        earlyClose: true
    )

    // MARK: - Stats

    /// Every cell sourced: the grid at its best.
    public static let statsComplete = AssetStatsDTO(
        openUsdcMicros: 229_000_000,
        highUsdcMicros: 231_800_000,
        lowUsdcMicros: 228_200_000,
        previousCloseUsdcMicros: 226_500_000,
        week52HighUsdcMicros: 262_000_000,
        week52LowUsdcMicros: 163_000_000,
        confUsdcMicros: 30_000,
        basis: .underlying,
        basisSymbol: "AAPL"
    )

    /// A recent listing. Benchmarks (keyless) has today's session candles but not a
    /// year of them, so there is no 52-week range; and there is no equity quote, so
    /// no confidence interval. A session, nothing more.
    public static let statsPartial = AssetStatsDTO(
        openUsdcMicros: 41_200_000,
        highUsdcMicros: 42_050_000,
        lowUsdcMicros: 40_900_000,
        previousCloseUsdcMicros: 41_000_000,
        basis: .underlying,
        basisSymbol: "SPCX"
    )

    // MARK: - Stock vs token

    /// The AAPLc token's Chainlink total-return mark, per token, struck two minutes
    /// before the sample clock.
    public static let appleMark = ReferenceQuoteDTO(
        source: .chainlinkTRV,
        status: .live,
        priceUsdcMicros: 232_050_000,
        publishedAt: tradingTuesday.addingTimeInterval(-120)
    )

    /// Apple on its exchange, per share, from Pyth.
    public static let appleEquityLive = ReferenceQuoteDTO(
        source: .pythEquity,
        status: .live,
        priceUsdcMicros: 231_400_000,
        confUsdcMicros: 30_000,
        publishedAt: tradingTuesday.addingTimeInterval(-8)
    )

    /// During the session: the pools trade 10 bps under the mark, with a 63 bps
    /// round trip on a 1 USDC probe.
    public static let stockVsTokenLive = StockVsTokenDTO(
        token: ReferenceQuoteDTO(
            source: .dexKyber,
            status: .live,
            priceUsdcMicros: 231_829_069,
            bidUsdcMicros: 231_100_000,
            askUsdcMicros: 232_558_139,
            probedAt: tradingTuesday
        ),
        mark: appleMark,
        equity: appleEquityLive,
        equitySymbol: "AAPL",
        premiumBps: -10,
        spreadBps: 63,
        asOf: tradingTuesday
    )

    /// After the bell: the Pyth equity print is frozen at 16:00 ET, while the 24/5
    /// total-return feed keeps moving through the post-market and so do the pools.
    /// Both per-token legs are live, so the premium stands.
    public static let stockVsTokenAfterHours = StockVsTokenDTO(
        token: ReferenceQuoteDTO(
            source: .dexKyber,
            status: .live,
            priceUsdcMicros: 233_210_000,
            bidUsdcMicros: 232_520_000,
            askUsdcMicros: 233_900_000,
            probedAt: tradingTuesday.addingTimeInterval(9 * 3600 - 30)
        ),
        mark: ReferenceQuoteDTO(
            source: .chainlinkTRV,
            status: .live,
            priceUsdcMicros: 232_050_000,
            publishedAt: tradingTuesday.addingTimeInterval(9 * 3600 - 240)
        ),
        equity: ReferenceQuoteDTO(
            source: .pythEquity,
            status: .stale,
            priceUsdcMicros: 231_400_000,
            confUsdcMicros: 30_000,
            publishedAt: tradingTuesday.addingTimeInterval(6 * 3600)
        ),
        equitySymbol: "AAPL",
        premiumBps: 50,
        spreadBps: 59,
        asOf: tradingTuesday.addingTimeInterval(9 * 3600)
    )

    /// Saturday: the pools trade while the total-return feed holds Friday's close
    /// (its last round at 19:59 ET). The mark is stale with its round time, and
    /// there is no premium: the gap is the market's move since Friday, not a premium.
    public static let stockVsTokenWeekend = StockVsTokenDTO(
        token: ReferenceQuoteDTO(
            source: .dexKyber,
            status: .live,
            priceUsdcMicros: 234_100_000,
            bidUsdcMicros: 233_400_000,
            askUsdcMicros: 234_800_000,
            probedAt: saturdayNoon.addingTimeInterval(-20)
        ),
        mark: ReferenceQuoteDTO(
            source: .chainlinkTRV,
            status: .stale,
            priceUsdcMicros: 232_050_000,
            // Friday 2026-09-25 19:59 ET.
            publishedAt: Date(timeIntervalSince1970: 1_790_380_740)
        ),
        equity: ReferenceQuoteDTO(
            source: .pythEquity,
            status: .stale,
            priceUsdcMicros: 231_400_000,
            confUsdcMicros: 30_000,
            // Friday's 16:00 ET print.
            publishedAt: Date(timeIntervalSince1970: 1_790_366_400)
        ),
        equitySymbol: "AAPL",
        spreadBps: 60,
        asOf: saturdayNoon
    )

    /// Our Pyth key is not entitled to the equity feed. The comparison that matters
    /// (token against mark) still stands; the reference line says what happened.
    public static let stockVsTokenEquityUnavailable = StockVsTokenDTO(
        token: stockVsTokenLive.token,
        mark: appleMark,
        equity: ReferenceQuoteDTO(
            source: .pythEquity,
            status: .unavailable,
            reason: ReferenceQuoteReason.notEntitled.rawValue
        ),
        equitySymbol: "AAPL",
        premiumBps: -10,
        spreadBps: 63,
        asOf: tradingTuesday
    )

    // MARK: - Charts

    /// A dense intraday series with candles, the way Benchmarks serves it.
    ///
    /// The series is generated *inside* the session high and low it is shown with,
    /// because the chart and the stats grid below it describe the same session of
    /// the same instrument. A free-running ramp drew a curve above its own stated
    /// day high in every harness screenshot. The shape is normalised so the highest
    /// candle touches the grid's high exactly and the lowest touches its low: the
    /// two agree by construction rather than by a comment saying they do.
    ///
    /// The previous close is deliberately *not* the first point's price. It is the
    /// close of the session before this window, so the dashed baseline sits off the
    /// curve and the day change is non-zero at t0 — which is what makes the baseline
    /// worth drawing, and what a harness screenshot has to show.
    public static func chart(
        range: AssetChartRange,
        points: Int = 78,
        sessionLowUsdcMicros: Int64 = 228_200_000,
        sessionHighUsdcMicros: Int64 = 231_800_000,
        previousCloseUsdcMicros: Int64? = 226_500_000,
        basisSymbol: String = "AAPL"
    ) -> AssetChartDTO {
        let step = range.sampleInterval
        let start = tradingTuesday.timeIntervalSince1970 - Double(points) * step
        let band = max(sessionHighUsdcMicros - sessionLowUsdcMicros, 0)
        // The candle wings are a fraction of the session's own range, so a narrow
        // session does not sprout candles wider than the day they belong to.
        let upperWing = band / 12
        let lowerWing = band / 14
        let openOffset = band / 24
        let closeFloor = sessionLowUsdcMicros + lowerWing
        let closeCeiling = max(sessionHighUsdcMicros - upperWing, closeFloor)

        // A deterministic wobble on a slow uptrend: the same shape on every run, so
        // a screenshot diff means a real change. Normalised to span exactly 0...1,
        // which is what pins the extremes to the grid's high and low.
        var shape = (0..<points).map { index -> Double in
            let progress = points > 1 ? Double(index) / Double(points - 1) : 0
            let wobble = (sin(Double(index) / 5.0) + 1) / 2
            return 0.7 * progress + 0.3 * wobble
        }
        let lowest = shape.min() ?? 0
        let span = (shape.max() ?? 1) - lowest
        shape = span > 0 ? shape.map { ($0 - lowest) / span } : shape.map { _ in 0 }

        var samples: [AssetChartPointDTO] = []
        samples.reserveCapacity(points)
        for index in 0..<points {
            let price = closeFloor + Int64((Double(closeCeiling - closeFloor) * shape[index]).rounded())
            samples.append(
                AssetChartPointDTO(
                    timestamp: Int64(start + Double(index) * step),
                    priceUsdcMicros: price,
                    openUsdcMicros: price - openOffset,
                    highUsdcMicros: price + upperWing,
                    lowUsdcMicros: price - lowerWing
                )
            )
        }
        return AssetChartDTO(
            points: samples,
            previousCloseUsdcMicros: previousCloseUsdcMicros,
            range: range,
            source: .benchmarks,
            basis: .underlying,
            basisSymbol: basisSymbol,
            market: sessionOpen
        )
    }

    /// The Hermes sampler: closes only, no candles, no previous close.
    public static func chartFromFallback(range: AssetChartRange) -> AssetChartDTO {
        let dense = chart(range: range, points: 25)
        return AssetChartDTO(
            points: dense.points.map {
                AssetChartPointDTO(timestamp: $0.timestamp, priceUsdcMicros: $0.priceUsdcMicros)
            },
            previousCloseUsdcMicros: nil,
            range: range,
            source: .hermes,
            basis: .underlying,
            basisSymbol: "AAPL",
            market: sessionOpen
        )
    }

    /// The last fallback: the token's own Chainlink rounds, per token. Only a few
    /// days reach back, so the backend serves it for 1D/1W/1M and nothing longer.
    public static func chartFromChainlink(range: AssetChartRange) -> AssetChartDTO {
        let dense = chart(
            range: range,
            points: 12,
            sessionLowUsdcMicros: 231_000_000,
            sessionHighUsdcMicros: 232_400_000
        )
        return AssetChartDTO(
            points: dense.points.map {
                AssetChartPointDTO(timestamp: $0.timestamp, priceUsdcMicros: $0.priceUsdcMicros)
            },
            previousCloseUsdcMicros: nil,
            range: range,
            source: .chainlink,
            basis: .token,
            basisSymbol: "AAPLc",
            market: sessionOpen
        )
    }

    /// A symbol with no history at all in the requested window.
    public static func chartEmpty(range: AssetChartRange) -> AssetChartDTO {
        AssetChartDTO(
            points: [],
            emptyReason: "price history unavailable",
            range: range,
            source: .benchmarks,
            market: sessionOpen
        )
    }

    /// The recent SPCX listing: Benchmarks candles for the short ranges, nothing
    /// before it listed. The same session candles the partial grid folds, so the
    /// 1D chart and the grid agree — the series is generated inside `statsPartial`'s
    /// own high and low, which is what makes that true rather than hoped for.
    public static func chartRecentListing(range: AssetChartRange) -> AssetChartDTO {
        switch range {
        case .oneDay, .oneWeek, .oneMonth:
            return chart(
                range: range,
                points: 40,
                sessionLowUsdcMicros: 40_900_000,
                sessionHighUsdcMicros: 42_050_000,
                previousCloseUsdcMicros: 41_000_000,
                basisSymbol: "SPCX"
            )
        case .threeMonths, .oneYear, .all:
            return chartEmpty(range: range)
        }
    }

    // MARK: - List rows

    /// A day of the underlying's closes for a row's sparkline, shaped like the
    /// backend's downsample of the 1D Benchmarks series. Deterministic, so a
    /// screenshot diff means a real change: `drift` is the whole-day move and
    /// `wobble` how noisy the path to it is.
    public static func spark(
        startUsdcMicros: Int64 = 226_500_000,
        driftUsdcMicros: Int64 = 5_500_000,
        wobbleUsdcMicros: Int64 = 900_000,
        points: Int = 24
    ) -> [Int64] {
        guard points > 1 else { return [] }
        return (0..<points).map { index in
            let progress = Double(index) / Double(points - 1)
            let wobble = sin(Double(index) / 2.6) * Double(wobbleUsdcMicros)
            return startUsdcMicros + Int64((Double(driftUsdcMicros) * progress + wobble).rounded())
        }
    }

    /// One list row in the production shape on Base: the price is the token's
    /// Chainlink mark (per token), while the line and the day move are the
    /// underlying's (per share), both labelled `underlying`. The series ends a little
    /// under the token price because the token's multiplier has grown with
    /// reinvested dividends. Sample rows carry no `logoUrl`, because the backend
    /// never sends one: the app draws its bundled marks.
    public static func listAsset(
        symbol: String,
        name: String,
        tokenAddress: String,
        priceUsdcMicros: Int64,
        change24h: String?,
        spark: [Int64]? = nil
    ) -> MarketAssetDTO {
        let series = spark ?? Self.spark(
            startUsdcMicros: Int64(Double(priceUsdcMicros) * 0.975),
            driftUsdcMicros: Int64(Double(priceUsdcMicros) * 0.018),
            wobbleUsdcMicros: Int64(Double(priceUsdcMicros) * 0.004)
        )
        let underlying = AssetSymbolFormatter.display(symbol)
        return MarketAssetDTO(
            symbol: symbol,
            name: name,
            tokenAddress: tokenAddress,
            routable: true,
            priceUsdcMicros: priceUsdcMicros,
            change24h: change24h,
            change24hBasis: change24h == nil ? nil : .underlying,
            change24hBasisSymbol: change24h == nil ? nil : underlying,
            sparkUsdcMicros: series,
            sparkBasis: series.isEmpty ? nil : .underlying,
            sparkBasisSymbol: series.isEmpty ? nil : underlying
        )
    }

    /// A popular list with the awkward rows in it on purpose: a faller, a stock
    /// that did not move, one with no day move at all, and one with no series. The
    /// row has to stay readable in every one of those. Addresses are the pinned
    /// B20 contracts.
    public static let popularAssets: [MarketAssetDTO] = [
        listAsset(symbol: "AAPLc", name: "Apple", tokenAddress: appleTokenAddress, priceUsdcMicros: 232_050_000, change24h: "0.012400"),
        listAsset(symbol: "NVDAc", name: "NVIDIA", tokenAddress: "0xb20000000000000000000078ee7ce2fe4908108c", priceUsdcMicros: 178_200_000, change24h: "0.038600"),
        // Down on the day against the previous close while the drawn session rose
        // (a gap down at the open, then a recovery). One instrument disagreeing with
        // itself, which is correct: the pill and the line are both tinted by the day
        // move, and the row has to survive the shape pointing the other way.
        listAsset(
            symbol: "TSLAc",
            name: "Tesla",
            tokenAddress: "0xb2000000000000000000001e800a7f5189430cd0",
            priceUsdcMicros: 412_700_000,
            change24h: "-0.024100",
            spark: spark(startUsdcMicros: 395_100_000, driftUsdcMicros: 9_200_000, wobbleUsdcMicros: 1_600_000)
        ),
        listAsset(symbol: "MSFTc", name: "Microsoft", tokenAddress: "0xb200000000000000000000ab99cfa739e253872b", priceUsdcMicros: 501_300_000, change24h: "0.000000"),
        listAsset(symbol: "AMZNc", name: "Amazon", tokenAddress: "0xb200000000000000000000d9192b6b456483c2e8", priceUsdcMicros: 189_400_000, change24h: "-0.008300"),
        // No day move: Benchmarks had no previous close. The pill shows "—".
        listAsset(symbol: "GOOGLc", name: "Alphabet", tokenAddress: "0xb2000000000000000000002d0ba3164cc74f58b7", priceUsdcMicros: 168_900_000, change24h: nil),
        // No series: the row draws no line rather than a flat one. SpaceX also has
        // no bundled logo, so it is the ticker tile.
        listAsset(symbol: "SPCXc", name: "SpaceX", tokenAddress: spaceXTokenAddress, priceUsdcMicros: 41_250_000, change24h: "0.004500", spark: []),
    ]

    // MARK: - In your cabals / up for vote

    public static let heldAssets: [HeldAssetDTO] = [
        HeldAssetDTO(
            asset: popularAssets[0],
            cabals: [
                HeldAssetCabalDTO(
                    groupId: "grp_weekend",
                    name: "Weekend investors",
                    units: "4.20",
                    valueUsd: "974.61",
                    dollarPnl: "+112.40",
                    mySliceUsd: "243.65"
                ),
                HeldAssetCabalDTO(
                    groupId: "grp_semis",
                    name: "Semis or bust",
                    units: "1.10",
                    valueUsd: "255.26",
                    dollarPnl: "-18.90",
                    mySliceUsd: "51.05"
                ),
            ],
            totalValueUsd: "1229.87",
            totalDollarPnl: "+93.50",
            mySliceUsd: "294.70"
        ),
        HeldAssetDTO(
            asset: popularAssets[2],
            cabals: [
                HeldAssetCabalDTO(
                    groupId: "grp_weekend",
                    name: "Weekend investors",
                    units: "0.75",
                    valueUsd: "309.53",
                    dollarPnl: "-42.10",
                    mySliceUsd: "77.38"
                ),
            ],
            totalValueUsd: "309.53",
            totalDollarPnl: "-42.10",
            mySliceUsd: "77.38"
        ),
        // A position so small the slice rounds to nothing: the line drops the slice
        // rather than reading "your slice $0.00".
        HeldAssetDTO(
            asset: popularAssets[4],
            cabals: [
                HeldAssetCabalDTO(
                    groupId: "grp_semis",
                    name: "Semis or bust",
                    units: "0.01",
                    valueUsd: "1.89",
                    dollarPnl: "+0.04",
                    mySliceUsd: "0.00"
                ),
            ],
            totalValueUsd: "1.89",
            totalDollarPnl: "+0.04",
            mySliceUsd: "0.00"
        ),
    ]

    public static let votableAssets: [VotableAssetDTO] = [
        VotableAssetDTO(
            asset: popularAssets[1],
            openProposals: 1,
            cabalNames: ["Semis or bust"],
            soonestExpiresAt: tradingTuesday.addingTimeInterval(4 * 3600)
        ),
        VotableAssetDTO(
            asset: popularAssets[3],
            openProposals: 3,
            cabalNames: ["Weekend investors", "Semis or bust", "Rent"],
            soonestExpiresAt: tradingTuesday.addingTimeInterval(35 * 60)
        ),
    ]

    public static func heldAssetsResponse(
        held: [HeldAssetDTO] = heldAssets,
        upForVote: [VotableAssetDTO] = votableAssets,
        market: MarketStatusDTO = sessionOpen
    ) -> HeldAssetsResponseDTO {
        HeldAssetsResponseDTO(held: held, upForVote: upForVote, market: market)
    }

    // MARK: - Detail

    /// A 1 USDC buy for 0.00430000 AAPLc and a 1 AAPLc sell for $231.10.
    public static let liquidity = AssetLiquidityDTO(
        label: "Via DEX",
        routable: true,
        buyProbeUsdcMicros: 1_000_000,
        buyProbeOutAmount: "430000",
        sellProbeInAmount: "100000000",
        sellProbeOutAmount: "231100000",
        spreadBps: 63
    )

    /// Kyber found a buy route but no sell route: half a market. The backend sends
    /// no stock-vs-token card then (the card is the token's pool price, and there
    /// is none) and no spread.
    public static let liquidityNoSellRoute = AssetLiquidityDTO(
        label: "Via DEX",
        routable: true,
        buyProbeUsdcMicros: 1_000_000,
        buyProbeOutAmount: "430000"
    )

    public static func detail(
        symbol: String = "AAPLc",
        name: String = "Apple",
        market: MarketStatusDTO = sessionOpen,
        liquidity: AssetLiquidityDTO = liquidity,
        stats: AssetStatsDTO? = statsComplete,
        stockVsToken: StockVsTokenDTO? = stockVsTokenLive
    ) -> AssetDetailDTO {
        AssetDetailDTO(
            symbol: symbol,
            name: name,
            tokenAddress: appleTokenAddress,
            routable: true,
            priceUsdcMicros: 232_050_000,
            change24h: "0.021634",
            change24hBasis: .underlying,
            change24hBasisSymbol: "AAPL",
            liquidity: liquidity,
            marketSession: market.session,
            afterHours: market.afterHours,
            market: market,
            stats: stats,
            stockVsToken: stockVsToken
        )
    }

    /// A recent, thin listing: today's session but less than a year of history,
    /// no equity quote, and a sell probe that found no route, so no spread and no
    /// card.
    public static func sparseDetail() -> AssetDetailDTO {
        AssetDetailDTO(
            symbol: "SPCXc",
            name: "SpaceX",
            tokenAddress: spaceXTokenAddress,
            routable: true,
            priceUsdcMicros: 41_250_000,
            change24h: "-0.008200",
            change24hBasis: .underlying,
            change24hBasisSymbol: "SPCX",
            liquidity: AssetLiquidityDTO(
                label: "Via DEX",
                routable: true,
                buyProbeUsdcMicros: 1_000_000,
                buyProbeOutAmount: "2400000"
            ),
            marketSession: .open,
            afterHours: false,
            market: sessionOpen,
            stats: statsPartial,
            stockVsToken: nil
        )
    }
}

private extension AssetChartRange {
    /// Seconds between samples, matching what the backend asks Benchmarks for.
    var sampleInterval: Double {
        switch self {
        case .oneDay: return 5 * 60
        case .oneWeek: return 30 * 60
        case .oneMonth: return 60 * 60
        case .threeMonths, .oneYear: return 24 * 60 * 60
        case .all: return 7 * 24 * 60 * 60
        }
    }
}
