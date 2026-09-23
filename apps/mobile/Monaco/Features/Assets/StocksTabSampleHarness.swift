#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only Stocks flow on fixed sample data (launch argument `-MonacoStocksTabSample`,
/// optionally followed by a `StocksTabSampleScenario` name for the browse sections).
///
/// No sign-in and no backend: the Stocks tab, the stock detail, the cabal picker and the amount
/// step all read from `StocksTabSampleData`, so `StocksTabSampleUITests` and its screenshots are
/// reproducible. Three joined cabals: one holds Apple, one holds only cash, and one never answers,
/// which is the partial answer the picker has to be honest about.
enum StocksTabSampleData {
    static let launchArgument = "-MonacoStocksTabSample"

    static var isEnabled: Bool {
        ProcessInfo.processInfo.arguments.contains(launchArgument)
    }

    static let holderId = "5a0c7d10-0001-4b1e-8f00-000000000001"
    static let cashOnlyId = "5a0c7d10-0002-4b1e-8f00-000000000002"
    static let silentId = "5a0c7d10-0003-4b1e-8f00-000000000003"

    static let home = HomeViewDTO(
        groups: [
            HomeGroupBoardRowDTO(groupId: holderId, name: "Weekend investors", potValueUsd: "548.20", percentReturn: "0.0964", dollarPnl: "+48.20", isJoined: true),
            HomeGroupBoardRowDTO(groupId: cashOnlyId, name: "Rent money", potValueUsd: "212.40", percentReturn: "-0.0345", dollarPnl: "-7.60", isJoined: true),
            HomeGroupBoardRowDTO(groupId: silentId, name: "Apple heads", potValueUsd: "91.35", percentReturn: "0.015", dollarPnl: "+1.35", isJoined: true),
        ],
        people: []
    )

    /// The state the four browse sections are in: `-MonacoStocksTabSample <scenario>`, or
    /// `full` when the flag stands alone.
    static var scenario: StocksTabSampleScenario {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument), arguments.indices.contains(flag + 1) else { return .full }
        return StocksTabSampleScenario(rawValue: arguments[flag + 1]) ?? .full
    }

    /// The pinned B20 rows in the production shape: Chainlink price per token, and the
    /// underlying's day move and sparkline, both labelled. See `MarketSampleData`.
    static let catalog: [MarketAssetDTO] = MarketSampleData.popularAssets

    static func groupView(_ groupId: String) -> GroupViewDTO? {
        switch groupId {
        case holderId:
            return view(id: holderId, name: "Weekend investors", total: "548.20", pot: [
                PotRowDTO(symbol: "AAPLc", units: "1.2034", markUsd: "231.40", valueUsd: "278.47", dollarPnl: "+28.47", afterHours: false, tokenAmount: "120340000"),
                PotRowDTO(symbol: "NVDAc", units: "1.05", markUsd: "178.20", valueUsd: "187.11", dollarPnl: "+22.11", afterHours: false, tokenAmount: "105000000"),
                PotRowDTO(symbol: "USDC", units: "82.62", markUsd: "1.00", valueUsd: "82.62", dollarPnl: "+0.00", afterHours: nil, tokenAmount: nil),
            ])
        case cashOnlyId:
            return view(id: cashOnlyId, name: "Rent money", total: "212.40", pot: [
                PotRowDTO(symbol: "USDC", units: "212.40", markUsd: "1.00", valueUsd: "212.40", dollarPnl: "+0.00", afterHours: nil, tokenAmount: nil),
            ])
        default:
            return nil
        }
    }

    private static func view(id: String, name: String, total: String, pot: [PotRowDTO]) -> GroupViewDTO {
        GroupViewDTO(
            id: id,
            name: name,
            treasuryAddress: nil,
            potTotalUsd: total,
            pot: pot,
            you: MemberSliceDTO(shareUnits: "100000000", equityUsd: "100.00", slicePercent: "0.2", dollarPnl: "+0.00", percentReturn: nil),
            members: [],
            proposals: nil,
            agent: nil
        )
    }

    /// 1D rises, 1W falls, 1M ends where it started, so each header verdict has a fixture.
    static func chart(range: AssetChartRange, priceMicros: Int64) -> AssetChartDTO {
        let end: Int64 = 1_790_000_000
        let (step, deltas): (Int64, [Int64]) = {
            switch range {
            case .oneDay: return (3_600, [-4_000_000, -2_500_000, -3_000_000, -1_000_000, 0])
            case .oneWeek: return (86_400, [9_000_000, 7_000_000, 8_000_000, 3_000_000, 0])
            case .oneMonth: return (6 * 86_400, [0, 5_000_000, -3_000_000, 2_000_000, 0])
            case .threeMonths: return (18 * 86_400, [-12_000_000, -6_000_000, -9_000_000, -2_000_000, 0])
            case .oneYear: return (73 * 86_400, [-40_000_000, -25_000_000, -30_000_000, -8_000_000, 0])
            case .all: return (365 * 86_400, [-120_000_000, -90_000_000, -60_000_000, -20_000_000, 0])
            }
        }()
        let points = deltas.enumerated().map { index, delta in
            AssetChartPointDTO(
                timestamp: end - Int64(deltas.count - 1 - index) * step,
                priceUsdcMicros: priceMicros + delta
            )
        }
        return AssetChartDTO(points: points, emptyReason: nil)
    }

    struct SampleError: Error {}

    /// Canned answers for the three reads the tab makes. `loading` never returns, which is
    /// how the skeleton is screenshotted.
    final class TabSource: StocksTabDataSource {
        let scenario: StocksTabSampleScenario
        /// `cabalsStale` answers once and then stops, which is what a refresh failing over
        /// rows already on screen looks like.
        private var heldReads = 0

        init(scenario: StocksTabSampleScenario = .full) {
            self.scenario = scenario
        }

        private var market: MarketStatusDTO {
            scenario == .afterHours ? MarketSampleData.sessionAfterHours : MarketSampleData.sessionOpen
        }

        private var assets: [MarketAssetDTO] {
            guard scenario == .noSeries else { return catalog }
            return catalog.map { asset in
                MarketAssetDTO(
                    symbol: asset.symbol,
                    name: asset.name,
                    tokenAddress: asset.tokenAddress,
                    routable: asset.routable,
                    priceUsdcMicros: asset.priceUsdcMicros,
                    change24h: asset.change24h,
                    change24hBasis: asset.change24hBasis,
                    change24hBasisSymbol: asset.change24hBasisSymbol
                )
            }
        }

        private func hangIfLoading() async throws {
            if scenario == .loading { try await Task.sleep(for: .seconds(3600)) }
        }

        func search(query: String, offset: Int, limit: Int) async throws -> ListMarketAssetsResponse {
            try await Task.sleep(for: .milliseconds(150))
            try await hangIfLoading()
            let term = query.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
            let matches = assets.filter {
                $0.name.lowercased().contains(term) || AssetSymbolFormatter.display($0.symbol).lowercased().contains(term)
            }
            let page = Array(matches.dropFirst(offset).prefix(limit))
            return ListMarketAssetsResponse(assets: page, hasMore: offset + page.count < matches.count, market: market)
        }

        func popular(limit: Int) async throws -> PopularAssetsResponse {
            try await Task.sleep(for: .milliseconds(150))
            try await hangIfLoading()
            if scenario == .popularFailed { throw SampleError() }
            return PopularAssetsResponse(assets: Array(assets.prefix(limit)), market: market)
        }

        func held() async throws -> HeldAssetsResponse {
            try await Task.sleep(for: .milliseconds(150))
            try await hangIfLoading()
            switch scenario {
            case .cabalsFailed:
                throw SampleError()
            case .cabalsStale:
                heldReads += 1
                if heldReads > 1 { throw SampleError() }
                return MarketSampleData.heldAssetsResponse(market: market)
            case .noCabals, .popularFailed:
                return HeldAssetsResponse(held: [], upForVote: [], market: market)
            case .full, .loading, .noSeries, .afterHours:
                return MarketSampleData.heldAssetsResponse(market: market)
            }
        }
    }

    struct DetailSource: AssetDetailDataSource {
        func detail(symbol: String) async throws -> AssetDetailDTO {
            // Slower than the chart on purpose: the curve must not wait for it.
            try await Task.sleep(for: .milliseconds(600))
            guard let asset = catalog.first(where: { $0.symbol == symbol }) else { throw SampleError() }
            return AssetDetailDTO(
                symbol: asset.symbol,
                name: asset.name,
                tokenAddress: asset.tokenAddress,
                routable: true,
                priceUsdcMicros: asset.priceUsdcMicros,
                change24h: asset.change24h,
                change24hBasis: asset.change24hBasis,
                change24hBasisSymbol: asset.change24hBasisSymbol,
                liquidity: AssetLiquidityDTO(
                    label: "Via DEX",
                    routable: true,
                    buyProbeUsdcMicros: 1_000_000,
                    buyProbeOutAmount: nil,
                    sellProbeInAmount: nil,
                    sellProbeOutAmount: nil,
                    spreadBps: nil
                )
            )
        }

        func chart(symbol: String, range: AssetChartRange) async throws -> AssetChartDTO {
            try await Task.sleep(for: .milliseconds(150))
            let price = catalog.first(where: { $0.symbol == symbol })?.priceUsdcMicros ?? 100_000_000
            return StocksTabSampleData.chart(range: range, priceMicros: price)
        }
    }

    struct HoldingsSource: CabalHoldingsDataSource {
        func groupView(groupId: String) async throws -> GroupViewDTO {
            try await Task.sleep(for: .milliseconds(200))
            guard let view = StocksTabSampleData.groupView(groupId) else { throw SampleError() }
            return view
        }
    }

    final class ProposeSource: ProposeService {
        func pot(groupId: String) async throws -> ProposePot {
            guard let view = StocksTabSampleData.groupView(groupId) else { throw SampleError() }
            return ProposePot(view: view)
        }

        func popularStocks() async throws -> [ProposeStock] {
            catalog.map(ProposeStock.init(market:))
        }

        func searchStocks(groupId: String, query: String, offset: Int, limit: Int) async throws -> (stocks: [ProposeStock], hasMore: Bool) {
            let page = try await TabSource().search(query: query, offset: offset, limit: limit)
            return (page.assets.map(ProposeStock.init(market:)), page.hasMore)
        }

        func priceMicros(symbol: String) async throws -> Int64? {
            catalog.first { $0.symbol == symbol }?.priceUsdcMicros
        }

        func buyQuote(groupId: String, symbol: String, usdcMicros: Int64) async throws -> BuyQuoteDTO {
            try await Task.sleep(for: .milliseconds(300))
            let price = catalog.first { $0.symbol == symbol }?.priceUsdcMicros ?? 100_000_000
            let atomics = Self.mulDiv(usdcMicros, 100_000_000, price)
            return BuyQuoteDTO(
                symbol: symbol, kind: "buy", usdcMicros: String(usdcMicros), tokenAmount: nil,
                routable: true, outputAmount: String(atomics), outputUsdcMicros: nil, priceUsdcMicros: String(price)
            )
        }

        /// `a * b / c` rounded down, without the Int64 overflow the plain product hits past
        /// about $92k of input.
        private static func mulDiv(_ a: Int64, _ b: Int64, _ c: Int64) -> Int64 {
            let (high, low) = a.multipliedFullWidth(by: b)
            return c.dividingFullWidth((high, low)).quotient
        }

        func sellQuote(groupId: String, symbol: String, tokenAmount: Int64) async throws -> BuyQuoteDTO {
            try await Task.sleep(for: .milliseconds(300))
            let price = catalog.first { $0.symbol == symbol }?.priceUsdcMicros ?? 100_000_000
            let usdc = Self.mulDiv(tokenAmount, price, 100_000_000)
            return BuyQuoteDTO(
                symbol: symbol, kind: "sell", usdcMicros: nil, tokenAmount: String(tokenAmount),
                routable: true, outputAmount: String(usdc), outputUsdcMicros: String(usdc), priceUsdcMicros: nil
            )
        }

        func propose(groupId: String, draft: ProposalDraft) async throws -> String {
            try await Task.sleep(for: .milliseconds(300))
            return UUID().uuidString
        }
    }

    static func sources(scenario: StocksTabSampleScenario) -> StocksFlowSources {
        StocksFlowSources(tab: TabSource(scenario: scenario), detail: DetailSource(), holdings: HoldingsSource(), propose: ProposeSource())
    }
}

/// Every state the four browse sections can be in, so each one can be screenshotted.
enum StocksTabSampleScenario: String, CaseIterable {
    /// All four sections, with the awkward rows in them: a faller, a stock that did not
    /// move, one with no day move and one with no series.
    case full
    /// Signed in, in no cabals, or in cabals that have bought nothing.
    case noCabals
    /// The cabal read failed; the catalogue did not. The market stays live.
    case cabalsFailed
    /// The cabal sections loaded once and then a refresh failed. The rows stay on screen
    /// (they are the member's own money) with the caption that says they are not fresh.
    case cabalsStale
    /// The catalogue would not load. Nothing else can be shown.
    case popularFailed
    /// Nothing has answered yet: the skeleton.
    case loading
    /// A catalogue with no day series at all: rows without sparklines.
    case noSeries
    /// After the bell: the rows carry the moon.
    case afterHours
}

struct StocksTabSampleHarness: View {
    @ObservedObject var auth: DynamicAuthService
    @State private var session: AppSessionStore = {
        let session = AppSessionStore()
        session.home = StocksTabSampleData.home
        session.isLoading = false
        return session
    }()
    private let scenario: StocksTabSampleScenario
    @State private var sources: StocksFlowSources
    @State private var model: StocksTabModel
    @State private var isPrepared = false

    init(auth: DynamicAuthService, scenario: StocksTabSampleScenario = StocksTabSampleData.scenario) {
        self.auth = auth
        self.scenario = scenario
        let sources = StocksTabSampleData.sources(scenario: scenario)
        _sources = State(initialValue: sources)
        _model = State(initialValue: StocksTabModel(dataSource: sources.tab ?? StocksTabSampleData.TabSource(scenario: scenario)))
    }

    var body: some View {
        TabView {
            NavigationStack {
                if isPrepared {
                    AssetsTabView(auth: auth, sources: sources, model: model)
                } else {
                    Color.clear
                }
            }
            .task {
                await prepare()
                isPrepared = true
            }
            .tabItem {
                Label("Stocks", systemImage: "chart.line.uptrend.xyaxis")
                    .accessibilityIdentifier("tab-stocks")
            }
        }
        .tint(MonacoTheme.ink)
        .environment(session)
    }

    /// Most scenarios are one canned answer per read. `cabalsStale` is two: the cabal
    /// sections have to land before a refresh can fail on top of them, and that
    /// sequence is the state worth screenshotting.
    private func prepare() async {
        guard scenario == .cabalsStale else { return }
        await model.loadSocial()
        await model.loadSocial()
    }
}
#endif
