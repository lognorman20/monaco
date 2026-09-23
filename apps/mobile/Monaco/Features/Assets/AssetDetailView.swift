import MonacoCore
import SwiftUI

/// One stock: what it costs, what it has done, and — below the chart — what the
/// member's cabals are doing about it.
///
/// The screen is a hero, a chart, and an ordered list of section slots. The slots are
/// the extension point: a card goes into `detailSections` in its place in the order
/// and nothing else on this screen moves.
struct AssetDetailView: View {
    @ObservedObject var auth: DynamicAuthService
    let symbol: String

    @State private var model: AssetDetailModel
    /// The member's own cabals against this stock. A separate model because it is a
    /// separate read at a separate cadence: the price is polled every ten seconds,
    /// this changes when somebody votes.
    @State private var social: AssetSocialModel
    /// The one screen this one is pushing, if any. See `AssetDetailRoute`.
    @State private var route: AssetDetailRoute?
    @State private var toast: MonacoToast?
    private let sources: StocksFlowSources

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// How often the hero re-reads the price, and how often the drawn range is
    /// re-read. Only the sample harness passes anything but the defaults, so a
    /// scripted price walk does not take a minute to show and a quiet chart re-read
    /// can be watched inside a screenshot run rather than two minutes after one.
    private let pricePollInterval: Duration
    private let chartPollInterval: Duration

    init(
        auth: DynamicAuthService,
        symbol: String,
        sources: StocksFlowSources = .live,
        pricePollInterval: Duration = AssetDetailPolling.price,
        chartPollInterval: Duration = AssetDetailPolling.chart
    ) {
        self.auth = auth
        self.symbol = symbol
        self.sources = sources
        self.pricePollInterval = pricePollInterval
        self.chartPollInterval = chartPollInterval
        _model = State(initialValue: AssetDetailModel(
            symbol: symbol,
            dataSource: sources.detail ?? LiveAssetDetailDataSource(auth: auth)
        ))
        _social = State(initialValue: AssetSocialModel(
            symbol: symbol,
            dataSource: sources.social ?? LiveAssetSocialDataSource(auth: auth)
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
        .navigationTitle(model.detail?.displayTicker ?? AssetSymbolFormatter.display(symbol))
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("asset-detail-root")
        // The bar sits in the safe area, not in the scroll view: this screen is five
        // cards long now, and the action a member came for must not be somewhere
        // they have to scroll to.
        .safeAreaInset(edge: .bottom, spacing: 0) { tradeBar }
        .monacoToast($toast)
        // Three independent loads: the curve does not wait on the (slow) detail call,
        // and neither waits on the per-cabal read behind the social cards. All of them
        // are keyed on who is signed in, not on the access token, so Dynamic's token
        // rotation does not reload the screen, but a new sign-in starts clean.
        .task(id: auth.sessionIdentity) {
            model.beginSession()
            await model.loadDetail()
        }
        .task(id: model.range) { _ = await model.loadChart(range: model.range) }
        .task(id: auth.sessionIdentity) {
            social.beginSession()
            await social.load()
        }
        // The hero keeps itself current while the member is looking at it. Both loops
        // are silent: a tick that fails leaves the screen exactly as they last saw it,
        // and only backs the loop off.
        .pollWhileVisible(every: pricePollInterval) { try await model.refreshDetail() }
        .pollWhileVisible(every: chartPollInterval) { try await model.refreshChart() }
        .onChange(of: model.rejectedSession) { _, rejected in
            guard let rejected else { return }
            Task { await auth.signOutAfterRejectedSession(rejectedToken: rejected.token) }
        }
        .onChange(of: social.rejectedSession) { _, rejected in
            guard let rejected else { return }
            Task { await auth.signOutAfterRejectedSession(rejectedToken: rejected.token) }
        }
        .navigationDestination(item: $route) { route in
            destination(for: route)
        }
        .monacoFrameStats("AssetDetail")
    }

    /// The screen behind a route. Declared once, so no card ever builds a destination.
    @ViewBuilder
    private func destination(for route: AssetDetailRoute) -> some View {
        switch route {
        case .propose(let kind):
            GroupPickerForProposalView(
                auth: auth,
                symbol: symbol,
                kind: kind,
                stock: proposeStock,
                onProposed: { cabalName in
                    self.route = nil
                    Haptics.success()
                    toast = MonacoToast(message: ProposeFlowCopy.proposalSent(cabalName), isSuccess: true)
                    // A new proposal is exactly the thing the position card counts.
                    Task { await social.refresh() }
                },
                service: sources.propose,
                holdingsDataSource: sources.holdings
            )
        case let .cabal(id, name):
            GroupDetailView(auth: auth, groupId: id, groupName: name)
        case .proposal(let id):
            ProposalDetailView(auth: auth, proposalId: id)
        }
    }

    /// The cabal's name as one of the cards already knows it, so the pushed screen
    /// carries its title from the first frame instead of saying "Cabal" until it
    /// loads. Nil is a perfectly good answer; the cabal screen loads its own name.
    private func cabalName(_ groupId: String) -> String? {
        if let holding = social.holdings.first(where: { $0.groupId == groupId }) {
            return holding.name.isEmpty ? nil : holding.name
        }
        if let proposal = social.openProposals.first(where: { $0.groupId == groupId }) {
            return proposal.groupName.isEmpty ? nil : proposal.groupName
        }
        if let item = social.activity.first(where: { $0.groupId == groupId }) {
            return item.groupName.isEmpty ? nil : item.groupName
        }
        return nil
    }

    /// Everything the propose flow needs, so it never refetches what this screen already showed.
    private var proposeStock: ProposeStock {
        guard let detail = model.detail else { return ProposeStock(symbol: symbol) }
        return ProposeStock(
            symbol: detail.symbol,
            name: ProposeStock.displayName(symbol: detail.symbol, catalogName: detail.name),
            priceMicros: detail.priceUsdcMicros,
            isTradable: detail.routable
        )
    }

    private var hero: some View {
        AssetDetailHero(
            // Same resolver as the list rows, so one stock never carries two names.
            displayName: ProposeStock.displayName(
                symbol: model.detail?.symbol ?? symbol,
                catalogName: model.detail?.name ?? ""
            ),
            priceCaption: model.heroPriceCaption,
            priceUsdcMicros: model.heroPriceUsdcMicros,
            move: model.move,
            isScrubbing: model.isScrubbing,
            tick: model.heroTick,
            priceAsOf: model.heroPriceAsOf,
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
    //   1. Your cabals' position: holdings, P&L, open votes
    //   2. Stats grid: the share's open/high/low and 52-week range, from Pyth
    //   3. Stock vs token: the B20 token's Kyber mid against its Chainlink mark,
    //      with the share's Pyth price as a separate reference line
    //   4. About: what the B20 token is, and the tracker disclosure
    //   5. Activity on this stock: proposals, fills and comments
    //
    // The trade bar is not a slot: it belongs in a `safeAreaInset`, not in this stack.
    //
    // No stack around the slots while they are all empty. The `EmptyView`s collapse
    // but a `VStack` holding them does not: it is still a child of the outer stack,
    // so the screen would carry a stray 24pt gap between the chart and the action row
    // until the first card lands. Each slot brings its own spacing from the outer
    // stack when it arrives.
    //
    // Do not put an accessibility identifier on a stack of cards either. A modifier
    // on a VStack is applied to each of its children, so naming the stack renames
    // every card inside it and makes each one unfindable by its own name.
    @ViewBuilder
    private var detailSections: some View {
        // 1. Your cabals' position
        if let summary = social.summary {
            AssetPositionCard(
                summary: summary,
                proposals: social.openProposals,
                symbol: symbol,
                // A holding row opens the cabal, a vote row opens the vote. Without
                // these the rows fall into their non-Button branch and the card is a
                // picture of a position rather than a way into one.
                openCabal: { route = .cabal(id: $0, name: cabalName($0)) },
                openProposal: { route = .proposal(id: $0.id) }
            )
        } else if social.hasFailed {
            // A read that failed is not an answer of "nobody holds this". Say so, and
            // offer the way back — the same retry restores the activity card below.
            AssetSocialFailedCard(symbol: symbol, retry: { Task { await social.load() } })
        }
        // 2. Stats grid. The 52-week bar places the *share's* own price: the range is
        // the share's, and the hero's total-return mark would drift up the bar with
        // every reinvested dividend even on a share that had not moved.
        if let grid = AssetStatsGrid.make(model.detail?.stats, currentUsdcMicros: equityPriceUsdcMicros) {
            AssetStatsCard(grid: grid)
        }
        // 3. Stock vs token
        if let card = stockVsTokenCard {
            StockVsTokenCardView(card: card)
        }
        // 4. About
        if let detail = model.detail {
            AssetAboutCard(about: AssetAboutCopy.make(
                symbol: detail.symbol,
                name: detail.name,
                tokenAddress: detail.tokenAddress,
                liquidityLabel: detail.liquidity.label
            ))
        }
        // 5. Activity on this stock
        if !social.activity.isEmpty {
            AssetActivityCard(
                symbol: symbol,
                activity: social.activity,
                openCabal: { route = .cabal(id: $0, name: cabalName($0)) }
            )
        }
    }

    /// The underlying share's own price, when the backend sent one. The stats cells
    /// are all the share's, so this is the figure that belongs on the 52-week bar.
    /// Nil when Pyth could not be read: the bar is then left off rather than placed
    /// from a price in the other unit.
    private var equityPriceUsdcMicros: Int64? {
        guard let equity = model.detail?.stockVsToken?.equity, equity.isPriced else { return nil }
        return equity.priceUsdcMicros
    }

    /// The comparison, built from the same detail payload the hero reads. Nil when
    /// the backend sent none, or when no line carries a price.
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
                holdingsState: social.state
            ),
            onBuy: { route = .propose(.buy) },
            onSell: { route = .propose(.sell) }
        )
        // The sell button arrives with the holdings answer; a fade reads as an answer
        // landing rather than as the layout jumping.
        .animation(reduceMotion ? nil : .easeInOut(duration: 0.2), value: social.state)
    }
}
