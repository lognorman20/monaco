import Foundation

/// Canned watchlist and alerts for the sample harness and tests. Deterministic apart from
/// `now`, which the stamps ("Set 2d ago") are measured from, so a screenshot says the same
/// thing whenever it is taken.
public enum WatchlistSampleData {
    /// Alphabet, the stock the alert sheet is shown on.
    public static let alphabet = MarketSampleData.listAsset(
        symbol: "GOOGLx",
        name: "Alphabet",
        priceUsdcMicros: 352_100_000,
        change24h: "0.008700"
    )

    /// A watchlist of four: a riser, a faller, one with no series, one without a day change.
    public static var watchlist: [MarketAssetDTO] {
        let popular = MarketSampleData.popularAssets
        let bySymbol = Dictionary(uniqueKeysWithValues: popular.map { ($0.symbol, $0) })
        return [alphabet] + ["TSLAx", "NEWx", "BRK.Bx"].compactMap { bySymbol[$0] }
    }

    public static func watchlistResponse(market: MarketStatusDTO = MarketSampleData.sessionOpen) -> WatchlistResponseDTO {
        WatchlistResponseDTO(assets: watchlist, market: market)
    }

    /// Alphabet's detail, as the stock screen reads it: watched, two alerts waiting.
    public static func alphabetDetail(watching: Bool = true, alertCount: Int = 2) -> AssetDetailDTO {
        AssetDetailDTO(
            symbol: alphabet.symbol,
            name: alphabet.name,
            solanaMint: alphabet.solanaMint,
            routable: true,
            priceUsdcMicros: alphabet.priceUsdcMicros,
            change24h: alphabet.change24h,
            liquidity: MarketSampleData.liquidity,
            marketSession: MarketSampleData.sessionOpen.session,
            afterHours: false,
            market: MarketSampleData.sessionOpen,
            stats: MarketSampleData.statsComplete,
            watching: watching,
            alertCount: alertCount
        )
    }

    /// Alphabet's alerts: two waiting, one that fired yesterday.
    public static func alphabetAlerts(now: Date) -> [PriceAlertDTO] {
        [
            PriceAlertDTO(
                id: "sample-alert-googl-above",
                symbol: "GOOGLx",
                direction: .above,
                priceUsdcMicros: 360_000_000,
                createdAt: now.addingTimeInterval(-2 * 86_400 - 3_600)
            ),
            PriceAlertDTO(
                id: "sample-alert-googl-below",
                symbol: "GOOGLx",
                direction: .below,
                priceUsdcMicros: 330_000_000,
                createdAt: now.addingTimeInterval(-5 * 3_600)
            ),
            PriceAlertDTO(
                id: "sample-alert-googl-fired",
                symbol: "GOOGLx",
                direction: .above,
                priceUsdcMicros: 350_000_000,
                active: false,
                createdAt: now.addingTimeInterval(-6 * 86_400),
                triggeredAt: now.addingTimeInterval(-26 * 3_600),
                triggeredPriceUsdcMicros: 350_420_000
            ),
        ]
    }

    /// Every alert across three stocks, in the server's order: waiting (newest first), then fired.
    public static func alerts(now: Date) -> PriceAlertsResponseDTO {
        let bySymbol = Dictionary(uniqueKeysWithValues: MarketSampleData.popularAssets.map { ($0.symbol, $0) })
        let googl = alphabetAlerts(now: now)
        let waiting = [
            googl[1],
            PriceAlertDTO(
                id: "sample-alert-tsla-below",
                symbol: "TSLAx",
                direction: .below,
                priceUsdcMicros: 400_000_000,
                createdAt: now.addingTimeInterval(-20 * 3_600)
            ),
            googl[0],
        ]
        let fired = [
            googl[2],
            PriceAlertDTO(
                id: "sample-alert-nvda-fired",
                symbol: "NVDAx",
                direction: .above,
                priceUsdcMicros: 175_000_000,
                active: false,
                createdAt: now.addingTimeInterval(-9 * 86_400),
                triggeredAt: now.addingTimeInterval(-4 * 86_400),
                triggeredPriceUsdcMicros: 175_310_000
            ),
        ]
        return PriceAlertsResponseDTO(
            alerts: waiting + fired,
            assets: [alphabet] + ["TSLAx", "NVDAx"].compactMap { bySymbol[$0] },
            market: MarketSampleData.sessionOpen
        )
    }
}
