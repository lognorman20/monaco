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

    func search(query: String, offset: Int, limit: Int) async throws -> ListMarketAssetsResponse {
        try await auth.withAccessToken { try await apiClient.listMarketAssets(accessToken: $0, query: query, limit: limit, offset: offset) }
    }

    func popular(limit: Int) async throws -> PopularAssetsResponse {
        try await auth.withAccessToken { try await apiClient.getPopularAssets(accessToken: $0, limit: limit) }
    }
}

/// State for the Stocks tab: the popular strip and debounced, paged catalog search.
///
/// Every page request is stamped with the search generation it was issued under and is dropped
/// when that generation has moved on, so a slow page cannot land in a later query's list. The
/// generation — rather than the query text — is what decides, because the text can come back:
/// typing "AAPL" and deleting the L leaves "AAP" on screen again while page two of the *first*
/// "AAP" is still in flight.
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
    /// A pull-to-refresh that failed while rows were on screen: the rows stay, but silently
    /// retracting the spinner would let the member read stale prices as fresh ones.
    private(set) var refreshFailed = false

    private(set) var popular: [MarketAssetDTO] = []
    private(set) var popularState: PopularState = .loading

    /// Set when the server rejects the session. The data source has already ended it.
    private(set) var sessionExpired = false

    private let dataSource: StocksTabDataSource
    private let pageSize: Int
    private let popularLimit: Int
    private let clock: () -> Date
    private var searchTask: Task<Void, Never>?
    private var popularLoadedAt: Date?
    private var isLoadingPopular = false
    /// Bumped every time the query on screen changes. Pages carry the value they were issued
    /// under; anything older than this is a response nobody is waiting for any more.
    private var generation = 0

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
        // Cancelling the search task does not reach a page already in flight from `loadMore`,
        // `refreshSearch` or `retrySearch`. Retiring the generation does: whatever they were
        // fetching now belongs to a query nobody is reading.
        let gen = nextGeneration()
        guard isSearching else {
            if wasSearching { resetResults() }
            searchState = .idle
            return
        }
        resetResults()
        searchState = .loading
        let term = trimmedQuery
        searchTask = Task { [weak self] in
            try? await Task.sleep(for: Self.searchDebounce)
            guard !Task.isCancelled else { return }
            await self?.runSearch(term, generation: gen)
        }
    }

    func retrySearch() async {
        let term = trimmedQuery
        guard !term.isEmpty else { return }
        searchTask?.cancel()
        searchState = .loading
        await runSearch(term, generation: generation)
    }

    /// Pull-to-refresh: re-reads page one of the query on screen.
    func refreshSearch() async {
        let term = trimmedQuery
        guard !term.isEmpty else { return }
        await runSearch(term, generation: generation)
    }

    func loadMore() async {
        let term = trimmedQuery
        guard !term.isEmpty, hasMore, !isLoadingMore else { return }
        let gen = generation
        isLoadingMore = true
        loadMoreFailed = false
        // Only this page's generation may clear the flag: a later query has its own Load more.
        defer { if gen == generation { isLoadingMore = false } }
        let offset = results.count
        do {
            let page = try await dataSource.search(query: term, offset: offset, limit: pageSize)
            // The query moved on while this page was in flight: it belongs to nobody now.
            guard gen == generation else { return }
            let known = Set(results.map(\.symbol))
            results += page.assets.filter { !known.contains($0.symbol) }
            hasMore = page.hasMore
            searchState = results.isEmpty ? .empty : .results
        } catch {
            guard gen == generation else { return }
            handle(error) { loadMoreFailed = true }
        }
    }

    private func runSearch(_ term: String, generation gen: Int) async {
        do {
            let page = try await dataSource.search(query: term, offset: 0, limit: pageSize)
            guard gen == generation else { return }
            results = page.assets
            hasMore = page.hasMore
            loadMoreFailed = false
            refreshFailed = false
            searchState = page.assets.isEmpty ? .empty : .results
        } catch {
            guard gen == generation else { return }
            handle(error) {
                // A failed refresh keeps the rows already on screen; only an empty list
                // has nothing better to show than the error.
                guard results.isEmpty else {
                    refreshFailed = true
                    return
                }
                hasMore = false
                searchState = .failed
            }
        }
    }

    /// Retires every page currently in flight and returns the generation to stamp the next one.
    private func nextGeneration() -> Int {
        generation &+= 1
        isLoadingMore = false
        return generation
    }

    private func resetResults() {
        results = []
        hasMore = false
        loadMoreFailed = false
        refreshFailed = false
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
