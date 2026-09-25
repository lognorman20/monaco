import MonacoCore
import SwiftUI

/// Buy, step 1 of 3: pick a stock. Popular stocks show before typing; search results page in as the
/// last row appears. Tapping a stock pushes the amount step.
///
/// The list reads like the Stocks tab, because it is the same market: the coin, the ticker in the
/// market's voice, the price and the day's move. The one addition is the company's name under the
/// ticker — here the member is choosing, not scanning a watchlist, and "AMBR" alone is a guess.
struct ProposeBuyView: View {
    let groupId: String
    var initialSymbol: String?
    var initialKind: AssetKind = .stock
    var initialDecimals: Int = AssetCatalogDefaults.decimals
    var onProposed: ((_ proposalId: String) -> Void)?

    private let service: ProposeService
    private static let pageSize = 25
    private static let searchDebounce: Duration = .milliseconds(300)

    @Environment(AppSessionStore.self) private var session: AppSessionStore?

    @State private var pot: ProposePot?
    @State private var potLoadFailed = false
    @State private var isLoadingPot = false
    @State private var query = ""
    @State private var popular: [ProposeStock] = []
    @State private var popularLoadFailed = false
    @State private var results: [ProposeStock] = []
    @State private var hasMore = false
    @State private var isSearching = false
    @State private var isLoadingMore = false
    @State private var searchFailed = false
    @State private var picked: ProposeStock?
    @State private var toast: MonacoToast?
    @State private var didApplyInitialSymbol = false

    /// Entry from Stock detail's cabal picker (no chooser sheet): on success the flow pops back
    /// here and confirms with a toast.
    init(auth: PrivyAuthService, groupId: String, initialSymbol: String? = nil, onProposed: ((_ proposalId: String) -> Void)? = nil) {
        self.init(service: LiveProposeService(auth: auth), groupId: groupId, pot: nil, initialSymbol: initialSymbol, onProposed: onProposed)
    }

    init(
        service: ProposeService,
        groupId: String,
        pot: ProposePot?,
        initialSymbol: String? = nil,
        onProposed: ((_ proposalId: String) -> Void)? = nil
    ) {
        self.service = service
        self.groupId = groupId
        self.initialSymbol = initialSymbol
        self.onProposed = onProposed
        _pot = State(initialValue: pot)
    }

    private var trimmedQuery: String {
        query.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var body: some View {
        ScrollView {
            // No side padding on the stack: the ruled lists run edge to edge, and the search field
            // and each header inset themselves.
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                MonacoSearchField(placeholder: ProposeFlowCopy.searchPlaceholder, text: $query)
                    .textInputAutocapitalization(.words)
                    .autocorrectionDisabled()
                    .submitLabel(.search)
                    .accessibilityIdentifier("proposal-search-field")
                    .padding(.horizontal, MonacoTheme.Space.m)

                if trimmedQuery.isEmpty {
                    popularSection
                } else {
                    resultsSection
                }
            }
            .padding(.top, MonacoTheme.Space.s)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .scrollDismissesKeyboard(.immediately)
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .navigationTitle(ProposeFlowCopy.buyTitle)
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(item: $picked) { stock in
            if let pot {
                ProposeAmountView(service: service, groupId: groupId, stock: stock, pot: pot, onProposed: finish)
            } else {
                ProposePotUnavailable(failed: potLoadFailed) { Task { await loadPot() } }
            }
        }
        .monacoToast($toast)
        .task {
            // A stock the member already chose is honoured on the first frame, before anything is
            // awaited: the list below is live while the pot and the popular stocks load, so a tap
            // on it during those round trips would otherwise be overwritten by this. The amount
            // step still waits for the pot — `ProposePotUnavailable` stands in until it lands.
            applyInitialSymbolIfNeeded()
            // The pot is the buy ceiling, so this screen loads it once for the whole flow.
            async let stocks: Void = loadPopular()
            await loadPot()
            await stocks
        }
        .task(id: trimmedQuery) {
            guard !trimmedQuery.isEmpty else {
                results = []
                searchFailed = false
                return
            }
            do {
                try await Task.sleep(for: Self.searchDebounce)
            } catch {
                return
            }
            await search(reset: true)
        }
        .accessibilityIdentifier("propose-buy")
    }

    // MARK: Sections

    @ViewBuilder
    private var popularSection: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(ProposeFlowCopy.popularTitle)
                .padding(.horizontal, MonacoTheme.Space.m)
            if popular.isEmpty {
                if popularLoadFailed {
                    EmptyState(title: ProposeFlowCopy.stocksLoadFailed, actionTitle: ProposalFeedCopy.tryAgain) {
                        Task { await loadPopular(force: true) }
                    }
                } else {
                    ProposeStockSkeleton(rows: 5)
                }
            } else {
                stockList(popular)
            }
        }
    }

    @ViewBuilder
    private var resultsSection: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            if isSearching && results.isEmpty {
                ProposeStockSkeleton(rows: 3)
            } else if searchFailed && results.isEmpty {
                EmptyState(title: ProposeFlowCopy.stocksLoadFailed, actionTitle: ProposalFeedCopy.tryAgain) {
                    Task { await search(reset: true) }
                }
            } else if results.isEmpty {
                EmptyState(title: ProposeFlowCopy.noMatches(trimmedQuery))
                    .accessibilityIdentifier("proposal-search-empty")
            } else {
                stockList(results, paginates: true)
                if isLoadingMore {
                    ProgressView()
                        .tint(MonacoTheme.muted)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, MonacoTheme.Space.s)
                }
            }
        }
    }

    private func stockList(_ stocks: [ProposeStock], paginates: Bool = false) -> some View {
        MonacoGroupedList {
            ForEach(Array(stocks.enumerated()), id: \.element.id) { index, stock in
                let isLast = index == stocks.count - 1
                Button {
                    pick(stock)
                } label: {
                    ProposeStockRow(
                        stock: stock,
                        logoURL: ProposeStockLogo.url(for: stock.symbol, in: session),
                        isLast: isLast
                    )
                }
                .buttonStyle(.monacoRow)
                // A stock with no route cannot be bought whatever amount is typed next, so the row
                // is not a way in. It stays in the list, dimmed, saying why.
                .disabled(!stock.isTradable)
                .accessibilityIdentifier("proposal-asset-\(stock.symbol)")
                .onAppear {
                    if paginates, isLast, hasMore { Task { await search(reset: false) } }
                }
            }
        }
    }

    // MARK: Actions

    private func pick(_ stock: ProposeStock) {
        Haptics.tap()
        picked = stock
    }

    private func applyInitialSymbolIfNeeded() {
        guard !didApplyInitialSymbol, let initialSymbol, !initialSymbol.isEmpty else { return }
        didApplyInitialSymbol = true
        let known = (session?.popularAssets ?? []).first { $0.symbol.caseInsensitiveCompare(initialSymbol) == .orderedSame }
        picked = known.map { ProposeStock(market: $0) }
            ?? ProposeStock(symbol: initialSymbol, kind: initialKind, tokenDecimals: initialDecimals)
    }

    /// Without a chooser sheet to dismiss, pop back to the picker and confirm here.
    private func finish(_ proposalId: String) {
        if let onProposed {
            onProposed(proposalId)
            return
        }
        picked = nil
        Haptics.success()
        toast = MonacoToast(
            message: pot.map { ProposeFlowCopy.proposalSent($0.name) } ?? ProposeFlowCopy.proposalSentGeneric,
            isSuccess: true
        )
    }

    private func loadPot() async {
        // Two quick taps on the retry used to fire two group-view reads.
        guard pot == nil, !isLoadingPot else { return }
        isLoadingPot = true
        potLoadFailed = false
        defer { isLoadingPot = false }
        do {
            pot = try await service.pot(groupId: groupId)
        } catch {
            if !error.isRequestCancellation { potLoadFailed = true }
        }
    }

    private func loadPopular(force: Bool = false) async {
        if !force, let cached = session?.popularAssets, !cached.isEmpty {
            popular = cached.map { ProposeStock(market: $0) }
            return
        }
        popularLoadFailed = false
        do {
            popular = try await service.popularStocks()
        } catch is CancellationError {
            return
        } catch {
            if !error.isRequestCancellation { popularLoadFailed = true }
        }
    }

    private func search(reset: Bool) async {
        let term = trimmedQuery
        guard !term.isEmpty else { return }
        if reset {
            isSearching = true
            searchFailed = false
        } else {
            guard !isLoadingMore else { return }
            isLoadingMore = true
        }
        defer {
            isSearching = false
            isLoadingMore = false
        }
        do {
            let page = try await service.searchStocks(
                groupId: groupId, query: term, offset: reset ? 0 : results.count, limit: Self.pageSize
            )
            guard term == trimmedQuery else { return }
            results = reset ? page.stocks : results + page.stocks
            hasMore = page.hasMore
        } catch is CancellationError {
            return
        } catch {
            if error.isRequestCancellation { return }
            if reset { searchFailed = true; results = [] }
            hasMore = false
        }
    }
}

/// Stands in for the amount step until the pot is known: it is the buy ceiling, so without it
/// "how much" has nothing to check the answer against. A failed load gets a real retry here
/// instead of a dead screen with a Review button that can never be tapped.
private struct ProposePotUnavailable: View {
    let failed: Bool
    let onRetry: () -> Void

    var body: some View {
        VStack {
            if failed {
                EmptyState(title: ProposeFlowCopy.potLoadFailed, actionTitle: ProposalFeedCopy.tryAgain, action: onRetry)
                    .accessibilityIdentifier("propose-pot-error")
            } else {
                ProposeAmountSkeleton()
                    .frame(maxHeight: .infinity, alignment: .top)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .navigationTitle(ProposeFlowCopy.amountTitle)
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// The amount step while the pot loads, in the step's own shape: the stock's line, the figure,
/// the pot line and the chips. Nothing in it can be typed into or tapped.
private struct ProposeAmountSkeleton: View {
    var body: some View {
        VStack(spacing: 0) {
            ProposeStockSkeleton(rows: 1)
            VStack(spacing: MonacoTheme.Space.l) {
                SkeletonBlock(width: 120, height: 48)
                SkeletonBlock(width: 150, height: 14)
            }
            .padding(.top, MonacoTheme.Space.xl)
            HStack(spacing: MonacoTheme.Space.s) {
                ForEach(0..<4, id: \.self) { _ in
                    SkeletonBlock(width: 64, height: 44, radius: 22)
                }
            }
            .padding(.top, MonacoTheme.Space.m)
        }
        .padding(.top, MonacoTheme.Space.s)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }
}

/// The company's logo for a stock the propose flow knows only by symbol. `ProposeStock` carries no
/// logo; the popular list the session already holds does, so the flow's rows borrow it. Nil falls
/// back to the ticker struck on the coin, which is the mark's resting state anyway.
enum ProposeStockLogo {
    @MainActor
    static func url(for symbol: String, in session: AppSessionStore?) -> URL? {
        session?.popularAssets.first { $0.symbol.caseInsensitiveCompare(symbol) == .orderedSame }?.logoURL
    }
}

/// A stock in the propose flow, in the market's voice: the coin, the ticker over the company's
/// name, and the price over the day's move on the right.
///
/// Laid out like `StockListRow` (the Stocks tab's row) without its sparkline, and on the same
/// `MonacoRowLayout` rules: the labels keep a floor and truncate, the figures shrink, and at the
/// accessibility sizes the figures drop under the labels. A search result carries no price, so it
/// shows none rather than a dash per row. A stock that cannot be bought dims and says why.
struct ProposeStockRow: View {
    let stock: ProposeStock
    var logoURL: URL?
    var isLast = false

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var titleWidthFloor = MonacoRowLayout.baseMinimumTitleWidth

    /// The Stocks tab's mark, so the same coin is the same size on both lists.
    static let markSize: CGFloat = StockListRow.markSize

    private var layout: MonacoRowLayout {
        MonacoRowLayout(dynamicTypeSize: dynamicTypeSize, scaledTitleWidthFloor: titleWidthFloor)
    }

    private var caption: String? {
        if !stock.isTradable { return ProposeScreenCopy.cantBuyCaption(name: stock.name) }
        // A stock the catalogue has no name for falls back to its ticker, which is already the title.
        return stock.name == stock.ticker ? nil : stock.name
    }

    var body: some View {
        content
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.vertical, 8)
            .frame(minHeight: 64)
            .contentShape(Rectangle())
            .overlay(alignment: .bottom) {
                if !isLast {
                    MonacoRule()
                        .padding(.leading, layout.separatorLeadingInset(markSize: Self.markSize))
                }
            }
            .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var content: some View {
        if layout.isStacked {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                HStack(spacing: MonacoTheme.Space.sm) {
                    mark
                    labels
                }
                if hasFigures {
                    HStack(spacing: MonacoTheme.Space.s) {
                        figures
                    }
                }
            }
        } else {
            HStack(spacing: MonacoTheme.Space.sm) {
                mark
                labels.frame(minWidth: layout.minimumTitleWidth, alignment: .leading)
                if hasFigures {
                    VStack(alignment: .trailing, spacing: 3) {
                        figures
                    }
                    .layoutPriority(1)
                }
            }
        }
    }

    private var mark: some View {
        StockMark(symbol: stock.symbol, displayName: stock.name, assetKind: stock.assetKind, size: Self.markSize, logoURL: logoURL)
            .frame(width: Self.markSize, height: Self.markSize)
            .opacity(stock.isTradable ? 1 : 0.45)
    }

    private var labels: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(stock.ticker)
                .font(MonacoTheme.Typo.ticker)
                .foregroundStyle(stock.isTradable ? MonacoTheme.ink : MonacoTheme.disabledLabel)
                .lineLimit(layout.titleLineLimit)
                .truncationMode(.tail)
            if let caption {
                Text(caption)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .lineLimit(layout.subtitleLineLimit)
                    .truncationMode(.tail)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var hasFigures: Bool {
        stock.priceMicros != nil
    }

    /// The price, then the day's move. A stock that cannot be bought keeps its price, greyed, and
    /// drops the coloured pill: a green or red capsule is the loudest thing on a row, and this row
    /// is the one the member should pass over.
    @ViewBuilder
    private var figures: some View {
        if let micros = stock.priceMicros {
            MoneyText(
                micros: micros,
                style: .row,
                color: stock.isTradable ? MonacoTheme.ink : MonacoTheme.disabledLabel,
                voice: .market
            )
            // No day figure, no pill: a row of grey dashes says nothing the price does not.
            if stock.isTradable, stock.change24h != nil {
                DayChangePill(change24h: stock.change24h, priceUsdcMicros: micros)
            }
        }
    }
}

/// Loading rows in the shape of `ProposeStockRow`: the coin, two lines, the price over the pill.
struct ProposeStockSkeleton: View {
    var rows = 4

    var body: some View {
        MonacoGroupedList {
            ForEach(0..<rows, id: \.self) { index in
                HStack(spacing: MonacoTheme.Space.sm) {
                    SkeletonBlock(width: ProposeStockRow.markSize, height: ProposeStockRow.markSize, radius: ProposeStockRow.markSize / 2)
                    VStack(alignment: .leading, spacing: 6) {
                        SkeletonBlock(width: 64, height: 14)
                        SkeletonBlock(width: 96, height: 12)
                    }
                    Spacer(minLength: MonacoTheme.Space.s)
                    VStack(alignment: .trailing, spacing: 6) {
                        SkeletonBlock(width: 72, height: 14)
                        SkeletonBlock(width: 52, height: 20, radius: 10)
                    }
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .frame(minHeight: 64)
                .overlay(alignment: .bottom) {
                    if index < rows - 1 {
                        MonacoRule().padding(.leading, MonacoTheme.Space.m + ProposeStockRow.markSize + MonacoTheme.Space.sm)
                    }
                }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading stocks")
    }
}
