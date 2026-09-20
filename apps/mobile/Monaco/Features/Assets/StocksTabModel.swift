import MonacoCore
import Observation
import SwiftUI

/// Reads for the Stocks tab. The live source calls the API; tests swap in a stub.
@MainActor
protocol StocksTabDataSource {
    func search(query: String, offset: Int, limit: Int) async throws -> ListMarketAssetsResponse
    func popular(limit: Int) async throws -> PopularAssetsResponse
}

@MainActor
struct LiveStocksTabDataSource: StocksTabDataSource {
    let auth: PrivyAuthService
    private let apiClient = MonacoAPIClient()

    init(auth: PrivyAuthService) {
        self.auth = auth
    }

    private func token() throws -> String {
        guard let token = auth.accessToken else { throw MonacoAPIError.missingAccessToken }
        return token
    }

    func search(query: String, offset: Int, limit: Int) async throws -> ListMarketAssetsResponse {
        try await apiClient.listMarketAssets(accessToken: try token(), query: query, limit: limit, offset: offset)
    }

    func popular(limit: Int) async throws -> PopularAssetsResponse {
        try await apiClient.getPopularAssets(accessToken: try token(), limit: limit)
    }
}

/// State for the Stocks tab: the popular strip and debounced, paged catalog search.
///
/// Every page request carries the query it was issued for and is dropped when that query is no
/// longer the one on screen, so a slow page cannot land in a later query's list.
@Observable
@MainActor
final class StocksTabModel {
    enum SearchState: Equatable {
        case idle
        case loading
        case results
        case empty
        case failed
    }

    enum PopularState: Equatable {
        case loading
        case loaded
        case failed
    }

    static let searchDebounce: Duration = .milliseconds(300)
    /// Prices on the tab age badly next to the detail screen's live mark, so a revisit refetches.
    static let popularStaleAfter: TimeInterval = 45

    private(set) var query = ""
    private(set) var searchState: SearchState = .idle
    private(set) var results: [MarketAssetDTO] = []
    private(set) var hasMore = false
    private(set) var isLoadingMore = false
    private(set) var loadMoreFailed = false

    private(set) var popular: [MarketAssetDTO] = []
    private(set) var popularState: PopularState = .loading

    /// Set when the server rejects the session; the view signs out.
    private(set) var sessionExpired = false

    private let dataSource: StocksTabDataSource
    private let pageSize: Int
    private let popularLimit: Int
    private let clock: () -> Date
    private var searchTask: Task<Void, Never>?
    private var popularLoadedAt: Date?
    private var isLoadingPopular = false

    init(
        dataSource: StocksTabDataSource,
        pageSize: Int = 25,
        popularLimit: Int = 10,
        clock: @escaping () -> Date = Date.init
    ) {
        self.dataSource = dataSource
        self.pageSize = pageSize
        self.popularLimit = popularLimit
        self.clock = clock
    }

    var trimmedQuery: String {
        query.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var isSearching: Bool {
        !trimmedQuery.isEmpty
    }

    // MARK: Popular

    /// Paints the shared session cache immediately; the strip still refetches for fresh prices.
    func seedPopular(_ assets: [MarketAssetDTO]) {
        guard popular.isEmpty, !assets.isEmpty else { return }
        popular = assets
        popularState = .loaded
    }

    func refreshPopularIfStale() async {
        guard let loadedAt = popularLoadedAt else {
            await loadPopular()
            return
        }
        guard clock().timeIntervalSince(loadedAt) >= Self.popularStaleAfter else { return }
        await loadPopular()
    }

    func loadPopular() async {
        guard !isLoadingPopular else { return }
        isLoadingPopular = true
        defer { isLoadingPopular = false }
        if popular.isEmpty {
            popularState = .loading
        }
        do {
            popular = try await dataSource.popular(limit: popularLimit).assets
            popularLoadedAt = clock()
            popularState = .loaded
        } catch {
            handle(error) { if popular.isEmpty { popularState = .failed } }
        }
    }

    // MARK: Search

    /// Called on every keystroke. Cancels the in-flight search and waits `searchDebounce`
    /// before asking the server.
    func updateQuery(_ raw: String) {
        let wasSearching = isSearching
        query = raw
        searchTask?.cancel()
        guard isSearching else {
            if wasSearching { resetResults() }
            searchState = .idle
            return
        }
        results = []
        hasMore = false
        loadMoreFailed = false
        searchState = .loading
        let term = trimmedQuery
        searchTask = Task { [weak self] in
            try? await Task.sleep(for: Self.searchDebounce)
            guard !Task.isCancelled else { return }
            await self?.runSearch(term)
        }
    }

    func retrySearch() async {
        let term = trimmedQuery
        guard !term.isEmpty else { return }
        searchTask?.cancel()
        searchState = .loading
        await runSearch(term)
    }

    /// Pull-to-refresh: re-reads page one of the query on screen.
    func refreshSearch() async {
        let term = trimmedQuery
        guard !term.isEmpty else { return }
        await runSearch(term)
    }

    func loadMore() async {
        let term = trimmedQuery
        guard !term.isEmpty, hasMore, !isLoadingMore else { return }
        isLoadingMore = true
        loadMoreFailed = false
        defer { isLoadingMore = false }
        let offset = results.count
        do {
            let page = try await dataSource.search(query: term, offset: offset, limit: pageSize)
            // The query moved on while this page was in flight: it belongs to nobody now.
            guard term == trimmedQuery else { return }
            let known = Set(results.map(\.symbol))
            results += page.assets.filter { !known.contains($0.symbol) }
            hasMore = page.hasMore
            searchState = results.isEmpty ? .empty : .results
        } catch {
            handle(error) { loadMoreFailed = true }
        }
    }

    private func runSearch(_ term: String) async {
        do {
            let page = try await dataSource.search(query: term, offset: 0, limit: pageSize)
            guard term == trimmedQuery else { return }
            results = page.assets
            hasMore = page.hasMore
            loadMoreFailed = false
            searchState = page.assets.isEmpty ? .empty : .results
        } catch {
            guard term == trimmedQuery else { return }
            handle(error) {
                results = []
                hasMore = false
                searchState = .failed
            }
        }
    }

    private func resetResults() {
        results = []
        hasMore = false
        loadMoreFailed = false
    }

    private func handle(_ error: Error, otherwise: () -> Void) {
        if error.isRequestCancellation { return }
        if case MonacoAPIError.httpStatus(401) = error {
            sessionExpired = true
            return
        }
        otherwise()
    }
}
