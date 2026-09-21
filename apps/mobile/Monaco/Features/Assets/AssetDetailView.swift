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
    @State private var pickerKind: ProposalPickKind?
    @State private var toast: MonacoToast?
    private let sources: StocksFlowSources

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
                case .loaded(let detail):
                    hero
                    if !detail.routable {
                        Text("Can't be bought right now.")
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.warning)
                            .accessibilityIdentifier("asset-detail-no-route")
                    }
                    chartCard
                    detailSections
                    actionRow
                }
            }
            .padding(MonacoTheme.Space.m)
        }
        .monacoCanvas()
        .foregroundStyle(MonacoTheme.ink)
        .navigationTitle(model.detail?.displayTicker ?? AssetSymbolFormatter.display(symbol))
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("asset-detail-root")
        .monacoToast($toast)
        // Two independent loads: the curve does not wait on the (slow) detail call. The
        // detail read is keyed on who is signed in, not on the access token, so Dynamic's
        // token rotation does not reload the screen, but a new sign-in starts clean.
        .task(id: auth.sessionIdentity) {
            model.beginSession()
            await model.loadDetail()
        }
        .task(id: model.range) { _ = await model.loadChart(range: model.range) }
        // The hero keeps itself current while the member is looking at it. Both loops
        // are silent: a tick that fails leaves the screen exactly as they last saw it,
        // and only backs the loop off.
        .pollWhileVisible(every: pricePollInterval) { try await model.refreshDetail() }
        .pollWhileVisible(every: chartPollInterval) { try await model.refreshChart() }
        .onChange(of: model.rejectedSession) { _, rejected in
            guard let rejected else { return }
            Task { await auth.signOutAfterRejectedSession(rejectedToken: rejected.token) }
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
                },
                service: sources.propose,
                holdingsDataSource: sources.holdings
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
        EmptyView() // 1. Your cabals' position
        EmptyView() // 2. Stats grid
        EmptyView() // 3. Stock vs token
        EmptyView() // 4. About
        EmptyView() // 5. Activity on this stock
    }

    private var actionRow: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            Button("Buy") {
                pickerKind = .buy
            }
            .buttonStyle(.monacoPrimary)
            .disabled(!model.canBuy)
            .accessibilityIdentifier("asset-detail-buy")

            Button("Sell") {
                pickerKind = .sell
            }
            .buttonStyle(.monacoSecondary)
            .accessibilityIdentifier("asset-detail-sell")
        }
    }
}
