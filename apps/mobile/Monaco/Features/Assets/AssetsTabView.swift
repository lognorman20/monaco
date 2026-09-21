import MonacoCore
import SwiftUI

/// Market browse: pinned search, one scrolling list. Search replaces Popular.
struct AssetsTabView: View {
    @ObservedObject var auth: DynamicAuthService
    @Environment(AppSessionStore.self) private var session
    @Environment(\.scenePhase) private var scenePhase
    // A tab body's `.task` fires once on first appearance, so coming back to Stocks from another
    // tab does not re-run it. The shell publishes which tab is showing for exactly this.
    @Environment(\.selectedMainTab) private var selectedMainTab
    @Environment(\.hostMainTab) private var hostMainTab

    @State private var model: StocksTabModel
    @State private var searchQuery = ""
    @State private var selectedSymbol: String?
    private let sources: StocksFlowSources

    init(auth: DynamicAuthService, sources: StocksFlowSources = .live) {
        self.auth = auth
        self.sources = sources
        _model = State(initialValue: StocksTabModel(dataSource: sources.tab ?? LiveStocksTabDataSource(auth: auth)))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            MonacoSearchField(
                placeholder: "Search Apple, Tesla, NVDA…",
                text: $searchQuery,
                isEnabled: true
            )
            .textInputAutocapitalization(.words)
            .autocorrectionDisabled()
            .submitLabel(.search)
            .accessibilityIdentifier("assets-search-field")

            if !model.isSearching {
                MonacoSectionHeader("Popular")
            }

            listRegion
        }
        .padding(MonacoTheme.Space.m)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .monacoCanvas()
        .foregroundStyle(MonacoTheme.ink)
        .navigationTitle("Stocks")
        .navigationBarTitleDisplayMode(.large)
        .accessibilityIdentifier("assets-root")
        .navigationDestination(isPresented: Binding(
            get: { selectedSymbol != nil },
            set: { if !$0 { selectedSymbol = nil } }
        )) {
            if let selectedSymbol {
                AssetDetailView(auth: auth, symbol: selectedSymbol, sources: sources)
            }
        }
        .onChange(of: searchQuery) { _, newValue in
            model.updateQuery(newValue)
        }
        .task {
            model.seedPopular(session.popularAssets)
            await refreshPopular()
        }
        .onChange(of: scenePhase) { _, phase in
            guard phase == .active else { return }
            Task { await refreshPopular() }
        }
        .onChange(of: selectedMainTab) { _, tab in
            guard let hostMainTab, tab == hostMainTab else { return }
            Task { await refreshPopular() }
        }
        .onChange(of: model.rejectedSession) { _, rejected in
            guard let rejected else { return }
            Task { await auth.signOutAfterRejectedSession(rejectedToken: rejected.token) }
        }
        .monacoFrameStats("Stocks")
    }

    @ViewBuilder
    private var listRegion: some View {
        if model.isSearching {
            searchRegion
        } else {
            popularRegion
        }
    }

    @ViewBuilder
    private var searchRegion: some View {
        switch model.searchState {
        case .idle, .loading:
            ScrollView { popularSkeleton }
                .scrollDisabled(true)
                .accessibilityIdentifier("assets-search-loading")
        case .failed:
            centeredStatus {
                EmptyState(
                    title: "Could not load stocks",
                    actionTitle: "Retry",
                    action: { Task { await model.retrySearch() } }
                )
            }
        case .empty:
            centeredStatus {
                EmptyState(title: "No matches for that search")
            }
        case .results:
            ScrollView {
                if model.refreshFailed {
                    Text("Couldn't refresh — these prices may be out of date.")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.warning)
                        .frame(maxWidth: .infinity)
                        .padding(.bottom, MonacoTheme.Space.s)
                        .accessibilityIdentifier("assets-refresh-failed")
                }
                assetList(model.results)
                if model.loadMoreFailed {
                    Text("Could not load more stocks.")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                        .frame(maxWidth: .infinity)
                        .padding(.top, MonacoTheme.Space.s)
                }
                if model.hasMore {
                    Button(model.isLoadingMore ? "Loading…" : "Load more") {
                        Task { await model.loadMore() }
                    }
                    .buttonStyle(.monacoSecondary)
                    .disabled(model.isLoadingMore)
                    .padding(.top, MonacoTheme.Space.s)
                    .accessibilityIdentifier("assets-load-more")
                }
            }
            .refreshable { await model.refreshSearch() }
            .accessibilityIdentifier("assets-grid-search")
        }
    }

    @ViewBuilder
    private var popularRegion: some View {
        switch model.popularState {
        case .loading where model.popular.isEmpty:
            // Not "assets-popular-…": that prefix is how UI tests find a stock row to tap.
            ScrollView { popularSkeleton }
                .scrollDisabled(true)
                .accessibilityIdentifier("assets-grid-popular-loading")
        case .failed where model.popular.isEmpty:
            centeredStatus {
                EmptyState(
                    title: "Could not load popular stocks",
                    actionTitle: "Retry",
                    action: { Task { await forceRefreshPopular() } }
                )
                .accessibilityIdentifier("assets-grid-popular-failed")
            }
        default:
            ScrollView {
                assetList(model.popular)
            }
            .refreshable { await forceRefreshPopular() }
            .accessibilityIdentifier("assets-grid-popular")
        }
    }

    private func assetList(_ assets: [MarketAssetDTO]) -> some View {
        MonacoGroupedList {
            ForEach(Array(assets.enumerated()), id: \.element.id) { index, asset in
                Button {
                    selectedSymbol = asset.symbol
                } label: {
                    assetRow(asset, isLast: index == assets.count - 1)
                }
                .buttonStyle(.monacoRow)
                .disabled(!asset.canBuy)
                .opacity(asset.canBuy ? 1 : 0.6)
                .accessibilityIdentifier(
                    model.isSearching ? "assets-row-\(asset.symbol)" : "assets-popular-\(asset.symbol)"
                )
            }
        }
    }

    /// Placeholder rows while a list has nothing to show yet. Search reuses the popular strip's.
    private var popularSkeleton: some View {
        MonacoGroupedList {
            ForEach(0..<6, id: \.self) { _ in
                HStack(spacing: MonacoTheme.Space.sm) {
                    SkeletonBlock(width: 40, height: 40, radius: MonacoTheme.Radius.tile)
                    VStack(alignment: .leading, spacing: 6) {
                        SkeletonBlock(width: 120, height: 14)
                        SkeletonBlock(width: 56, height: 12)
                    }
                    Spacer()
                    SkeletonBlock(width: 64, height: 14)
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .frame(minHeight: 60)
            }
        }
        .accessibilityLabel("Loading stocks")
    }

    /// Keeps the shared session strip in step with the tab so other screens read fresh prices too.
    private func refreshPopular() async {
        await model.refreshPopularIfStale()
        syncPopularToSession()
    }

    private func forceRefreshPopular() async {
        await model.loadPopular()
        syncPopularToSession()
    }

    private func syncPopularToSession() {
        guard !model.popular.isEmpty else { return }
        session.popularAssets = model.popular
    }

    private func centeredStatus<Content: View>(@ViewBuilder content: () -> Content) -> some View {
        VStack(spacing: MonacoTheme.Space.m) {
            Spacer(minLength: 0)
            content()
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func assetRow(_ asset: MarketAssetDTO, isLast: Bool) -> some View {
        let ticker = AssetSymbolFormatter.display(asset.symbol)
        return MonacoRow(
            title: ProposeStock.displayName(symbol: asset.symbol, catalogName: asset.name),
            subtitle: asset.canBuy ? ticker : "Can't be bought right now",
            isLast: isLast,
            leading: { StockMark(symbol: asset.symbol, size: 40) },
            trailing: {
                if let micros = asset.priceUsdcMicros {
                    MoneyText(micros: micros, style: .row)
                } else {
                    Text("—").font(MonacoTheme.Typo.moneyRow).foregroundStyle(MonacoTheme.muted)
                }
                if let change = asset.change24h, !change.isEmpty {
                    PercentText(percentReturn: change, style: .caption)
                }
            }
        )
    }
}
