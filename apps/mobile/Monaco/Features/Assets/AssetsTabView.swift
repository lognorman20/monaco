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
                Text("Popular")
                    .font(MonacoTheme.TypeRole.title)
                    .foregroundStyle(MonacoTheme.ink)
            }

            gridRegion
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
    }

    @ViewBuilder
    private var gridRegion: some View {
        if isSearching, isLoadingList, assets.isEmpty, !listFailed {
            centeredStatus {
                ProgressView()
                    .tint(MonacoTheme.ink)
                Text("Loading stocks…")
                    .font(MonacoTheme.TypeRole.caption)
                    .foregroundStyle(MonacoTheme.muted)
            }
            .accessibilityIdentifier("assets-search-loading")
        } else if isSearching, listFailed, assets.isEmpty {
            centeredStatus {
                MonacoEmptyStateCard(
                    message: "Could not load stocks.",
                    systemImage: "exclamationmark.triangle"
                )
                Button("Retry") {
                    Task { await loadList(reset: true) }
                }
                .buttonStyle(.monacoSecondary)
            }
        } else if isSearching, !isLoadingList, assets.isEmpty {
            centeredStatus {
                MonacoEmptyStateCard(
                    message: "No matches for that search.",
                    systemImage: "chart.pie"
                )
            }
        } else if !isSearching, popular.isEmpty {
            ScrollView {
                MonacoCard {
                    Text("Popular names show up here once prices load.")
                        .font(MonacoTheme.TypeRole.body)
                        .foregroundStyle(MonacoTheme.muted)
                }
                .accessibilityIdentifier("assets-grid-popular")
            }
            .refreshable {
                await session.refreshPopular(auth: auth)
            }
        } else {
            ScrollView {
                LazyVGrid(columns: gridColumns, spacing: MonacoTheme.Space.s) {
                    ForEach(gridAssets) { asset in
                        Button {
                            selectedSymbol = asset.symbol
                        } label: {
                            assetTile(asset)
                        }
                        .buttonStyle(.plain)
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

    /// 2 columns on a phone portrait sheet; more columns as the canvas widens.
    private var gridColumns: [GridItem] {
        [GridItem(.adaptive(minimum: 152, maximum: 280), spacing: MonacoTheme.Space.s, alignment: .top)]
    }

    private func assetTile(_ asset: MarketAssetDTO) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(asset.displayTicker)
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.75)
            Text(formattedPrice(asset.priceUsdcMicros))
                .font(.subheadline.monospacedDigit())
                .foregroundStyle(MonacoTheme.muted)
            if let change = asset.change24h, !change.isEmpty {
                Text(PercentReturnFormatter.format(change))
                    .font(MonacoTheme.TypeRole.caption.monospacedDigit())
                    .foregroundStyle(MonacoTheme.signed(change))
            }
        }
        .padding(MonacoTheme.Space.m)
        .frame(maxWidth: .infinity, minHeight: 72, alignment: .leading)
        .background(
            MonacoTheme.surface,
            in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
        }
        .opacity(asset.routable ? 1 : 0.7)
    }

    private func formattedPrice(_ micros: Int64?) -> String {
        guard let micros else { return "—" }
        return UsdAmountFormatter.format(micros: micros)
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
