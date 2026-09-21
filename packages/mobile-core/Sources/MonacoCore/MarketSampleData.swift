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
        confUsdcMicros: 30_000,
        basis: .underlying,
        basisSymbol: "AAPL"
    )

    /// A symbol listed a month ago: a session, no year behind it, no Pyth feed.
    public static let statsPartial = AssetStatsDTO(
        openUsdcMicros: 41_200_000,
        highUsdcMicros: 42_050_000,
        lowUsdcMicros: 40_900_000,
        previousCloseUsdcMicros: 41_000_000,
        spreadBps: 48,
        basis: .underlying,
        basisSymbol: "NEW"
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
            basis: .underlying,
            basisSymbol: "AAPL",
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
            basis: .underlying,
            basisSymbol: "AAPL",
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

    // MARK: - List rows

    /// A day of closes for a row's sparkline, shaped like the backend's downsample
    /// of the 1D series. Deterministic, so a screenshot diff means a real change:
    /// `drift` is the whole-day move and `wobble` how noisy the path to it is.
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

    /// The catalogue's logo URL for a ticker, in the shape the xStocks metadata host
    /// publishes: `https://xstocks-metadata.backed.fi/logos/tokens/AAPLx.png`.
    ///
    /// Sample rows carry no logo by default, so a screen rendered from this data is
    /// deterministic and needs no network — which is what the UI tests rely on. A
    /// demo or a screenshot that wants the real marks opts in with
    /// `-MonacoSampleLogos`, and `sampleLogosRequested` reports that.
    public static func logoURL(forSymbol symbol: String) -> String {
        "https://xstocks-metadata.backed.fi/logos/tokens/\(symbol).png"
    }

    /// Whether this process was launched asking sample rows to carry real logos.
    public static var sampleLogosRequested: Bool {
        ProcessInfo.processInfo.arguments.contains("-MonacoSampleLogos")
    }

    /// `logoUrl` as given, or the catalogue URL when the process asked for real logos.
    public static func resolvedLogoURL(explicit: String?, symbol: String) -> String? {
        if let explicit { return explicit }
        return sampleLogosRequested ? logoURL(forSymbol: symbol) : nil
    }

    public static func listAsset(
        symbol: String,
        name: String,
        priceUsdcMicros: Int64,
        change24h: String?,
        spark: [Int64]? = nil,
        logoUrl: String? = nil
    ) -> MarketAssetDTO {
        let series = spark ?? Self.spark(
            startUsdcMicros: priceUsdcMicros,
            driftUsdcMicros: Int64(Double(priceUsdcMicros) * 0.018),
            wobbleUsdcMicros: Int64(Double(priceUsdcMicros) * 0.004)
        )
        return MarketAssetDTO(
            symbol: symbol,
            name: name,
            solanaMint: "Xs" + String(symbol.uppercased().prefix(4)) + "1111111111111111111111111111",
            routable: true,
            priceUsdcMicros: priceUsdcMicros,
            change24h: change24h,
            sparkUsdcMicros: series,
            // The production shape: Pyth serves the underlying equity, Jupiter
            // prices the token. Sample rows carry the same labels so the harness
            // renders what the app really gets rather than a simplified version.
            sparkBasis: series.isEmpty ? nil : .underlying,
            sparkBasisSymbol: series.isEmpty ? nil : String(symbol.dropLast()),
            changeBasis: change24h == nil ? nil : .token,
            changeBasisSymbol: change24h == nil ? nil : symbol,
            logoUrl: Self.resolvedLogoURL(explicit: logoUrl, symbol: symbol)
        )
    }

    /// A popular list with the awkward rows in it on purpose: a faller, a stock
    /// that did not move, one with no day change at all, and one with no series —
    /// the row has to stay readable in every one of those.
    public static let popularAssets: [MarketAssetDTO] = [
        listAsset(symbol: "AAPLx", name: "Apple", priceUsdcMicros: 232_050_000, change24h: "0.012400"),
        listAsset(symbol: "NVDAx", name: "NVIDIA", priceUsdcMicros: 178_200_000, change24h: "0.038600"),
        // The divergence, on purpose: the token is down on the day while the drawn
        // window — Tesla on NASDAQ — rose. The line is tinted from the line, the
        // pill from the token, and the row has to survive the two disagreeing.
        listAsset(
            symbol: "TSLAx",
            name: "Tesla",
            priceUsdcMicros: 412_700_000,
            change24h: "-0.024100",
            spark: spark(startUsdcMicros: 404_100_000, driftUsdcMicros: 9_200_000, wobbleUsdcMicros: 1_600_000)
        ),
        listAsset(symbol: "MSFTx", name: "Microsoft", priceUsdcMicros: 501_300_000, change24h: "0.000000"),
        listAsset(symbol: "AMZNx", name: "Amazon", priceUsdcMicros: 189_400_000, change24h: "-0.008300"),
        // No day change: the pill shows "—" and the row still lays out.
        listAsset(symbol: "BRK.Bx", name: "Berkshire Hathaway", priceUsdcMicros: 468_900_000, change24h: nil),
        // No series: the row draws no sparkline rather than a flat line.
        listAsset(symbol: "NEWx", name: "Newly listed", priceUsdcMicros: 41_250_000, change24h: "0.004500", spark: []),
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
                    dollarPnl: "112.40",
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
            totalDollarPnl: "93.50",
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
                    dollarPnl: "0.04",
                    mySliceUsd: "0.00"
                ),
            ],
            totalValueUsd: "1.89",
            totalDollarPnl: "0.04",
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
