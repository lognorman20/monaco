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
    @State private var pickerKind: ProposalPickKind?
    @State private var toast: MonacoToast?

    /// How often the hero re-reads the price. Only the sample harness passes anything
    /// but the default, so a scripted price walk does not take a minute to show.
    private let pricePollInterval: Duration

    init(
        auth: PrivyAuthService,
        symbol: String,
        dataSource: AssetDetailDataSource? = nil,
        pricePollInterval: Duration = AssetDetailPolling.price
    ) {
        self.auth = auth
        self.symbol = symbol
        self.pricePollInterval = pricePollInterval
        _model = State(initialValue: AssetDetailModel(
            symbol: symbol,
            dataSource: dataSource ?? LiveAssetDetailDataSource(auth: auth)
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
                    if !detail.liquidity.routable {
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
        .navigationTitle(AssetSymbolFormatter.display(symbol))
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("asset-detail-root")
        .monacoToast($toast)
        // Two independent loads: the curve does not wait on the (slow) detail call.
        .task { await model.loadDetail() }
        .task(id: model.range) { await model.loadChart(range: model.range) }
        // The hero keeps itself current while the member is looking at it. Both loops
        // are silent: a tick that fails leaves the screen exactly as they last saw it.
        .pollWhileVisible(every: pricePollInterval) { await model.refreshDetail() }
        .pollWhileVisible(every: AssetDetailPolling.chart) { await model.refreshChart() }
        .onChange(of: model.sessionExpired) { _, expired in
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
    @ViewBuilder
    private var detailSections: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
            EmptyView() // 1. Your cabals' position
            EmptyView() // 2. Stats grid
            EmptyView() // 3. Stock vs token
            EmptyView() // 4. About
            EmptyView() // 5. Activity on this stock
        }
        .accessibilityIdentifier("asset-detail-sections")
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
