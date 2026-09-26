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
    // lane: news
    /// Headlines about the stock. Its own read, loaded beside the social one so the hero
    /// and the chart never wait on a feed.
    @State private var news: NewsFeedModel
    /// The one screen this one is pushing, if any. See `AssetDetailRoute`.
    @State private var route: AssetDetailRoute?
    @State private var toast: MonacoToast?
    // lane: watchlist
    @State private var watch: AssetWatchModel
    @State private var showsAlertSheet = false
    private let alertSource: PriceAlertDataSource
    private let alertSheetPrefill: PriceAlertPrefill?

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// How often the hero re-reads the price, and how often the drawn range is
    /// re-read. Only the sample harness passes anything but the defaults, so a
    /// scripted price walk does not take a minute to show and a quiet chart re-read
    /// can be watched inside a screenshot run rather than two minutes after one.
    private let pricePollInterval: Duration
    private let chartPollInterval: Duration
    /// A sample to hold selected once the curve lands, as if a finger were resting on it.
    /// Only the sample harness passes one: the scrubbed hero is a gesture, and a
    /// screenshot run cannot make gestures.
    private let scrubbedIndexOnLoad: Int?

    init(
        auth: PrivyAuthService,
        symbol: String,
        dataSource: AssetDetailDataSource? = nil,
        socialDataSource: AssetSocialDataSource? = nil,
        // lane: news
        newsDataSource: NewsDataSource? = nil,
        pricePollInterval: Duration = AssetDetailPolling.price,
        chartPollInterval: Duration = AssetDetailPolling.chart,
        scrubbedIndexOnLoad: Int? = nil,
        // lane: watchlist
        watchDataSource: (any WatchlistDataSource & PriceAlertDataSource)? = nil,
        alertSheetPrefill: PriceAlertPrefill? = nil
    ) {
        self.auth = auth
        self.symbol = symbol
        self.pricePollInterval = pricePollInterval
        self.chartPollInterval = chartPollInterval
        self.scrubbedIndexOnLoad = scrubbedIndexOnLoad
        _model = State(initialValue: AssetDetailModel(
            symbol: symbol,
            dataSource: dataSource ?? LiveAssetDetailDataSource(auth: auth)
        ))
        _social = State(initialValue: AssetSocialModel(
            symbol: symbol,
            dataSource: socialDataSource ?? LiveAssetSocialDataSource(auth: auth)
        ))
        // lane: news
        let newsSource: NewsDataSource = newsDataSource ?? LiveNewsDataSource(auth: auth)
        _news = State(initialValue: NewsFeedModel(fetch: { try await newsSource.assetNews(symbol: symbol) }))
        // lane: watchlist
        let watchSource: any WatchlistDataSource & PriceAlertDataSource = watchDataSource ?? LiveWatchlistDataSource(auth: auth)
        _watch = State(initialValue: AssetWatchModel(symbol: symbol, dataSource: watchSource))
        alertSource = watchSource
        self.alertSheetPrefill = alertSheetPrefill
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                switch model.detailState {
                case .loading:
                    AssetDetailHeroSkeleton()
                        .padding(.horizontal, MonacoTheme.Space.m)
                    chartCard
                    // The screen keeps its shape below the chart too: one ruled section
                    // where the first section will land, rather than bare paper.
                    AssetDetailSectionSkeleton()
                        .padding(.top, MonacoTheme.Space.s)
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
                    // lane: watchlist
                    PriceAlertButton(alertCount: watch.alertCount) { showsAlertSheet = true }
                        .padding(.horizontal, MonacoTheme.Space.m)
                        .padding(.top, -MonacoTheme.Space.sm)
                    // The "can't be bought" line used to live here, above the chart.
                    // It belongs next to the button it disables, which is now in the
                    // trade bar — saying it twice made the screen argue with itself.
                    chartCard
                    detailSections
                        // 32pt from the range chips to the first section's rule: the
                        // same break as between two sections, whose last lines keep
                        // their own padding and the chips do not.
                        .padding(.top, MonacoTheme.Space.s)
                }
            }
            // No horizontal padding on the stack: the curve runs edge to edge, and the hero
            // and every section inset themselves.
            .padding(.top, MonacoTheme.Space.m)
            // The last section ends a full step above the trade bar rather than 16pt from
            // its rule, where the bar's hairline read as the section's own last line.
            .padding(.bottom, MonacoTheme.Space.xl)
        }

        .monacoCanvas()
        .foregroundStyle(MonacoTheme.ink)
        .navigationTitle(AssetSymbolFormatter.display(symbol, kind: model.detail?.resolvedKind ?? .stock))
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("asset-detail-root")
        // The bar sits in the safe area, not in the scroll view: this screen is five
        // cards long now, and the action a member came for must not be somewhere
        // they have to scroll to.
        .safeAreaInset(edge: .bottom, spacing: 0) { tradeBar }
        .monacoToast($toast)
        // lane: watchlist
        .assetWatchChrome(
            watch: watch,
            detail: model.detail,
            name: heroDisplayName,
            alerts: alertSource,
            showsAlertSheet: $showsAlertSheet,
            prefill: alertSheetPrefill,
            toast: $toast
        )
        // Three independent loads: the curve does not wait on the (slow) detail call,
        // and neither waits on the per-cabal read behind the social cards.
        .task { await model.loadDetail() }
        .task(id: model.range) {
            await model.loadChart(range: model.range)
            holdScrubIfAsked()
        }
        .task { await social.load() }
        // lane: news
        .task { await news.load() }
        .pollWhileVisible(every: NewsRefresh.interval) { await news.refresh() }
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
        .navigationDestination(item: $route) { route in
            destination(for: route)
        }
        .monacoFrameStats("AssetDetail")
    }

    /// See `scrubbedIndexOnLoad`. Never overrides a scrub a finger has already made.
    private func holdScrubIfAsked() {
        guard let scrubbedIndexOnLoad, model.scrubbedIndex == nil,
              let count = model.series?.points.count, count > 0
        else { return }
        model.scrubbedIndex = min(max(scrubbedIndexOnLoad, 0), count - 1)
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
                }
            )
        case let .cabal(id, name):
            GroupDetailView(auth: auth, groupId: id, groupName: name)
        case .variant(let variantSymbol):
            AssetDetailView(auth: auth, symbol: variantSymbol)
        case .proposal(let id):
            ProposalDetailView(auth: auth, proposalId: id)
        // lane: news
        case .news:
            AssetNewsView(model: news, companyName: heroDisplayName)
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
            change24h: detail.change24h,
            isTradable: detail.liquidity.routable,
            assetKind: detail.resolvedKind,
            tokenDecimals: detail.resolvedDecimals
        )
    }

    private var isPreIpo: Bool { model.detail?.resolvedKind == .preIpo }

    /// Same resolver as the list rows, so one stock never carries two names. A pre-IPO
    /// token has no ticker convention, so it goes by the catalogue's name.
    private var heroDisplayName: String {
        if isPreIpo, let detail = model.detail {
            return AssetCatalogDisplayName.format(catalogName: detail.name, symbol: detail.symbol, kind: .preIpo)
        }
        return ProposeStock.displayName(
            symbol: model.detail?.symbol ?? symbol,
            catalogName: model.detail?.name ?? ""
        )
    }

    private var hero: some View {
        AssetDetailHero(
            // Same resolver as the list rows, so one stock never carries two names.
            displayName: heroDisplayName,
            ticker: AssetSymbolFormatter.display(symbol),
            priceUsdcMicros: model.heroPriceUsdcMicros,
            move: model.move,
            isScrubbing: model.isScrubbing,
            tick: model.heroTick,
            session: isPreIpo ? nil : model.sessionChip
        )
        .padding(.horizontal, MonacoTheme.Space.m)
    }


    private var chartCard: some View {
        AssetChartCard(model: model, isMarketLive: model.isMarketLive)
    }

    // MARK: - Section slots
    //
    // Everything below the chart, in the order it appears. Each slot is one view;
    // adding a section means adding it here, in its place, and nothing above or below
    // has to move. The order is the product's, not the implementation's: what the
    // member's own cabals are doing comes before what the market says about the
    // stock, and the disclosure comes last.
    //
    //   1. Your cabals' position — holdings, P&L, open votes            (#341)
    //   2. Stats — open/high/low, 52-week range, trading cost           (#342)
    //   3. Stock vs token — NASDAQ against the xStock, premium          (#347)
    //   3b. News — the newest three headlines, "See all" for the rest   (lane: news)
    //   4. About — what the token is, and the tracker disclosure        (#343)
    //   5. Activity on this stock — proposals, fills and comments       (#344)
    //
    // The trade bar (#340) is not a slot: it belongs in a `safeAreaInset`, not in
    // this stack.
    //
    // The slots share one stack so the gap between two sections is set once: 24pt above
    // each rule, on top of the 12pt a section's last line already keeps under itself.
    // The stack is never empty: this is only built once the detail has loaded, and
    // About draws from the detail alone.
    //
    // Do not put an accessibility identifier on this stack. A modifier on a VStack is
    // applied to each of its children, so naming the stack renames every section
    // inside it and makes each one unfindable by its own name — which is exactly
    // what it did to the chart before this was noticed.
    private var detailSections: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
            // 1. Your cabals' position — holdings, P&L, open votes            (#341)
            if let summary = social.summary {
                AssetPositionCard(
                    summary: summary,
                    proposals: social.openProposals,
                    symbol: symbol,
                    // #341: a holding row opens the cabal, a vote row opens the vote.
                    // Without these the rows fall into their non-Button branch and the
                    // section is a picture of a position rather than a way into one.
                    openCabal: { route = .cabal(id: $0, name: cabalName($0)) },
                    openProposal: { route = .proposal(id: $0.id) }
                )
            } else if social.hasFailed {
                // A read that failed is not an answer of "nobody holds this". Say so, and
                // offer the way back — the same retry restores the activity below.
                AssetSocialFailedCard(symbol: symbol, retry: { Task { await social.load() } })
            }
            // 1b. What a pre-IPO token carries that a stock does not: reference price,
            //     premium, the issuer's disclosure and the other issuers it comes from.
            if let detail = model.detail, detail.resolvedKind == .preIpo {
                PreIpoDetailSection(detail: detail, openVariant: { route = .variant(symbol: $0) })
            }
            // 2. Stats — open/high/low, 52-week range, trading cost           (#342)
            if let grid = AssetStatsGrid.make(model.detail?.stats, currentUsdcMicros: model.detail?.priceUsdcMicros) {
                AssetStatsCard(grid: grid)
            }
            // 3. Stock vs token — NASDAQ against the xStock, premium          (#347)
            if let card = stockVsTokenCard {
                StockVsTokenCardView(card: card)
            }
            // lane: news
            // 3b. News — what happened to it today: the newest three headlines
            AssetNewsSection(model: news, companyName: heroDisplayName, seeAll: { route = .news })
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
                AssetActivityCard(
                    symbol: symbol,
                    activity: social.activity,
                    openCabal: { route = .cabal(id: $0, name: cabalName($0)) }
                )
            }
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
