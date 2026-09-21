import Foundation

/// Canned market payloads for the sample harness and for previews.
///
/// Every state the market fields can be in is named here, so the screens built on
/// them can be driven — and screenshotted — with no backend and no network. The
/// harness flags in the app pick a scenario by name; this is the data behind them.
///
/// The awkward states are the point. A stats grid with half its cells missing, an
/// equity feed our Pyth key is not entitled to, a pool with no sell route: the
/// backend really produces these, so the UI has to be built against them rather
/// than against a happy path that always has every number.
///
/// Units, as on the wire: the hero price and `mark` are the AAPLc token's Chainlink
/// total-return mark, per token; the chart, the grid and `equity` are Apple per share
/// from Pyth; `token` is the Kyber quote-implied mid, per token. The per-token figures
/// sit a little above the per-share ones because the token's multiplier has grown with
/// reinvested dividends.
public enum MarketSampleData {
    /// 2026-09-22 14:00 UTC — 10:00 ET on an ordinary Tuesday.
    public static let tradingTuesday = Date(timeIntervalSince1970: 1_790_085_600)

    /// The pinned AAPLc token contract on Base.
    public static let appleTokenAddress = "0xb200000000000000000000c2e324d24d7eecd1fb"

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
        spreadBps: 63,
        confUsdcMicros: 30_000,
        basis: .underlying,
        basisSymbol: "AAPL"
    )

    /// No year of history reached and no Pyth key: a session and a spread, nothing more.
    public static let statsPartial = AssetStatsDTO(
        openUsdcMicros: 41_200_000,
        highUsdcMicros: 42_050_000,
        lowUsdcMicros: 40_900_000,
        previousCloseUsdcMicros: 41_000_000,
        spreadBps: 149,
        basis: .underlying,
        basisSymbol: "SPCX"
    )

    // MARK: - Stock vs token

    /// The AAPLc token's Chainlink total-return mark, per token.
    public static let appleMark = ReferenceQuoteDTO(
        source: .chainlinkTRV,
        status: .live,
        priceUsdcMicros: 232_050_000
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
            askUsdcMicros: 232_558_139
        ),
        mark: appleMark,
        equity: appleEquityLive,
        equitySymbol: "AAPL",
        premiumBps: -10,
        spreadBps: 63,
        asOf: tradingTuesday
    )

    /// After the bell: the equity print is frozen at 16:00 ET and the mark holds the
    /// close, while the pools keep trading. This is the state the card exists to show.
    public static let stockVsTokenAfterHours = StockVsTokenDTO(
        token: ReferenceQuoteDTO(
            source: .dexKyber,
            status: .live,
            priceUsdcMicros: 233_210_000,
            bidUsdcMicros: 232_520_000,
            askUsdcMicros: 233_900_000
        ),
        mark: appleMark,
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

    /// Kyber found a buy route but no sell route: half a market, so no mid, no
    /// premium and no spread — and the line says why.
    public static let stockVsTokenNoRoute = StockVsTokenDTO(
        token: ReferenceQuoteDTO(
            source: .dexKyber,
            status: .unavailable,
            reason: ReferenceQuoteReason.noRoute.rawValue
        ),
        mark: appleMark,
        equity: appleEquityLive,
        equitySymbol: "AAPL",
        asOf: tradingTuesday
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
    /// The previous close is deliberately *not* the first point's price. It is the
    /// close of the session before this window, so the dashed baseline sits off the
    /// curve and the day change is non-zero at t0 — which is what makes the baseline
    /// worth drawing, and what a harness screenshot has to show.
    public static func chart(
        range: AssetChartRange,
        points: Int = 78,
        startUsdcMicros: Int64 = 226_500_000,
        previousCloseUsdcMicros: Int64? = 224_800_000
    ) -> AssetChartDTO {
        let step = range.sampleInterval
        let start = tradingTuesday.timeIntervalSince1970 - Double(points) * step
        var samples: [AssetChartPointDTO] = []
        samples.reserveCapacity(points)
        for index in 0..<points {
            // A deterministic wobble with a slow uptrend: the same shape on every
            // run, so a screenshot diff means a real change.
            let wobble = Int64((sin(Double(index) / 5.0) * 900_000).rounded())
            let price = startUsdcMicros + wobble + Int64(index) * 60_000
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
            basis: .underlying,
            basisSymbol: "AAPL",
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
        let dense = chart(range: range, points: 12, startUsdcMicros: 231_000_000)
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

    public static func detail(
        symbol: String = "AAPLc",
        name: String = "Apple",
        market: MarketStatusDTO = sessionOpen,
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
            liquidity: liquidity,
            marketSession: market.session,
            afterHours: market.afterHours,
            market: market,
            stats: stats,
            stockVsToken: stockVsToken
        )
    }

    /// A thin listing with no Pyth key configured: no year of history, no equity
    /// line, and a wide spread on the probes.
    public static func sparseDetail() -> AssetDetailDTO {
        AssetDetailDTO(
            symbol: "SPCXc",
            name: "SpaceX",
            tokenAddress: "0xb2000000000000000000000000000000000005cc",
            routable: true,
            priceUsdcMicros: 41_250_000,
            change24h: "-0.008200",
            liquidity: AssetLiquidityDTO(
                label: "Via DEX",
                routable: true,
                buyProbeUsdcMicros: 1_000_000,
                buyProbeOutAmount: "2400000",
                sellProbeInAmount: "100000000",
                sellProbeOutAmount: "41050000",
                spreadBps: 149
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
