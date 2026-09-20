#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: the stock detail screen on canned market data, no sign-in and no
/// backend. Launch with `-MonacoAssetDetailSample <scenario>`.
///
/// Every state the data layer can produce is reachable from here, so each one can
/// be screenshotted: the four market sessions, a holiday and a half day, a full
/// stats grid and a half-empty one, the stock-vs-token card live / frozen after the
/// bell / falling back to the on-chain price / refused for want of an entitlement,
/// and each chart range as dense candles, as the sparse fallback, and as empty.
///
/// `MarketSampleData` in MonacoCore is the data; this is the wiring.
enum AssetDetailSampleScenario: String, CaseIterable {
    /// Market open, every stats cell sourced, both Pyth feeds live.
    case open
    /// After the bell: the equity print is frozen, the token keeps moving.
    case afterHours
    /// Pre-market, before the 09:30 print exists.
    case preMarket
    /// A holiday: closed all day, next session is a half day.
    case holiday
    /// A newly listed xStock: no year of history, no Pyth crypto feed, thin book.
    case sparse
    /// No Pyth crypto feed, so the on-chain price stands in and says so.
    case jupiterFallback
    /// Our key is not entitled to the equity feed: no price, no premium.
    case notEntitled
    /// The sparse Hermes fallback series: closes only, and no previous close, so
    /// the day-change baseline is not drawn at all.
    case fallbackSeries
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
    @ObservedObject var auth: PrivyAuthService

    var body: some View {
        NavigationStack {
            AssetDetailView(
                auth: auth,
                symbol: scenario == .sparse ? "NEWx" : "AAPLx",
                dataSource: AssetDetailSampleDataSource(scenario: scenario)
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
        case .open, .fallbackSeries, .emptyChart, .chartFailed, .loading:
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
        case .jupiterFallback:
            return MarketSampleData.detail(stockVsToken: MarketSampleData.stockVsTokenJupiterFallback)
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
        default:
            return MarketSampleData.chart(range: range)
        }
    }
}

private struct SampleChartFailure: Error {}
#endif
