import MonacoCore
import SwiftUI

/// Market browse: search, popular strip, paginated catalog with prices.
struct AssetsView: View {
    @ObservedObject var auth: PrivyAuthService
    let home: HomeViewDTO
    var onRefresh: () async -> Void = {}

    private let apiClient = MonacoAPIClient()
    private let pageSize = 25
    private let searchDebounceNanos: UInt64 = 300_000_000

    @State private var searchQuery = ""
    @State private var popularAssets: [MarketAssetDTO] = []
    @State private var assets: [MarketAssetDTO] = []
    @State private var hasMoreAssets = false
    @State private var catalogOffset = 0
    @State private var isLoadingPopular = false
    @State private var isLoadingCatalog = false
    @State private var isLoadingMore = false
    @State private var catalogLoadFailed = false
    @State private var popularLoadFailed = false
    @State private var searchTask: Task<Void, Never>?

    var body: some View {
        List {
            Section {
                TextField("Search stocks", text: $searchQuery)
                    .textInputAutocapitalization(.characters)
                    .autocorrectionDisabled()
                    .accessibilityIdentifier("assets-search-field")
            }

            if !popularAssets.isEmpty || isLoadingPopular || popularLoadFailed {
                Section("Popular") {
                    if isLoadingPopular && popularAssets.isEmpty {
                        ProgressView()
                    } else if popularLoadFailed && popularAssets.isEmpty {
                        Label("Could not load popular stocks.", systemImage: "exclamationmark.triangle.fill")
                            .font(.footnote)
                            .foregroundStyle(.orange)
                    } else {
                        ScrollView(.horizontal, showsIndicators: false) {
                            HStack(spacing: 12) {
                                ForEach(popularAssets) { asset in
                                    NavigationLink {
                                        AssetDetailView(auth: auth, home: home, symbol: asset.symbol)
                                    } label: {
                                        popularCard(asset)
                                    }
                                    .buttonStyle(.plain)
                                    .accessibilityIdentifier("assets-popular-\(asset.symbol)")
                                }
                            }
                            .padding(.vertical, 4)
                        }
                    }
                }
            }

            Section("Market") {
                catalogContent

                if hasMoreAssets && !isLoadingCatalog {
                    Button(isLoadingMore ? "Loading…" : "Load more") {
                        Task { await loadCatalog(reset: false) }
                    }
                    .disabled(isLoadingMore)
                    .accessibilityIdentifier("assets-load-more")
                }
            }
        }
        .monacoInsetList()
        .background(MonacoTheme.background)
        .navigationTitle("Assets")
        .onChange(of: searchQuery) { _, _ in
            scheduleCatalogSearch(reset: true)
        }
        .task {
            await loadPopular()
            await loadCatalog(reset: true)
        }
        .refreshable {
            await onRefresh()
            await loadPopular()
            await loadCatalog(reset: true)
        }
        .onDisappear {
            searchTask?.cancel()
        }
    }

    @ViewBuilder
    private func popularCard(_ asset: MarketAssetDTO) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(asset.displaySymbol)
                .font(.subheadline.bold())
                .foregroundStyle(MonacoTheme.primaryText)
            Text(MarketFormatters.usd(fromMicros: asset.priceUsdcMicros))
                .font(.caption.monospacedDigit())
                .foregroundStyle(MonacoTheme.secondaryText)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
    }

    @ViewBuilder
    private var catalogContent: some View {
        if isLoadingCatalog && assets.isEmpty {
            HStack {
                ProgressView()
                Text("Loading stocks…")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        } else if catalogLoadFailed && assets.isEmpty {
            Label("Could not load stocks.", systemImage: "exclamationmark.triangle.fill")
                .font(.footnote)
                .foregroundStyle(.orange)
            Button("Retry") {
                Task { await loadCatalog(reset: true) }
            }
        } else if assets.isEmpty {
            Text(searchQuery.isEmpty ? "No stocks are available right now. Pull down to refresh." : "No matches for \"\(searchQuery)\".")
                .font(.footnote)
                .foregroundStyle(.secondary)
        } else {
            ForEach(assets) { asset in
                NavigationLink {
                    AssetDetailView(auth: auth, home: home, symbol: asset.symbol)
                } label: {
                    assetRow(asset)
                }
                .accessibilityIdentifier("assets-row-\(asset.symbol)")
            }
        }
    }

    @ViewBuilder
    private func assetRow(_ asset: MarketAssetDTO) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text(asset.displaySymbol)
                    .font(.body.bold())
                    .foregroundStyle(MonacoTheme.primaryText)
                Text(asset.displayName)
                    .font(.caption)
                    .foregroundStyle(MonacoTheme.secondaryText)
                if !asset.routable {
                    Text("Unavailable to buy")
                        .font(.caption2)
                        .foregroundStyle(MonacoTheme.warning)
                }
            }
            Spacer()
            VStack(alignment: .trailing, spacing: 4) {
                Text(MarketFormatters.usd(fromMicros: asset.priceUsdcMicros))
                    .font(.subheadline.monospacedDigit())
                    .foregroundStyle(MonacoTheme.primaryText)
                if let change = MarketFormatters.percentChange(asset.change24h) {
                    Text(change)
                        .font(.caption2.monospacedDigit())
                        .foregroundStyle(change.hasPrefix("-") ? MonacoTheme.warning : MonacoTheme.success)
                }
            }
        }
        .opacity(asset.routable ? 1 : 0.7)
    }

    private func scheduleCatalogSearch(reset: Bool) {
        searchTask?.cancel()
        searchTask = Task {
            do {
                try await Task.sleep(nanoseconds: searchDebounceNanos)
            } catch {
                return
            }
            guard !Task.isCancelled else { return }
            await loadCatalog(reset: reset)
        }
    }

    private func loadPopular() async {
        guard let token = auth.accessToken else { return }
        isLoadingPopular = true
        popularLoadFailed = false
        defer { isLoadingPopular = false }

        do {
            let response = try await apiClient.getPopularMarketAssets(accessToken: token, limit: 10)
            popularAssets = response.assets
        } catch {
            popularLoadFailed = true
            popularAssets = []
        }
    }

    private func loadCatalog(reset: Bool) async {
        guard let token = auth.accessToken else { return }
        if reset {
            isLoadingCatalog = true
            catalogLoadFailed = false
            catalogOffset = 0
            hasMoreAssets = false
            if !assets.isEmpty {
                assets = []
            }
        } else {
            isLoadingMore = true
        }
        defer {
            isLoadingCatalog = false
            isLoadingMore = false
        }

        let query = searchQuery.trimmingCharacters(in: .whitespacesAndNewlines)
        let offset = reset ? 0 : catalogOffset

        do {
            let response = try await apiClient.listMarketAssets(
                accessToken: token,
                query: query,
                limit: pageSize,
                offset: offset
            )
            guard !Task.isCancelled, query == searchQuery.trimmingCharacters(in: .whitespacesAndNewlines) else { return }
            if reset {
                assets = response.assets
            } else {
                assets.append(contentsOf: response.assets)
            }
            catalogOffset = assets.count
            hasMoreAssets = response.hasMore
        } catch {
            guard !Task.isCancelled else { return }
            if reset {
                catalogLoadFailed = true
                assets = []
            }
        }
    }
}

#Preview {
    NavigationStack {
        AssetsView(
            auth: PrivyAuthService(),
            home: HomeViewDTO(groups: [], people: [])
        )
        .monacoRootAppearance()
    }
}
