#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: the stock detail screen on canned market data, no sign-in and no
/// backend. Launch with `-MonacoAssetDetailSample <scenario>`.
///
/// Every state the data layer can produce is reachable from here: the market
/// sessions, a weekend, a holiday, a full stats grid and a half-empty one, the
/// stock-vs-token card live / after the bell / with a mark holding Friday's close /
/// without an entitled equity feed / absent for want of a sell route, and each chart
/// range as dense Pyth candles, as the Hermes fallback, as the Chainlink token
/// rounds and as empty.
///
/// The screen on this branch draws the hero, the move and the chart; the stats
/// grid and the card views land with stocks-detail-cards. Until then the
/// scenarios that differ only in the grid or the card (noRoute, notEntitled,
/// weekend) draw the same screen. The data is here so those views are built and
/// screenshotted against exactly what the backend sends.
///
/// `MarketSampleData` in MonacoCore is the data; this is the wiring.
enum AssetDetailSampleScenario: String, CaseIterable {
    /// Market open, every stats cell sourced, Kyber and the equity line live.
    case open
    /// After the bell: the equity print is frozen, the 24/5 mark and the pools keep
    /// moving.
    case afterHours
    /// Pre-market, before the 09:30 print exists.
    case preMarket
    /// Saturday: the mark holds Friday's close, so it is stale and there is no
    /// premium.
    case weekend
    /// A holiday: closed all day, next session is a half day.
    case holiday
    /// A recent, thin listing (SPCXc): today's session but no year of history, no
    /// equity quote, no sell route, so no spread and no card.
    case sparse
    /// Kyber found no sell route: no spread, and no card at all.
    case noRoute
    /// Our Pyth key is not entitled to the equity feed: no reference line.
    case notEntitled
    /// The sparse Hermes fallback series: closes only, and no previous close, so
    /// the day-change baseline is not drawn at all.
    case fallbackSeries
    /// The last fallback: the token's own Chainlink rounds, per token.
    case chainlinkSeries
    /// A symbol with no history in the window.
    case emptyChart
    /// The chart call fails while the detail call succeeds.
    case chartFailed
    /// Both calls are still in flight.
    case loading
    /// The price moves every few seconds, the way a poll makes it: the digits roll
    /// and the change pill flashes. This is the scenario the live hero is
    /// screenshotted and demoed from.
    case ticking
    /// Every range but the one on screen takes seconds to answer, so the chip
    /// carries its spinner while the curve already drawn stays put.
    case slowRange
    /// The server answers with a series built for another window. It must be
    /// refused rather than drawn under the wrong chip.
    case staleRange
    /// The background chart re-read, at a cadence you can watch: every couple of
    /// seconds the window grows a bar, the way an open market's day chart does. The
    /// curve must not wipe itself left to right each time, and the selected chip
    /// must not sprout a spinner for a refresh nobody asked for.
    case tickingChart

    static let launchArgument = "-MonacoAssetDetailSample"

    static var requested: AssetDetailSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument), arguments.indices.contains(flag + 1) else { return nil }
        return AssetDetailSampleScenario(rawValue: arguments[flag + 1])
    }

    /// `-MonacoScrubHolds` keeps a scrub selected after the finger lifts, so a UI
    /// test can drag and then read the hero. XCUITest's press-drag-hold is one
    /// synthesised gesture that only returns once the touch has ended, so without
    /// this the screen has always snapped back before a test can look at it.
    static var scrubHolds: Bool {
        ProcessInfo.processInfo.arguments.contains("-MonacoScrubHolds")
    }
}

struct AssetDetailSampleHarness: View {
    let scenario: AssetDetailSampleScenario
    @ObservedObject var auth: DynamicAuthService

    var body: some View {
        NavigationStack {
            AssetDetailView(
                auth: auth,
                symbol: scenario == .sparse ? "SPCXc" : "AAPLc",
                sources: StocksFlowSources(detail: AssetDetailSampleDataSource(scenario: scenario)),
                // The scripted price walk is the point of `ticking`; at the shipping
                // cadence a screenshot would wait ten seconds for the first move.
                pricePollInterval: scenario == .ticking ? .seconds(2) : AssetDetailPolling.price,
                // Same for the quiet chart re-read, which ships at two minutes.
                chartPollInterval: scenario == .tickingChart ? .seconds(2) : AssetDetailPolling.chart
            )
        }
        .tint(MonacoTheme.ink)
        .environment(\.scrubSelectionPersists, AssetDetailSampleScenario.scrubHolds)
    }
}

/// Canned answers for the two calls the screen makes. `loading` never returns, which
/// is how the skeleton is screenshotted.
///
/// A class, not a struct, because `ticking` and `tickingChart` have to remember how
/// many times they have been polled: that is the whole state the live hero is built on.
@MainActor
private final class AssetDetailSampleDataSource: AssetDetailDataSource {
    let scenario: AssetDetailSampleScenario
    private var detailCalls = 0
    private var chartCalls = 0

    init(scenario: AssetDetailSampleScenario) {
        self.scenario = scenario
    }

    func detail(symbol: String) async throws -> AssetDetailDTO {
        if scenario == .loading { try await Task.sleep(for: .seconds(3600)) }
        detailCalls += 1
        switch scenario {
        case .open, .fallbackSeries, .chainlinkSeries, .emptyChart, .chartFailed, .loading,
             .slowRange, .staleRange, .tickingChart:
            return MarketSampleData.detail()
        case .ticking:
            return tickingDetail()
        case .afterHours:
            return MarketSampleData.detail(
                market: MarketSampleData.sessionAfterHours,
                stockVsToken: MarketSampleData.stockVsTokenAfterHours
            )
        case .preMarket:
            return MarketSampleData.detail(market: MarketSampleData.sessionPreMarket)
        case .weekend:
            return MarketSampleData.detail(
                market: MarketSampleData.sessionWeekend,
                stockVsToken: MarketSampleData.stockVsTokenWeekend
            )
        case .holiday:
            return MarketSampleData.detail(market: MarketSampleData.sessionHoliday)
        case .sparse:
            return MarketSampleData.sparseDetail()
        case .noRoute:
            return MarketSampleData.detail(liquidity: MarketSampleData.liquidityNoSellRoute, stockVsToken: nil)
        case .notEntitled:
            return MarketSampleData.detail(stockVsToken: MarketSampleData.stockVsTokenEquityUnavailable)
        }
    }

    func chart(symbol: String, range: AssetChartRange) async throws -> AssetChartDTO {
        chartCalls += 1
        switch scenario {
        case .tickingChart:
            // One more bar every re-read, which is what an open market's day chart
            // does. Same range, same shape, one sample longer.
            return MarketSampleData.chart(range: range, points: 78 + chartCalls)
        case .loading:
            try await Task.sleep(for: .seconds(3600))
            return MarketSampleData.chart(range: range)
        case .chartFailed:
            throw SampleChartFailure()
        case .emptyChart:
            return MarketSampleData.chartEmpty(range: range)
        case .sparse:
            // The same session candles the partial grid is folded from, and nothing
            // before the listing.
            return MarketSampleData.chartRecentListing(range: range)
        case .fallbackSeries:
            return MarketSampleData.chartFromFallback(range: range)
        case .chainlinkSeries:
            // The feed holds every round it ever published, which on a B20 token is
            // weeks, not months: the session, the week, the month and the whole
            // history draw, and the two ranges older than the feed say which day it
            // started rather than repeating "unavailable".
            switch range {
            case .oneDay, .oneWeek, .oneMonth, .all: return MarketSampleData.chartFromChainlink(range: range)
            case .threeMonths, .oneYear: return MarketSampleData.chartBeforeTheFeedExisted(range: range)
            }
        case .slowRange:
            // The day chart lands at once; every other window makes the member wait,
            // which is exactly when the chip has to say it is working.
            if range != .oneDay { try await Task.sleep(for: .seconds(6)) }
            return MarketSampleData.chart(range: range)
        case .staleRange:
            // Always a window other than the one asked for. Answering 1Y to every
            // chip made 1Y itself the one chip where the mismatch could not be shown:
            // tapping it succeeded and drew.
            return MarketSampleData.chart(range: range == .oneYear ? .oneDay : .oneYear)
        default:
            return MarketSampleData.chart(range: range)
        }
    }

    /// A price that walks: up, up, down, up… deterministic, so the flash and the
    /// digit roll can be screenshotted and compared between runs. Only the token's
    /// mark moves; everything else is the `open` sample.
    private func tickingDetail() -> AssetDetailDTO {
        let steps: [Int64] = [0, 180_000, 420_000, -260_000, 150_000, -90_000, 520_000]
        let drift = steps[detailCalls % steps.count]
        let base = MarketSampleData.detail()
        return AssetDetailDTO(
            symbol: base.symbol,
            name: base.name,
            tokenAddress: base.tokenAddress,
            routable: base.routable,
            priceUsdcMicros: (base.priceUsdcMicros ?? 232_050_000) + drift,
            change24h: base.change24h,
            change24hBasis: base.change24hBasis,
            change24hBasisSymbol: base.change24hBasisSymbol,
            liquidity: base.liquidity,
            marketSession: base.marketSession,
            afterHours: base.afterHours,
            market: base.market,
            stats: base.stats,
            stockVsToken: base.stockVsToken
        )
    }
}

private struct SampleChartFailure: Error {}
#endif
