#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: the Stocks tab on canned market data, no sign-in and no backend.
/// Launch with `-MonacoStocksTabSample <scenario>`; add `-MonacoStocksTabScroll bottom`
/// to open it scrolled down to the mover cards and the catalogue.
///
/// Every state the four sections can be in is reachable from here, so each one can
/// be screenshotted: the whole tab populated, a member whose cabals own nothing,
/// a cabal read that failed while the market is still live, a catalogue that would
/// not load at all, symbols with no day series, and the session moon on the rows.
///
/// `MarketSampleData` in MonacoCore is the data; this is the wiring.
enum StocksTabSampleScenario: String, CaseIterable {
    /// All four sections, with the awkward rows in them: a faller, a stock that
    /// did not move, one with no day change and one with no series.
    case full
    /// Signed in, in no cabals, or in cabals that have bought nothing.
    case noCabals
    /// The cabal read failed; the catalogue did not. The market stays live.
    case cabalsFailed
    /// The cabal sections loaded once and then a refresh failed. The rows stay on
    /// screen — they are the member's own money — with the caption that says they
    /// are not fresh.
    case cabalsStale
    /// The catalogue would not load. Nothing else can be shown.
    case popularFailed
    /// Nothing has answered yet: the skeleton.
    case loading
    /// A catalogue with no day series at all — rows without sparklines.
    case noSeries
    /// After the bell: the rows carry the moon.
    case afterHours

    static let launchArgument = "-MonacoStocksTabSample"

    static var requested: StocksTabSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument), arguments.indices.contains(flag + 1) else { return nil }
        return StocksTabSampleScenario(rawValue: arguments[flag + 1])
    }
}

struct StocksTabSampleHarness: View {
    let scenario: StocksTabSampleScenario
    @ObservedObject var auth: PrivyAuthService
    @State private var session = AppSessionStore()
    @State private var model: StocksTabModel
    @State private var isPrepared = false

    init(scenario: StocksTabSampleScenario, auth: PrivyAuthService) {
        self.scenario = scenario
        self.auth = auth
        _model = State(initialValue: StocksTabModel(dataSource: StocksTabSampleDataSource(scenario: scenario)))
    }

    var body: some View {
        NavigationStack {
            if isPrepared {
                // lane: news
                AssetsTabView(auth: auth, model: model, newsDataSource: NewsSampleSource(scenario: .tab))
            } else {
                Color.clear
            }
        }
        .environment(session)
        .tint(MonacoTheme.ink)
        .defaultScrollAnchor(SampleScrollAnchor.requested(by: "-MonacoStocksTabScroll"))
        .task {
            await prepare()
            isPrepared = true
        }
    }

    /// Most scenarios are one canned answer per read. `cabalsStale` is two: the
    /// cabal sections have to land before a refresh can fail on top of them, and
    /// that sequence is the state worth screenshotting.
    private func prepare() async {
        guard scenario == .cabalsStale else { return }
        await model.loadSocial()
        await model.loadSocial()
    }
}

/// Canned answers for the three reads the tab makes. `loading` never returns,
/// which is how the skeleton is screenshotted.
private final class StocksTabSampleDataSource: StocksTabDataSource {
    let scenario: StocksTabSampleScenario
    /// `cabalsStale` answers once and then stops, which is what a refresh failing
    /// over rows that are already on screen looks like.
    private var heldReads = 0

    init(scenario: StocksTabSampleScenario) {
        self.scenario = scenario
    }

    private var market: MarketStatusDTO {
        scenario == .afterHours ? MarketSampleData.sessionAfterHours : MarketSampleData.sessionOpen
    }

    private var assets: [MarketAssetDTO] {
        guard scenario == .noSeries else { return MarketSampleData.popularAssets }
        return MarketSampleData.popularAssets.map { asset in
            MarketAssetDTO(
                symbol: asset.symbol,
                name: asset.name,
                solanaMint: asset.solanaMint,
                routable: asset.routable,
                priceUsdcMicros: asset.priceUsdcMicros,
                change24h: asset.change24h,
                sparkUsdcMicros: [],
                logoUrl: asset.logoUrl
            )
        }
    }

    func search(query: String, offset: Int, limit: Int) async throws -> ListMarketAssetsResponse {
        if scenario == .loading { try await Task.sleep(for: .seconds(3600)) }
        let needle = query.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        let matches = assets.filter {
            needle.isEmpty
                || $0.symbol.lowercased().contains(needle)
                || $0.name.lowercased().contains(needle)
        }
        return ListMarketAssetsResponse(assets: matches, hasMore: false, market: market)
    }

    func popular(limit: Int) async throws -> PopularAssetsResponse {
        if scenario == .loading { try await Task.sleep(for: .seconds(3600)) }
        if scenario == .popularFailed { throw SampleStocksFailure() }
        return PopularAssetsResponse(assets: Array(assets.prefix(limit)), market: market)
    }

    func held() async throws -> HeldAssetsResponse {
        if scenario == .loading { try await Task.sleep(for: .seconds(3600)) }
        if scenario == .cabalsFailed { throw SampleStocksFailure() }
        if scenario == .cabalsStale {
            heldReads += 1
            if heldReads > 1 { throw SampleStocksFailure() }
            return MarketSampleData.heldAssetsResponse(market: market)
        }
        if scenario == .noCabals || scenario == .popularFailed {
            return HeldAssetsResponse(held: [], upForVote: [], market: market)
        }
        return MarketSampleData.heldAssetsResponse(market: market)
    }
}

private struct SampleStocksFailure: Error {}
#endif
