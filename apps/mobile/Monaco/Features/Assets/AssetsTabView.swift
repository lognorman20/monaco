import MonacoCore
import SwiftUI

/// Market browse: pinned search, one scrolling grid. Search replaces Popular.
struct AssetsTabView: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session

    private let apiClient = MonacoAPIClient()
    private let pageSize = 25
    private let searchDebounceNanos: UInt64 = 300_000_000

    @State private var searchQuery = ""
    @State private var assets: [MarketAssetDTO] = []
    @State private var hasMore = false
    @State private var listOffset = 0
    @State private var isLoadingList = false
    @State private var isLoadingMore = false
    @State private var listFailed = false
    @State private var searchTask: Task<Void, Never>?
    @State private var selectedSymbol: String?

    private var popular: [MarketAssetDTO] {
        session.popularAssets
    }

    private var trimmedQuery: String {
        searchQuery.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private var isSearching: Bool {
        !trimmedQuery.isEmpty
    }

    private var gridAssets: [MarketAssetDTO] {
        isSearching ? assets : popular
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            MonacoSearchField(
                placeholder: "Search Apple, Tesla, NVDA…",
                text: $searchQuery,
                isEnabled: true
            )
            .accessibilityIdentifier("assets-search-field")

            if !isSearching {
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
                AssetDetailView(auth: auth, symbol: selectedSymbol)
            }
        }
        .onChange(of: searchQuery) { _, _ in
            scheduleListSearch()
        }
        .task {
            if session.popularAssets.isEmpty {
                await session.refreshPopular(auth: auth)
            }
        }
        .onDisappear {
            searchTask?.cancel()
        }
        .monacoFrameStats("Stocks")
    }

    @ViewBuilder
    private var listRegion: some View {
        if isSearching, isLoadingList, assets.isEmpty, !listFailed {
            centeredStatus {
                ProgressView()
                    .tint(MonacoTheme.ink)
                Text("Loading stocks…")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
            }
            .accessibilityIdentifier("assets-search-loading")
        } else if isSearching, listFailed, assets.isEmpty {
            centeredStatus {
                EmptyState(
                    title: "Could not load stocks",
                    actionTitle: "Retry",
                    action: { Task { await loadList(reset: true) } }
                )
            }
        } else if isSearching, !isLoadingList, assets.isEmpty {
            centeredStatus {
                EmptyState(title: "No matches for that search")
            }
        } else if !isSearching, popular.isEmpty {
            ScrollView {
                EmptyState(title: "Popular names show up here once prices load")
                    .accessibilityIdentifier("assets-grid-popular")
            }
            .refreshable {
                await session.refreshPopular(auth: auth)
            }
        } else {
            ScrollView {
                MonacoGroupedList {
                    ForEach(Array(gridAssets.enumerated()), id: \.element.id) { index, asset in
                        Button {
                            selectedSymbol = asset.symbol
                        } label: {
                            assetRow(asset, isLast: index == gridAssets.count - 1)
                        }
                        .buttonStyle(.monacoRow)
                        .accessibilityIdentifier(
                            isSearching ? "assets-row-\(asset.symbol)" : "assets-popular-\(asset.symbol)"
                        )
                    }
                }

                if isSearching, hasMore {
                    Button(isLoadingMore ? "Loading…" : "Load more") {
                        Task { await loadList(reset: false) }
                    }
                    .buttonStyle(.monacoSecondary)
                    .disabled(isLoadingMore)
                    .padding(.top, MonacoTheme.Space.s)
                    .accessibilityIdentifier("assets-load-more")
                }
            }
            .refreshable {
                if isSearching {
                    await loadList(reset: true)
                } else {
                    await session.refreshPopular(auth: auth)
                }
            }
            .accessibilityIdentifier(isSearching ? "assets-grid-search" : "assets-grid-popular")
        }
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
            title: AssetDisplayNames.name(forSymbol: asset.symbol) ?? ticker,
            subtitle: ticker,
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


    private func scheduleListSearch() {
        searchTask?.cancel()
        let query = trimmedQuery
        if query.isEmpty {
            assets = []
            hasMore = false
            listOffset = 0
            isLoadingList = false
            listFailed = false
            return
        }
        isLoadingList = true
        assets = []
        listFailed = false
        searchTask = Task {
            do {
                try await Task.sleep(nanoseconds: searchDebounceNanos)
            } catch {
                return
            }
            guard !Task.isCancelled else { return }
            await loadList(reset: true)
        }
    }

    private func loadList(reset: Bool) async {
        guard let token = auth.accessToken else { return }
        let query = trimmedQuery
        guard !query.isEmpty else { return }

        if reset {
            isLoadingList = true
            listFailed = false
            listOffset = 0
            hasMore = false
        } else {
            isLoadingMore = true
        }
        defer {
            isLoadingList = false
            isLoadingMore = false
        }

        let offset = reset ? 0 : listOffset
        do {
            let response = try await apiClient.listMarketAssets(
                accessToken: token,
                query: query,
                limit: pageSize,
                offset: offset
            )
            if reset {
                assets = response.assets
            } else {
                assets.append(contentsOf: response.assets)
            }
            listOffset = assets.count
            hasMore = response.hasMore
        } catch {
            if error.isRequestCancellation { return }
            if reset {
                listFailed = true
                assets = []
            }
        }
    }
}
