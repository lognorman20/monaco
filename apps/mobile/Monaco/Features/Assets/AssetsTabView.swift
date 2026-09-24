import MonacoCore
import SwiftUI

/// Market browse: pinned search over four sections — what your cabals own, what
/// they are voting on, the day's movers, then the catalogue.
///
/// The order is the product's argument in one screen: your friends' money first,
/// the decision in front of you second, and the market only after that. Searching
/// replaces the lot with results, because a search is a different question.
struct AssetsTabView: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session
    @Environment(\.scenePhase) private var scenePhase
    // A tab body's `.task` fires once on first appearance, so coming back to Stocks from another
    // tab does not re-run it. The shell publishes which tab is showing for exactly this.
    @Environment(\.selectedMainTab) private var selectedMainTab
    @Environment(\.hostMainTab) private var hostMainTab
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    @State private var model: StocksTabModel
    @State private var searchQuery = ""
    @State private var selectedSymbol: String?

    /// `model` is the seam the sample harness uses: some states — rows on screen
    /// plus a failed refresh — are a sequence of two responses, not one canned
    /// answer, so the harness drives the model into them before the view appears.
    init(auth: PrivyAuthService, dataSource: StocksTabDataSource? = nil, model: StocksTabModel? = nil) {
        self.auth = auth
        _model = State(initialValue: model ?? StocksTabModel(dataSource: dataSource ?? LiveStocksTabDataSource(auth: auth)))
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

            listRegion
        }
        .padding(MonacoTheme.Space.m)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .monacoCanvas()
        .foregroundStyle(MonacoTheme.ink)
        .navigationTitle("Stocks")
        .navigationBarTitleDisplayMode(.large)
        // Same reason as the sections: without `.contain` this identifier is pushed
        // onto every leaf on the screen, and the search field stops answering to the
        // name the design system gave it.
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("assets-root")
        .navigationDestination(isPresented: Binding(
            get: { selectedSymbol != nil },
            set: { if !$0 { selectedSymbol = nil } }
        )) {
            if let selectedSymbol {
                AssetDetailView(auth: auth, symbol: selectedSymbol)
            }
        }
        .onChange(of: searchQuery) { _, newValue in
            model.updateQuery(newValue)
        }
        .task {
            model.seedPopular(session.popularAssets)
            await refreshTab()
        }
        .onChange(of: scenePhase) { _, phase in
            guard phase == .active else { return }
            Task { await refreshTab() }
        }
        .onChange(of: selectedMainTab) { _, tab in
            guard let hostMainTab, tab == hostMainTab else { return }
            Task { await refreshTab() }
        }
        .monacoFrameStats("Stocks")
    }

    @ViewBuilder
    private var listRegion: some View {
        if model.isSearching {
            searchRegion
        } else {
            browseRegion
        }
    }

    // MARK: Search

    @ViewBuilder
    private var searchRegion: some View {
        switch model.searchState {
        case .idle, .loading:
            ScrollView { skeletonRows(count: 6) }
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
                    staleCaption(identifier: "assets-refresh-failed")
                }
                assetList(model.resultRows, identifierPrefix: "assets-row")
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

    // MARK: Browse

    @ViewBuilder
    private var browseRegion: some View {
        switch model.popularState {
        case .loading where model.popular.isEmpty:
            ScrollView { skeletonRows(count: 6) }
                .scrollDisabled(true)
                .accessibilityIdentifier("assets-popular-loading")
        case .failed where model.popular.isEmpty:
            centeredStatus {
                EmptyState(
                    title: "Could not load popular stocks",
                    actionTitle: "Retry",
                    action: { Task { await forceRefreshTab() } }
                )
                .accessibilityIdentifier("assets-popular-failed")
            }
        default:
            ScrollView {
                LazyVStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                    inYourCabalsSection
                    upForVoteSection
                    topMoversSection
                    popularSection
                }
            }
            .refreshable { await forceRefreshTab() }
            .accessibilityIdentifier("assets-grid-popular")
        }
    }

    @ViewBuilder
    private var inYourCabalsSection: some View {
        switch model.socialState {
        case .loading where model.heldRows.isEmpty:
            section("In your cabals", identifier: "assets-held-loading") {
                skeletonRows(count: 2)
            }
        case .failed where model.heldRows.isEmpty:
            section("In your cabals", identifier: "assets-held-failed") {
                EmptyState(
                    title: "Could not load your cabals",
                    message: "The market below is still live.",
                    actionTitle: "Retry",
                    action: { Task { await model.loadSocial() } }
                )
            }
        default:
            if model.heldRows.isEmpty {
                section("In your cabals", identifier: "assets-held-empty") {
                    EmptyState(
                        title: "Your cabals own nothing yet",
                        message: "Propose the first buy and it shows up here."
                    )
                }
            } else {
                section("In your cabals", identifier: "assets-held") {
                    if model.socialRefreshFailed {
                        staleCaption(identifier: "assets-held-stale")
                    }
                    assetList(model.heldRows, identifierPrefix: "assets-held")
                }
            }
        }
    }

    /// The same sentence the search region uses. One wording for "what you are
    /// looking at may be out of date", wherever it happens.
    private func staleCaption(identifier: String) -> some View {
        Text(AssetsTabView.staleRefreshCaption)
            .font(MonacoTheme.Typo.caption)
            .foregroundStyle(MonacoTheme.warning)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.bottom, MonacoTheme.Space.s)
            .accessibilityIdentifier(identifier)
    }

    static let staleRefreshCaption = "Couldn't refresh — these prices may be out of date."

    @ViewBuilder
    private var upForVoteSection: some View {
        // No header at all when there is nothing open: an empty "Up for vote" on
        // every quiet day is a section that teaches people to skip it.
        if !model.voteRows.isEmpty {
            section("Up for vote", identifier: "assets-vote") {
                assetList(model.voteRows, identifierPrefix: "assets-vote")
            }
        }
    }

    @ViewBuilder
    private var topMoversSection: some View {
        // Hidden at accessibility text sizes: a mover card is a thing you scan
        // several of, and at AX5 one card fills the screen. Every mover is in
        // Popular below, so nothing is lost by leaving it out.
        if !model.moverRows.isEmpty, StockMoverStrip.isAvailable(at: dynamicTypeSize) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                MonacoSectionHeader("Top movers")
                ScrollView(.horizontal, showsIndicators: false) {
                    LazyHStack(spacing: MonacoTheme.Space.sm) {
                        ForEach(model.moverRows) { row in
                            Button { open(row.asset.symbol) } label: {
                                StockMoverCard(row: row)
                            }
                            .buttonStyle(.plain)
                            .accessibilityIdentifier("assets-mover-\(row.asset.symbol)")
                        }
                    }
                    // The strip runs to the screen edge, so it reads as scrollable,
                    // but its first card lines up with the sections above it.
                    .padding(.horizontal, MonacoTheme.Space.m)
                }
                .padding(.horizontal, -MonacoTheme.Space.m)
            }
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("assets-movers")
        }
    }

    private var popularSection: some View {
        section("Popular", identifier: "assets-popular") {
            assetList(model.popularRows, identifierPrefix: "assets-popular")
        }
    }

    private func section<Content: View>(
        _ title: String,
        identifier: String,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            MonacoSectionHeader(title)
            content()
        }
        // `.contain` first, so the section is itself one container element and the
        // identifier lands on it. Without it the VStack is not an accessibility
        // element at all, and SwiftUI pushes the identifier down onto every leaf
        // inside — stamping "assets-popular" over each row's own identifier and
        // leaving every row in the section indistinguishable.
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier(identifier)
    }

    private func assetList(_ rows: [MarketRowData], identifierPrefix: String) -> some View {
        MonacoGroupedList {
            ForEach(rows) { row in
                Button {
                    open(row.asset.symbol)
                } label: {
                    StockListRow(
                        row: row,
                        afterHours: model.afterHours,
                        isLast: row.id == rows.last?.id
                    )
                }
                .buttonStyle(.monacoRow)
                // On the Button, not inside its label: the row combines its children
                // into one element, and an identifier set inside that label never
                // reaches the button element — the row then inherits the section's
                // identifier instead, and every row in a section looks alike to a test.
                .accessibilityIdentifier("\(identifierPrefix)-\(row.asset.symbol)")
            }
        }
    }

    private func open(_ symbol: String) {
        Haptics.selection()
        selectedSymbol = symbol
    }

    private func skeletonRows(count: Int) -> some View {
        MonacoGroupedList {
            ForEach(0..<count, id: \.self) { _ in
                HStack(spacing: MonacoTheme.Space.sm) {
                    SkeletonBlock(width: 40, height: 40, radius: MonacoTheme.Radius.tile)
                    VStack(alignment: .leading, spacing: 6) {
                        SkeletonBlock(width: 120, height: 14)
                        SkeletonBlock(width: 56, height: 12)
                    }
                    Spacer()
                    SkeletonBlock(width: 56, height: 24)
                    VStack(alignment: .trailing, spacing: 6) {
                        SkeletonBlock(width: 64, height: 14)
                        SkeletonBlock(width: 46, height: 14)
                    }
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .frame(minHeight: 64)
            }
        }
        .accessibilityLabel("Loading stocks")
    }

    /// Keeps the shared session strip in step with the tab so other screens read fresh prices too.
    private func refreshTab() async {
        async let catalogue: Void = model.refreshPopularIfStale()
        async let social: Void = model.refreshSocialIfStale()
        _ = await (catalogue, social)
        syncPopularToSession()
    }

    private func forceRefreshTab() async {
        await model.refreshEverything()
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
}
