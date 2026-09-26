#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: the watchlist and price alerts on canned data, no sign-in and no backend.
/// Launch with `-MonacoWatchlistSample <scenario>`.
///
/// Every state the three surfaces can be in is reachable from here: the Stocks tab with a
/// watchlist, with the first-use hint, loading, and in its edit sheet; the stock screen
/// with its star and alert button; the alert sheet with a preset chosen and with a line the
/// price has already passed; and the alerts page full, empty, loading and failed.
///
/// `WatchlistSampleData` in MonacoCore is the data; this is the wiring.
enum WatchlistSampleScenario: String, CaseIterable {
    /// The Stocks tab with four watched stocks above the member's cabals.
    case stocksTab
    /// A member who has never starred a stock: no section, one line under the search.
    case stocksTabHint
    /// The watchlist still loading, holding room for the rows it had last time.
    case stocksTabLoading
    /// The edit sheet: drag handles and delete controls.
    case stocksTabEdit
    /// Alphabet's stock screen, starred, two alerts waiting.
    case stock
    /// The alert sheet over Alphabet with +5% chosen.
    case alertSheet
    /// Below, with a typed price the stock is already under.
    case alertSheetProblem
    /// Every alert, grouped by stock, waiting then fired.
    case alerts
    /// No alerts yet.
    case alertsEmpty
    /// Nothing has answered yet.
    case alertsLoading
    /// The first read failed.
    case alertsFailed

    static let launchArgument = "-MonacoWatchlistSample"

    static var requested: WatchlistSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument), arguments.indices.contains(flag + 1) else { return nil }
        return WatchlistSampleScenario(rawValue: arguments[flag + 1])
    }
}

struct WatchlistSampleHarness: View {
    let scenario: WatchlistSampleScenario
    @ObservedObject var auth: PrivyAuthService

    @State private var session = AppSessionStore()
    @State private var watchlist: WatchlistModel
    @State private var isPrepared = false
    private let source: WatchlistSampleSource

    init(scenario: WatchlistSampleScenario, auth: PrivyAuthService) {
        self.scenario = scenario
        self.auth = auth
        let source = WatchlistSampleSource(scenario: scenario, now: Date())
        self.source = source
        let memory: InMemoryWatchlistMemory
        switch scenario {
        case .stocksTabHint: memory = InMemoryWatchlistMemory(lastCount: 0, hasWatched: false)
        default: memory = InMemoryWatchlistMemory(lastCount: 3, hasWatched: true)
        }
        _watchlist = State(initialValue: WatchlistModel(dataSource: source, memory: memory))
    }

    var body: some View {
        NavigationStack {
            if isPrepared {
                screen
            } else {
                Color.clear
            }
        }
        .environment(session)
        .tint(MonacoTheme.ink)
        .task {
            if scenario == .stocksTabEdit {
                await watchlist.load()
                watchlist.isEditing = true
            }
            isPrepared = true
        }
    }

    @ViewBuilder
    private var screen: some View {
        switch scenario {
        case .stocksTab, .stocksTabHint, .stocksTabLoading, .stocksTabEdit:
            AssetsTabView(
                auth: auth,
                model: StocksTabModel(dataSource: WatchlistSampleStocksSource()),
                watchlist: watchlist
            )
        case .stock, .alertSheet, .alertSheetProblem:
            AssetDetailView(
                auth: auth,
                symbol: WatchlistSampleData.alphabet.symbol,
                dataSource: WatchlistSampleDetailSource(),
                socialDataSource: WatchlistSampleSocialSource(),
                watchDataSource: source,
                alertSheetPrefill: prefill
            )
        case .alerts, .alertsEmpty, .alertsLoading, .alertsFailed:
            AlertsView(auth: auth, dataSource: source)
        }
    }

    private var prefill: PriceAlertPrefill? {
        switch scenario {
        case .alertSheet: return PriceAlertPrefill(direction: .above, presetPercent: 5)
        case .alertSheetProblem: return PriceAlertPrefill(direction: .below, amountText: "360")
        default: return nil
        }
    }
}

/// Canned answers for the watchlist and the alerts. `stocksTabLoading` and `alertsLoading`
/// never answer, which is how their skeletons are screenshotted.
@MainActor
private final class WatchlistSampleSource: WatchlistDataSource, PriceAlertDataSource {
    let scenario: WatchlistSampleScenario
    let now: Date
    private var watched: [MarketAssetDTO]

    init(scenario: WatchlistSampleScenario, now: Date) {
        self.scenario = scenario
        self.now = now
        watched = scenario == .stocksTabHint ? [] : WatchlistSampleData.watchlist
    }

    func watchlist() async throws -> WatchlistResponseDTO {
        if scenario == .stocksTabLoading { try await Task.sleep(for: .seconds(3600)) }
        return WatchlistResponseDTO(assets: watched, market: MarketSampleData.sessionOpen)
    }

    func add(symbol: String) async throws {
        guard !watched.contains(where: { $0.symbol == symbol }) else { return }
        if symbol == WatchlistSampleData.alphabet.symbol { watched.append(WatchlistSampleData.alphabet) }
    }

    func remove(symbol: String) async throws {
        watched.removeAll { $0.symbol == symbol }
    }

    func reorder(symbols: [String]) async throws -> [String] {
        let bySymbol = Dictionary(uniqueKeysWithValues: watched.map { ($0.symbol, $0) })
        watched = symbols.compactMap { bySymbol[$0] }
        return symbols
    }

    func alerts(symbol: String?) async throws -> PriceAlertsResponseDTO {
        switch scenario {
        case .alertsLoading: try await Task.sleep(for: .seconds(3600))
        case .alertsFailed: throw WatchlistSampleFailure()
        case .alertsEmpty: return PriceAlertsResponseDTO(alerts: [], market: MarketSampleData.sessionOpen)
        default: break
        }
        let all = WatchlistSampleData.alerts(now: now)
        guard let symbol else { return all }
        return PriceAlertsResponseDTO(
            alerts: all.alerts.filter { $0.symbol.caseInsensitiveCompare(symbol) == .orderedSame },
            assets: all.assets.filter { $0.symbol.caseInsensitiveCompare(symbol) == .orderedSame },
            market: all.market
        )
    }

    func createAlert(symbol: String, direction: PriceAlertDirection, lineUsdcMicros: Int64) async throws -> PriceAlertDTO {
        PriceAlertDTO(id: UUID().uuidString, symbol: symbol, direction: direction, priceUsdcMicros: lineUsdcMicros, createdAt: Date())
    }

    func deleteAlert(id: String) async throws {}
}

/// Alphabet's detail and a day of its chart.
@MainActor
private struct WatchlistSampleDetailSource: AssetDetailDataSource {
    func detail(symbol: String) async throws -> AssetDetailDTO {
        WatchlistSampleData.alphabetDetail()
    }

    func chart(symbol: String, range: AssetChartRange) async throws -> AssetChartDTO {
        let dense = MarketSampleData.chart(range: range, startUsdcMicros: 346_800_000, previousCloseUsdcMicros: 349_100_000)
        return AssetChartDTO(
            points: dense.points,
            previousCloseUsdcMicros: dense.previousCloseUsdcMicros,
            range: range,
            source: dense.source,
            basis: .underlying,
            basisSymbol: "GOOGL",
            market: MarketSampleData.sessionOpen
        )
    }
}

/// Nobody in the member's cabals holds Alphabet.
@MainActor
private struct WatchlistSampleSocialSource: AssetSocialDataSource {
    func social(symbol: String) async throws -> AssetSocialDTO {
        AssetSocialSampleData.empty(symbol: symbol)
    }
}

/// The rest of the Stocks tab, so the watchlist is seen above what it sits on.
@MainActor
private struct WatchlistSampleStocksSource: StocksTabDataSource {
    func search(query: String, offset: Int, limit: Int) async throws -> ListMarketAssetsResponse {
        ListMarketAssetsResponse(assets: [], hasMore: false, market: MarketSampleData.sessionOpen)
    }

    func popular(limit: Int) async throws -> PopularAssetsResponse {
        PopularAssetsResponse(assets: Array(MarketSampleData.popularAssets.prefix(limit)), market: MarketSampleData.sessionOpen)
    }

    func held() async throws -> HeldAssetsResponse {
        MarketSampleData.heldAssetsResponse(market: MarketSampleData.sessionOpen)
    }
}

private struct WatchlistSampleFailure: Error {}
#endif
