import MonacoCore
import SwiftUI

/// Market browse: horizontal popular strip, paginated list, and search.
struct AssetsTabView: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session

    private let apiClient = MonacoAPIClient()
    private let pageSize = 25
    private let searchDebounceNanos: UInt64 = 300_000_000

    @State private var searchQuery = ""
    @State private var searchAssets: [MarketAssetDTO] = []
    @State private var browseAssets: [MarketAssetDTO] = []
    @State private var searchHasMore = false
    @State private var browseHasMore = false
    @State private var searchOffset = 0
    @State private var browseOffset = 0
    @State private var isLoadingSearch = false
    @State private var isLoadingBrowse = false
    @State private var isLoadingMore = false
    @State private var searchFailed = false
    @State private var browseFailed = false
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

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            MonacoSearchField(
                placeholder: "Search a stock",
                text: $searchQuery,
                isEnabled: true
            )
            .accessibilityIdentifier("assets-search-field")

            contentRegion
        }
        .padding(MonacoTheme.Space.m)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .monacoCanvas()
        .foregroundStyle(MonacoTheme.ink)
        .navigationTitle("Assets")
        .navigationBarTitleDisplayMode(.inline)
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
            if browseAssets.isEmpty && !isSearching {
                await loadBrowse(reset: true)
            }
        }
        .onDisappear {
            searchTask?.cancel()
        }
    }

    @ViewBuilder
    private var contentRegion: some View {
        if isSearching {
            searchRegion
        } else {
            browseRegion
        }
    }

    @ViewBuilder
    private var browseRegion: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
                popularStrip

                if isLoadingBrowse, browseAssets.isEmpty, !browseFailed {
                    browseLoadingState
                } else if browseFailed, browseAssets.isEmpty {
                    browseErrorState
                } else if !isLoadingBrowse, browseAssets.isEmpty {
                    MonacoEmptyStateCard(
                        message: "No stocks to browse right now.",
                        systemImage: "chart.pie"
                    )
                } else {
                    Text("All stocks")
                        .font(MonacoTheme.TypeRole.title)
                        .foregroundStyle(MonacoTheme.ink)

                    LazyVStack(spacing: MonacoTheme.Space.s) {
                        ForEach(browseAssets) { asset in
                            assetListRow(asset, identifierPrefix: "assets-browse")
                        }
                    }
                    .accessibilityIdentifier("assets-browse-list")

                    if browseHasMore {
                        Button(isLoadingMore ? "Loading…" : "Load more") {
                            Task { await loadBrowse(reset: false) }
                        }
                        .buttonStyle(.monacoSecondary)
                        .disabled(isLoadingMore)
                        .padding(.top, MonacoTheme.Space.s)
                        .accessibilityIdentifier("assets-browse-load-more")
                    }
                }
            }
        }
        .refreshable {
            await session.refreshPopular(auth: auth)
            await loadBrowse(reset: true)
        }
    }

    @ViewBuilder
    private var searchRegion: some View {
        if isLoadingSearch, searchAssets.isEmpty, !searchFailed {
            centeredStatus {
                ProgressView()
                    .tint(MonacoTheme.ink)
                Text("Loading stocks…")
                    .font(MonacoTheme.TypeRole.caption)
                    .foregroundStyle(MonacoTheme.muted)
            }
            .accessibilityIdentifier("assets-search-loading")
        } else if searchFailed, searchAssets.isEmpty {
            centeredStatus {
                MonacoEmptyStateCard(
                    message: "Could not load stocks.",
                    systemImage: "exclamationmark.triangle"
                )
                Button("Retry") {
                    Task { await loadSearch(reset: true) }
                }
                .buttonStyle(.monacoSecondary)
            }
        } else if !isLoadingSearch, searchAssets.isEmpty {
            centeredStatus {
                MonacoEmptyStateCard(
                    message: "No matches for that search.",
                    systemImage: "chart.pie"
                )
            }
        } else {
            ScrollView {
                LazyVStack(spacing: MonacoTheme.Space.s) {
                    ForEach(searchAssets) { asset in
                        assetListRow(asset, identifierPrefix: "assets-row")
                    }
                }
                .accessibilityIdentifier("assets-grid-search")

                if searchHasMore {
                    Button(isLoadingMore ? "Loading…" : "Load more") {
                        Task { await loadSearch(reset: false) }
                    }
                    .buttonStyle(.monacoSecondary)
                    .disabled(isLoadingMore)
                    .padding(.top, MonacoTheme.Space.s)
                    .accessibilityIdentifier("assets-load-more")
                }
            }
            .refreshable {
                await loadSearch(reset: true)
            }
        }
    }

    @ViewBuilder
    private var popularStrip: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text("Popular")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)

            if popular.isEmpty {
                MonacoCard {
                    Text("Popular names show up here once prices load.")
                        .font(MonacoTheme.TypeRole.body)
                        .foregroundStyle(MonacoTheme.muted)
                }
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    LazyHStack(spacing: MonacoTheme.Space.s) {
                        ForEach(popular) { asset in
                            Button {
                                selectedSymbol = asset.symbol
                            } label: {
                                popularCard(asset)
                            }
                            .buttonStyle(.plain)
                            .accessibilityIdentifier("assets-popular-\(asset.symbol)")
                        }
                    }
                    .padding(.vertical, 2)
                }
                .accessibilityIdentifier("assets-popular-strip")
            }
        }
    }

    private var browseLoadingState: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            ProgressView()
                .tint(MonacoTheme.ink)
            Text("Loading stocks…")
                .font(MonacoTheme.TypeRole.caption)
                .foregroundStyle(MonacoTheme.muted)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityIdentifier("assets-browse-loading")
    }

    private var browseErrorState: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoEmptyStateCard(
                message: "Could not load stocks.",
                systemImage: "exclamationmark.triangle"
            )
            Button("Retry") {
                Task { await loadBrowse(reset: true) }
            }
            .buttonStyle(.monacoSecondary)
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

    private func popularCard(_ asset: MarketAssetDTO) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            AssetLogoView(logoURL: asset.logoUrl, symbol: asset.displayTicker, size: 40)
            Text(asset.displayTicker)
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(1)
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
        .frame(width: 132, alignment: .leading)
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

    private func assetListRow(_ asset: MarketAssetDTO, identifierPrefix: String) -> some View {
        Button {
            selectedSymbol = asset.symbol
        } label: {
            HStack(spacing: MonacoTheme.Space.m) {
                AssetLogoView(logoURL: asset.logoUrl, symbol: asset.displayTicker, size: 44)
                VStack(alignment: .leading, spacing: 2) {
                    Text(asset.displayTicker)
                        .font(MonacoTheme.TypeRole.title)
                        .foregroundStyle(MonacoTheme.ink)
                        .lineLimit(1)
                    Text(asset.displayName)
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(MonacoTheme.muted)
                        .lineLimit(1)
                }
                Spacer(minLength: MonacoTheme.Space.s)
                VStack(alignment: .trailing, spacing: 2) {
                    Text(formattedPrice(asset.priceUsdcMicros))
                        .font(.subheadline.monospacedDigit())
                        .foregroundStyle(MonacoTheme.ink)
                    if let change = asset.change24h, !change.isEmpty {
                        Text(PercentReturnFormatter.format(change))
                            .font(MonacoTheme.TypeRole.caption.monospacedDigit())
                            .foregroundStyle(MonacoTheme.signed(change))
                    }
                }
            }
            .padding(MonacoTheme.Space.m)
            .frame(maxWidth: .infinity, alignment: .leading)
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
        .buttonStyle(.plain)
        .accessibilityIdentifier("\(identifierPrefix)-\(asset.symbol)")
    }

    private func formattedPrice(_ micros: Int64?) -> String {
        guard let micros else { return "—" }
        return UsdAmountFormatter.format(micros: micros)
    }

    private func scheduleListSearch() {
        searchTask?.cancel()
        let query = trimmedQuery
        if query.isEmpty {
            searchAssets = []
            searchHasMore = false
            searchOffset = 0
            isLoadingSearch = false
            searchFailed = false
            if browseAssets.isEmpty {
                Task { await loadBrowse(reset: true) }
            }
            return
        }
        isLoadingSearch = true
        searchAssets = []
        searchFailed = false
        searchTask = Task {
            do {
                try await Task.sleep(nanoseconds: searchDebounceNanos)
            } catch {
                return
            }
            guard !Task.isCancelled else { return }
            await loadSearch(reset: true)
        }
    }

    private func loadSearch(reset: Bool) async {
        guard let token = auth.accessToken else { return }
        let query = trimmedQuery
        guard !query.isEmpty else { return }

        if reset {
            isLoadingSearch = true
            searchFailed = false
            searchOffset = 0
            searchHasMore = false
        } else {
            isLoadingMore = true
        }
        defer {
            isLoadingSearch = false
            isLoadingMore = false
        }

        let offset = reset ? 0 : searchOffset
        do {
            let response = try await apiClient.listMarketAssets(
                accessToken: token,
                query: query,
                limit: pageSize,
                offset: offset
            )
            if reset {
                searchAssets = response.assets
            } else {
                searchAssets.append(contentsOf: response.assets)
            }
            searchOffset = searchAssets.count
            searchHasMore = response.hasMore
        } catch {
            if error.isRequestCancellation { return }
            if reset {
                searchFailed = true
                searchAssets = []
            }
        }
    }

    private func loadBrowse(reset: Bool) async {
        guard let token = auth.accessToken else { return }
        guard trimmedQuery.isEmpty else { return }

        if reset {
            isLoadingBrowse = true
            browseFailed = false
            browseOffset = 0
            browseHasMore = false
        } else {
            isLoadingMore = true
        }
        defer {
            isLoadingBrowse = false
            isLoadingMore = false
        }

        let offset = reset ? 0 : browseOffset
        do {
            let response = try await apiClient.listMarketAssets(
                accessToken: token,
                query: "",
                limit: pageSize,
                offset: offset
            )
            if reset {
                browseAssets = response.assets
            } else {
                browseAssets.append(contentsOf: response.assets)
            }
            browseOffset = browseAssets.count
            browseHasMore = response.hasMore
        } catch {
            if error.isRequestCancellation { return }
            if reset {
                browseFailed = true
                browseAssets = []
            }
        }
    }
}

private struct AssetLogoView: View {
    let logoURL: String?
    let symbol: String
    var size: CGFloat = 44

    private var resolvedURL: URL? {
        let trimmed = logoURL?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !trimmed.isEmpty else { return nil }
        return URL(string: trimmed)
    }

    var body: some View {
        Group {
            if let resolvedURL {
                AsyncImage(url: resolvedURL, transaction: Transaction(animation: .easeOut(duration: 0.2))) { phase in
                    switch phase {
                    case .success(let image):
                        image
                            .resizable()
                            .scaledToFill()
                    case .failure:
                        placeholder
                    default:
                        placeholder.overlay {
                            ProgressView()
                                .controlSize(.mini)
                                .tint(MonacoTheme.muted)
                        }
                    }
                }
            } else {
                placeholder
            }
        }
        .frame(width: size, height: size)
        .clipShape(RoundedRectangle(cornerRadius: size * 0.22, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: size * 0.22, style: .continuous)
                .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
        }
        .accessibilityHidden(true)
    }

    private var placeholder: some View {
        ZStack {
            RoundedRectangle(cornerRadius: size * 0.22, style: .continuous)
                .fill(MonacoTheme.canvasWash)
            Text(String(symbol.prefix(1)))
                .font(.custom("AvenirNext-DemiBold", size: size * 0.38))
                .foregroundStyle(MonacoTheme.accent)
        }
    }
}
