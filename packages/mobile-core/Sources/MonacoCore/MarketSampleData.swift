import Foundation

/// Canned market payloads for the sample harness and for previews.
///
/// Every state the new fields can be in is named here, so the screens built on
/// them can be driven — and screenshotted — with no backend and no network. The
/// harness flags in the app pick a scenario by name; this is the data behind them.
///
/// The awkward states are the point. A stats grid with half its cells missing, an
/// equity feed our key is not entitled to, an xStock with no Pyth feed at all: the
/// backend really produces these, so the UI has to be built against them rather
/// than against a happy path that always has every number.
public enum MarketSampleData {
    /// 2026-09-22 14:00 UTC — 10:00 ET on an ordinary Tuesday.
    public static let tradingTuesday = Date(timeIntervalSince1970: 1_790_085_600)

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
        spreadBps: 12,
        confUsdcMicros: 30_000
    )

    /// A symbol listed a month ago: a session, no year behind it, no Pyth feed.
    public static let statsPartial = AssetStatsDTO(
        openUsdcMicros: 41_200_000,
        highUsdcMicros: 42_050_000,
        lowUsdcMicros: 40_900_000,
        previousCloseUsdcMicros: 41_000_000,
        spreadBps: 48
    )

    // MARK: - Stock vs token

    public static let stockVsTokenLive = StockVsTokenDTO(
        equity: ReferenceQuoteDTO(
            source: .pythEquity,
            status: .live,
            priceUsdcMicros: 231_400_000,
            confUsdcMicros: 30_000,
            publishedAt: tradingTuesday.addingTimeInterval(-8)
        ),
        token: ReferenceQuoteDTO(
            source: .pythCrypto,
            status: .live,
            priceUsdcMicros: 232_050_000,
            confUsdcMicros: 50_000,
            publishedAt: tradingTuesday.addingTimeInterval(-2)
        ),
        premiumBps: 28,
        asOf: tradingTuesday
    )

    /// After the bell: the equity print is frozen at 16:00 ET, the token keeps
    /// moving. This is the state the whole card exists to show.
    public static let stockVsTokenAfterHours = StockVsTokenDTO(
        equity: ReferenceQuoteDTO(
            source: .pythEquity,
            status: .stale,
            priceUsdcMicros: 231_400_000,
            confUsdcMicros: 30_000,
            publishedAt: tradingTuesday.addingTimeInterval(6 * 3600)
        ),
        token: ReferenceQuoteDTO(
            source: .pythCrypto,
            status: .live,
            priceUsdcMicros: 234_800_000,
            confUsdcMicros: 110_000,
            publishedAt: tradingTuesday.addingTimeInterval(9 * 3600 - 3)
        ),
        premiumBps: 147,
        asOf: tradingTuesday.addingTimeInterval(9 * 3600)
    )

    /// No Pyth crypto feed for this xStock, so the on-chain price stands in — and
    /// says so. There is no confidence interval because Jupiter publishes none.
    public static let stockVsTokenJupiterFallback = StockVsTokenDTO(
        equity: ReferenceQuoteDTO(
            source: .pythEquity,
            status: .live,
            priceUsdcMicros: 178_200_000,
            confUsdcMicros: 24_000,
            publishedAt: tradingTuesday.addingTimeInterval(-5)
        ),
        token: ReferenceQuoteDTO(
            source: .jupiter,
            status: .live,
            priceUsdcMicros: 179_050_000,
            publishedAt: tradingTuesday
        ),
        premiumBps: 47,
        asOf: tradingTuesday
    )

    /// Our key is not entitled to the equity feed. No price, no premium, and copy
    /// that says what actually happened.
    public static let stockVsTokenEquityUnavailable = StockVsTokenDTO(
        equity: ReferenceQuoteDTO(
            source: .pythEquity,
            status: .unavailable,
            reason: ReferenceQuoteReason.notEntitled.rawValue
        ),
        token: ReferenceQuoteDTO(
            source: .pythCrypto,
            status: .live,
            priceUsdcMicros: 232_050_000,
            confUsdcMicros: 50_000,
            publishedAt: tradingTuesday
        ),
        asOf: tradingTuesday
    )

    // MARK: - Charts

    /// A dense intraday series with candles, the way Benchmarks serves it.
    public static func chart(
        range: AssetChartRange,
        points: Int = 78,
        startUsdcMicros: Int64 = 226_500_000,
        previousCloseUsdcMicros: Int64? = 226_500_000
    ) -> AssetChartDTO {
        let step = range.sampleInterval
        let start = tradingTuesday.timeIntervalSince1970 - Double(points) * step
        var samples: [AssetChartPointDTO] = []
        samples.reserveCapacity(points)
        var price = startUsdcMicros
        for index in 0..<points {
            // A deterministic wobble with a slow uptrend: the same shape on every
            // run, so a screenshot diff means a real change.
            let wobble = Int64((sin(Double(index) / 5.0) * 900_000).rounded())
            price = startUsdcMicros + wobble + Int64(index) * 60_000
            samples.append(
                AssetChartPointDTO(
                    timestamp: Int64(start + Double(index) * step),
                    priceUsdcMicros: price,
                    openUsdcMicros: price - 180_000,
                    highUsdcMicros: price + 420_000,
                    lowUsdcMicros: price - 360_000
                )
            )
        }
        return AssetChartDTO(
            points: samples,
            previousCloseUsdcMicros: previousCloseUsdcMicros,
            range: range,
            source: .benchmarks,
            market: sessionOpen
        )
    }

    /// The sparse fallback: closes only, no candles, no previous close.
    public static func chartFromFallback(range: AssetChartRange) -> AssetChartDTO {
        let dense = chart(range: range, points: 25)
        return AssetChartDTO(
            points: dense.points.map {
                AssetChartPointDTO(timestamp: $0.timestamp, priceUsdcMicros: $0.priceUsdcMicros)
            },
            previousCloseUsdcMicros: nil,
            range: range,
            source: .hermes,
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

    // MARK: - Detail

    public static let liquidity = AssetLiquidityDTO(
        label: "Via Jupiter",
        routable: true,
        buyProbeUsdcMicros: 1_000_000,
        buyProbeOutAmount: "4300000",
        sellProbeInAmount: "100000000",
        sellProbeOutAmount: "232050000",
        spreadBps: 12
    )

    public static func detail(
        symbol: String = "AAPLx",
        name: String = "Apple",
        market: MarketStatusDTO = sessionOpen,
        stats: AssetStatsDTO? = statsComplete,
        stockVsToken: StockVsTokenDTO? = stockVsTokenLive
    ) -> AssetDetailDTO {
        AssetDetailDTO(
            symbol: symbol,
            name: name,
            solanaMint: "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp",
            routable: true,
            priceUsdcMicros: 232_050_000,
            change24h: "0.024500",
            liquidity: liquidity,
            marketSession: market.session,
            afterHours: market.afterHours,
            market: market,
            stats: stats,
            stockVsToken: stockVsToken
        )
    }

    /// A newly listed xStock: no year of history, no Pyth feeds, thin liquidity.
    public static func sparseDetail() -> AssetDetailDTO {
        AssetDetailDTO(
            symbol: "NEWx",
            name: "Newly listed",
            solanaMint: "NEWmint1111111111111111111111111111111111111",
            routable: true,
            priceUsdcMicros: 41_250_000,
            change24h: "-0.008200",
            liquidity: AssetLiquidityDTO(
                label: "Via Jupiter",
                routable: true,
                buyProbeUsdcMicros: 1_000_000,
                spreadBps: 48
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
