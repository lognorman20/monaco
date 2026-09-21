#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: the stock detail screen on canned market data, no sign-in and no
/// backend. Launch with `-MonacoAssetDetailSample <scenario>`.
///
/// Every state the data layer can produce is reachable from here, so each one can
/// be screenshotted: the market sessions, a holiday, a full stats grid and a
/// half-empty one, the stock-vs-token card live / after the bell / with no sell
/// route / without an entitled equity feed, and each chart range as dense Pyth
/// candles, as the Hermes fallback, as the Chainlink token rounds and as empty.
///
/// `MarketSampleData` in MonacoCore is the data; this is the wiring.
enum AssetDetailSampleScenario: String, CaseIterable {
    /// Market open, every stats cell sourced, Kyber and the equity line live.
    case open
    /// After the bell: the equity print is frozen, the pools keep trading.
    case afterHours
    /// Pre-market, before the 09:30 print exists.
    case preMarket
    /// A holiday: closed all day, next session is a half day.
    case holiday
    /// A thin listing with no Pyth key: no year of history, no card, wide spread.
    case sparse
    /// Kyber found no sell route: the token line is unavailable and says why.
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

    static let launchArgument = "-MonacoAssetDetailSample"

    static var requested: AssetDetailSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument), arguments.indices.contains(flag + 1) else { return nil }
        return AssetDetailSampleScenario(rawValue: arguments[flag + 1])
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
                sources: StocksFlowSources(detail: AssetDetailSampleDataSource(scenario: scenario))
            )
        }
        .tint(MonacoTheme.ink)
    }
}

/// Canned answers for the two calls the screen makes. `loading` never returns, which
/// is how the skeleton is screenshotted.
private struct AssetDetailSampleDataSource: AssetDetailDataSource {
    let scenario: AssetDetailSampleScenario

    func detail(symbol: String) async throws -> AssetDetailDTO {
        if scenario == .loading { try await Task.sleep(for: .seconds(3600)) }
        switch scenario {
        case .open, .fallbackSeries, .chainlinkSeries, .emptyChart, .chartFailed, .loading:
            return MarketSampleData.detail()
        case .afterHours:
            return MarketSampleData.detail(
                market: MarketSampleData.sessionAfterHours,
                stockVsToken: MarketSampleData.stockVsTokenAfterHours
            )
        case .preMarket:
            return MarketSampleData.detail(market: MarketSampleData.sessionPreMarket)
        case .holiday:
            return MarketSampleData.detail(market: MarketSampleData.sessionHoliday)
        case .sparse:
            return MarketSampleData.sparseDetail()
        case .noRoute:
            return MarketSampleData.detail(stockVsToken: MarketSampleData.stockVsTokenNoRoute)
        case .notEntitled:
            return MarketSampleData.detail(stockVsToken: MarketSampleData.stockVsTokenEquityUnavailable)
        }
    }

    func chart(symbol: String, range: AssetChartRange) async throws -> AssetChartDTO {
        switch scenario {
        case .loading:
            try await Task.sleep(for: .seconds(3600))
            return MarketSampleData.chart(range: range)
        case .chartFailed:
            throw SampleChartFailure()
        case .emptyChart, .sparse:
            return MarketSampleData.chartEmpty(range: range)
        case .fallbackSeries:
            return MarketSampleData.chartFromFallback(range: range)
        case .chainlinkSeries:
            // The rounds only reach back days, so the backend serves no long range from them.
            switch range {
            case .oneDay, .oneWeek, .oneMonth: return MarketSampleData.chartFromChainlink(range: range)
            case .threeMonths, .oneYear, .all: return MarketSampleData.chartEmpty(range: range)
            }
        default:
            return MarketSampleData.chart(range: range)
        }
    }
}

private struct SampleChartFailure: Error {}
#endif
