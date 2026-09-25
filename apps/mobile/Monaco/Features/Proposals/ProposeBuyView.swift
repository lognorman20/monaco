import MonacoCore
import SwiftUI

/// Buy, step 1 of 3: pick a stock. Popular stocks show before typing; search results page in as the
/// last row appears. Tapping a stock pushes the amount step.
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
    init(
        auth: PrivyAuthService,
        groupId: String,
        initialSymbol: String? = nil,
        initialKind: AssetKind = .stock,
        initialDecimals: Int = AssetCatalogDefaults.decimals,
        onProposed: ((_ proposalId: String) -> Void)? = nil
    ) {
        self.init(
            service: LiveProposeService(auth: auth),
            groupId: groupId,
            pot: nil,
            initialSymbol: initialSymbol,
            initialKind: initialKind,
            initialDecimals: initialDecimals,
            onProposed: onProposed
        )
    }

    init(
        service: ProposeService,
        groupId: String,
        pot: ProposePot?,
        initialSymbol: String? = nil,
        initialKind: AssetKind = .stock,
        initialDecimals: Int = AssetCatalogDefaults.decimals,
        onProposed: ((_ proposalId: String) -> Void)? = nil
    ) {
        self.service = service
        self.groupId = groupId
        self.initialSymbol = initialSymbol
        self.initialKind = initialKind
        self.initialDecimals = initialDecimals
        self.onProposed = onProposed
        _pot = State(initialValue: pot)
    }

    private var trimmedQuery: String {
        query.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                MonacoSearchField(placeholder: ProposeFlowCopy.searchPlaceholder, text: $query)
                    .textInputAutocapitalization(.words)
                    .autocorrectionDisabled()
                    .submitLabel(.search)
                    .accessibilityIdentifier("proposal-search-field")

                if trimmedQuery.isEmpty {
                    popularSection
                } else {
                    resultsSection
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
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
            if popular.isEmpty {
                if popularLoadFailed {
                    EmptyState(title: ProposeFlowCopy.stocksLoadFailed, actionTitle: ProposalFeedCopy.tryAgain) {
                        Task { await loadPopular(force: true) }
                    }
                } else {
                    skeletonRows
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
                skeletonRows
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
                    ProposeStockRow(stock: stock, isLast: isLast)
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("proposal-asset-\(stock.symbol)")
                .onAppear {
                    if paginates, isLast, hasMore { Task { await search(reset: false) } }
                }
            }
        }
    }

    private var skeletonRows: some View {
        MonacoGroupedList {
            ForEach(0..<4, id: \.self) { index in
                HStack(spacing: MonacoTheme.Space.sm) {
                    SkeletonBlock(width: 44, height: 44, radius: MonacoTheme.Radius.tile)
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

    // MARK: Actions

    private func pick(_ stock: ProposeStock) {
        Haptics.tap()
        picked = stock
    }

    private func applyInitialSymbolIfNeeded() {
        guard !didApplyInitialSymbol, let initialSymbol, !initialSymbol.isEmpty else { return }
        didApplyInitialSymbol = true
        let known = (session?.popularAssets ?? []).first { $0.symbol.caseInsensitiveCompare(initialSymbol) == .orderedSame }
        picked = known.map(ProposeStock.init(market:))
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
            popular = cached.map(ProposeStock.init(market:))
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
                ProgressView()
                    .tint(MonacoTheme.muted)
                    .accessibilityLabel("Loading")
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .navigationTitle(ProposeFlowCopy.amountTitle)
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// Stock row for the pick step: mark, ticker, price over 24h move. The company name
/// waits for the amount screen, which is where the member has committed to a stock
/// and has room to be told what it is.
struct ProposeStockRow: View {
    let stock: ProposeStock
    var isLast = false

    var body: some View {
        MonacoRow(
            title: stock.ticker,
            chevron: true,
            isLast: isLast
        ) {
            StockMark(symbol: stock.symbol, displayName: stock.name, assetKind: stock.assetKind)
        } trailing: {
            VStack(alignment: .trailing, spacing: 4) {
                if stock.assetKind == .preIpo {
                    MonacoChip(title: PreIpoCopy.chipLabel, isSelected: false)
                }
                if let micros = stock.priceMicros {
                    MoneyText(micros: micros, style: .row)
                    if let change = stock.change24h, !change.isEmpty {
                        PercentText(percentReturn: change, style: .caption)
                    }
                }
            }
        }
    }
}
