#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: news on the stock screen and the Stocks tab, on canned headlines, with no
/// sign-in and no backend. Launch with `-MonacoNewsSample <scenario>`; add
/// `-MonacoNewsScroll <center|bottom|0…1>` to open the stock screen scrolled down to the
/// News section.
///
/// Its own harness rather than more cases on `AssetDetailSampleHarness`: it drives two
/// screens (the stock screen and the Stocks tab) plus the full list, and every state of
/// the one section — loading, answered, empty, failed — which is a different axis from
/// the market sessions that harness walks. The other stock-screen and Stocks-tab samples
/// pass `NewsSampleSource(.detail)` / `(.tab)` so their screenshots show headlines too.
///
/// `NewsSampleData` in MonacoCore is the data; this is the wiring.
enum NewsSampleScenario: String, CaseIterable {
    /// The stock screen with Apple's headlines: three rows and "See all".
    case detail
    /// The feed answered with nothing: "No news for Apple yet."
    case detailEmpty
    /// The feed has not answered: three rows in the headlines' shape.
    case detailLoading
    /// The read failed with nothing on screen: one quiet line and a retry.
    case detailFailed
    /// "See all": every headline the server sent, as a full-screen list.
    case all
    /// The Stocks tab with "Market today" under the movers.
    case tab
    /// The Stocks tab while the market pulse is still being read.
    case tabLoading
    /// The Stocks tab after the market pulse failed.
    case tabFailed

    static let launchArgument = "-MonacoNewsSample"

    static var requested: NewsSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument), arguments.indices.contains(flag + 1) else { return nil }
        return NewsSampleScenario(rawValue: arguments[flag + 1])
    }
}

struct NewsSampleHarness: View {
    let scenario: NewsSampleScenario
    @ObservedObject var auth: PrivyAuthService
    @State private var session = AppSessionStore()
    @State private var listModel: NewsFeedModel

    init(scenario: NewsSampleScenario, auth: PrivyAuthService) {
        self.scenario = scenario
        self.auth = auth
        let source = NewsSampleSource(scenario: scenario)
        _listModel = State(initialValue: NewsFeedModel(fetch: { try await source.assetNews(symbol: "AAPLx") }))
    }

    var body: some View {
        NavigationStack {
            switch scenario {
            case .detail, .detailEmpty, .detailLoading, .detailFailed:
                AssetDetailView(
                    auth: auth,
                    symbol: "AAPLx",
                    dataSource: NewsSampleDetailSource(),
                    socialDataSource: NewsSampleSocialSource(),
                    newsDataSource: NewsSampleSource(scenario: scenario)
                )
            case .all:
                AssetNewsView(model: listModel, companyName: "Apple")
                    .task { await listModel.load() }
            case .tab, .tabLoading, .tabFailed:
                AssetsTabView(
                    auth: auth,
                    dataSource: NewsSampleStocksSource(),
                    newsDataSource: NewsSampleSource(scenario: scenario)
                )
            }
        }
        .environment(session)
        .tint(MonacoTheme.ink)
        .defaultScrollAnchor(SampleScrollAnchor.requested(by: "-MonacoNewsScroll"))
    }
}

/// Canned headlines. `…Loading` never answers, which is how the skeleton is shot.
@MainActor
struct NewsSampleSource: NewsDataSource {
    let scenario: NewsSampleScenario

    func assetNews(symbol: String) async throws -> NewsFeedDTO {
        switch scenario {
        case .detailLoading:
            try await Task.sleep(for: .seconds(3600))
            return NewsSampleData.empty()
        case .detailFailed:
            throw NewsSampleFailure()
        case .detailEmpty:
            return NewsSampleData.empty()
        default:
            return NewsSampleData.apple()
        }
    }

    func marketNews() async throws -> NewsFeedDTO {
        switch scenario {
        case .tabLoading:
            try await Task.sleep(for: .seconds(3600))
            return NewsSampleData.empty()
        case .tabFailed:
            throw NewsSampleFailure()
        default:
            return NewsSampleData.market()
        }
    }
}

/// The stock screen around the section: the Apple sample every other stock-screen
/// scenario draws.
private struct NewsSampleDetailSource: AssetDetailDataSource {
    func detail(symbol: String) async throws -> AssetDetailDTO {
        MarketSampleData.detail()
    }

    func chart(symbol: String, range: AssetChartRange) async throws -> AssetChartDTO {
        MarketSampleData.chart(range: range)
    }
}

/// No cabal holds it, so the section sits where most members will see it.
private struct NewsSampleSocialSource: AssetSocialDataSource {
    func social(symbol: String) async throws -> AssetSocialDTO {
        AssetSocialSampleData.empty(symbol: symbol)
    }
}

/// The Stocks tab around the section: the popular list, the movers, and the member's
/// cabals, all from the market samples.
private struct NewsSampleStocksSource: StocksTabDataSource {
    func search(query: String, offset: Int, limit: Int) async throws -> ListMarketAssetsResponse {
        ListMarketAssetsResponse(assets: MarketSampleData.popularAssets, hasMore: false, market: MarketSampleData.sessionOpen)
    }

    func popular(limit: Int) async throws -> PopularAssetsResponse {
        PopularAssetsResponse(assets: Array(MarketSampleData.popularAssets.prefix(limit)), market: MarketSampleData.sessionOpen)
    }

    func held() async throws -> HeldAssetsResponse {
        MarketSampleData.heldAssetsResponse(market: MarketSampleData.sessionOpen)
    }
}

private struct NewsSampleFailure: Error {}
#endif
