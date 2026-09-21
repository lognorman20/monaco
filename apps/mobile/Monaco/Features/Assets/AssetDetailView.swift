import MonacoCore
import SwiftUI

/// One stock: what it costs, what it has done, and — below the chart — what the
/// member's cabals are doing about it.
///
/// The screen is a hero, a chart, and an ordered list of section slots. The slots are
/// the extension point: a card goes into `detailSections` in its place in the order
/// and nothing else on this screen moves.
struct AssetDetailView: View {
    @ObservedObject var auth: PrivyAuthService
    let symbol: String

    @State private var model: AssetDetailModel
    /// The member's own cabals against this stock. A separate model because it is a
    /// separate read at a separate cadence: the price is polled every ten seconds,
    /// this changes when somebody votes.
    @State private var social: AssetSocialModel
    @State private var pickerKind: ProposalPickKind?
    @State private var toast: MonacoToast?

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// How often the hero re-reads the price, and how often the drawn range is
    /// re-read. Only the sample harness passes anything but the defaults, so a
    /// scripted price walk does not take a minute to show and a quiet chart re-read
    /// can be watched inside a screenshot run rather than two minutes after one.
    private let pricePollInterval: Duration
    private let chartPollInterval: Duration

    init(
        auth: PrivyAuthService,
        symbol: String,
        dataSource: AssetDetailDataSource? = nil,
        socialDataSource: AssetSocialDataSource? = nil,
        pricePollInterval: Duration = AssetDetailPolling.price,
        chartPollInterval: Duration = AssetDetailPolling.chart
    ) {
        self.auth = auth
        self.symbol = symbol
        self.pricePollInterval = pricePollInterval
        self.chartPollInterval = chartPollInterval
        _model = State(initialValue: AssetDetailModel(
            symbol: symbol,
            dataSource: dataSource ?? LiveAssetDetailDataSource(auth: auth)
        ))
        _social = State(initialValue: AssetSocialModel(
            symbol: symbol,
            dataSource: socialDataSource ?? LiveAssetSocialDataSource(auth: auth)
        ))
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                switch model.detailState {
                case .loading:
                    AssetDetailHeroSkeleton()
                    chartCard
                case .failed:
                    EmptyState(
                        title: "Could not load this stock",
                        actionTitle: "Retry",
                        action: { Task { await model.loadDetail() } }
                    )
                    .accessibilityIdentifier("asset-detail-failed")
                    // The curve was decoupled from this call; throwing away a chart that did
                    // arrive would leave the screen emptier than before they were split.
                    chartCard
                case .loaded:
                    hero
                    // The "can't be bought" line used to live here, above the chart.
                    // It belongs next to the button it disables, which is now in the
                    // trade bar — saying it twice made the screen argue with itself.
                    chartCard
                    detailSections
                }
            }
            .padding(MonacoTheme.Space.m)
        }
        .monacoCanvas()
        .foregroundStyle(MonacoTheme.ink)
        .navigationTitle(AssetSymbolFormatter.display(symbol))
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("asset-detail-root")
        // The bar sits in the safe area, not in the scroll view: this screen is five
        // cards long now, and the action a member came for must not be somewhere
        // they have to scroll to.
        .safeAreaInset(edge: .bottom, spacing: 0) { tradeBar }
        .monacoToast($toast)
        // Three independent loads: the curve does not wait on the (slow) detail call,
        // and neither waits on the per-cabal read behind the social cards.
        .task { await model.loadDetail() }
        .task(id: model.range) { await model.loadChart(range: model.range) }
        .task { await social.load() }
        // The hero keeps itself current while the member is looking at it. Both loops
        // are silent: a tick that fails leaves the screen exactly as they last saw it.
        .pollWhileVisible(every: pricePollInterval) { await model.refreshDetail() }
        .pollWhileVisible(every: chartPollInterval) { await model.refreshChart() }
        .onChange(of: model.sessionExpired) { _, expired in
            if expired { Task { await auth.logout() } }
        }
        .onChange(of: social.sessionExpired) { _, expired in
            if expired { Task { await auth.logout() } }
        }
        .navigationDestination(item: $pickerKind) { kind in
            GroupPickerForProposalView(
                auth: auth,
                symbol: symbol,
                kind: kind,
                stock: proposeStock,
                onProposed: { cabalName in
                    pickerKind = nil
                    Haptics.success()
                    toast = MonacoToast(message: ProposeFlowCopy.proposalSent(cabalName), isSuccess: true)
                    // A new proposal is exactly the thing the position card counts.
                    Task { await social.refresh() }
                }
            )
        }
        .monacoFrameStats("AssetDetail")
    }

    /// Everything the propose flow needs, so it never refetches what this screen already showed.
    private var proposeStock: ProposeStock {
        guard let detail = model.detail else { return ProposeStock(symbol: symbol) }
        return ProposeStock(
            symbol: detail.symbol,
            name: ProposeStock.displayName(symbol: detail.symbol, catalogName: detail.name),
            priceMicros: detail.priceUsdcMicros,
            change24h: detail.change24h,
            isTradable: detail.liquidity.routable
        )
    }

    private var hero: some View {
        AssetDetailHero(
            // Same resolver as the list rows, so one stock never carries two names.
            displayName: ProposeStock.displayName(
                symbol: model.detail?.symbol ?? symbol,
                catalogName: model.detail?.name ?? ""
            ),
            priceUsdcMicros: model.heroPriceUsdcMicros,
            move: model.move,
            isScrubbing: model.isScrubbing,
            tick: model.heroTick,
            session: model.sessionChip
        )
    }

    private var chartCard: some View {
        AssetChartCard(model: model, isMarketLive: model.isMarketLive)
    }

    // MARK: - Section slots
    //
    // Everything below the chart, in the order it appears. Each slot is one view;
    // adding a card means adding it here, in its place, and nothing above or below
    // has to move. The order is the product's, not the implementation's: what the
    // member's own cabals are doing comes before what the market says about the
    // stock, and the disclosure comes last.
    //
    //   1. Your cabals' position — holdings, P&L, open votes            (#341)
    //   2. Stats grid — open/high/low, 52-week range, trading cost      (#342)
    //   3. Stock vs token — NASDAQ against the xStock, premium          (#347)
    //   4. About — what the token is, and the tracker disclosure        (#343)
    //   5. Activity on this stock — proposals, fills and comments       (#344)
    //
    // The trade bar (#340) is not a slot: it belongs in a `safeAreaInset`, not in
    // this stack.
    // No stack around the slots while they are all empty. The `EmptyView`s collapse
    // but a `VStack` holding them does not: it is still a child of the outer stack,
    // so the screen would carry a stray 24pt gap between the chart and the action row
    // until the first card lands. Each slot brings its own spacing from the outer
    // stack when it arrives.
    //
    // Do not put an accessibility identifier on a stack of cards either. A modifier
    // on a VStack is applied to each of its children, so naming the stack renames
    // every card inside it and makes each one unfindable by its own name — which is
    // exactly what it did to the chart before this was noticed.
    @ViewBuilder
    private var detailSections: some View {
        // 1. Your cabals' position — holdings, P&L, open votes            (#341)
        if let summary = social.summary {
            AssetPositionCard(
                summary: summary,
                proposals: social.openProposals,
                symbol: symbol
            )
        }
        // 2. Stats grid — open/high/low, 52-week range, trading cost      (#342)
        if let grid = AssetStatsGrid.make(model.detail?.stats, currentUsdcMicros: model.detail?.priceUsdcMicros) {
            AssetStatsCard(grid: grid)
        }
        // 3. Stock vs token — NASDAQ against the xStock, premium          (#347)
        if let card = stockVsTokenCard {
            StockVsTokenCardView(card: card)
        }
        // 4. About — what the token is, and the tracker disclosure        (#343)
        if let detail = model.detail {
            AssetAboutCard(about: AssetAboutCopy.make(
                symbol: detail.symbol,
                name: detail.name,
                solanaMint: detail.solanaMint,
                liquidityLabel: detail.liquidity.label
            ))
        }
        // 5. Activity on this stock — proposals, fills and comments       (#344)
        if !social.activity.isEmpty {
            AssetActivityCard(symbol: symbol, activity: social.activity)
        }
    }

    /// The Pyth comparison, built from the same detail payload the hero reads. Nil
    /// when the backend sent no comparison, or when neither leg carries a price.
    private var stockVsTokenCard: StockVsTokenCard? {
        guard let detail = model.detail else { return nil }
        return StockVsTokenCard.make(
            symbol: detail.symbol,
            name: ProposeStock.displayName(symbol: detail.symbol, catalogName: detail.name),
            quotes: detail.stockVsToken,
            market: model.marketStatus
        )
    }

    /// The sticky bar. It replaces the inline action row, which sat below five cards
    /// and offered a Sell that could walk into "This cabal does not hold this stock."
    private var tradeBar: some View {
        AssetTradeBar(
            state: AssetTradeBarState.make(
                isRoutable: model.canBuy,
                liquidityLabel: model.detail?.liquidity.label,
                holdings: social.holdings,
                hasLoadedHoldings: social.hasAnswered
            ),
            onBuy: { pickerKind = .buy },
            onSell: { pickerKind = .sell }
        )
        // The sell button arrives with the holdings answer; a fade reads as an answer
        // landing rather than as the layout jumping.
        .animation(reduceMotion ? nil : .easeInOut(duration: 0.2), value: social.hasAnswered)
    }
}
