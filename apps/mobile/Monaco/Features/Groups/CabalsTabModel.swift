import MonacoCore
import Observation
import SwiftUI

/// Reads for the Cabals tab. The live source calls the API; Debug builds can
/// swap in sample data for screenshots.
@MainActor
protocol CabalsTabDataSource {
    func leaderboard() async throws -> GroupLeaderboardResponseDTO
    func pnlHistory(range: GroupPnLRange) async throws -> MyGroupsPnLHistoryDTO
    func search(query: String, cursor: String?) async throws -> GroupSearchResponseDTO
}

@MainActor
struct LiveCabalsTabDataSource: CabalsTabDataSource {
    let auth: DynamicAuthService
    private let apiClient = MonacoAPIClient()

    init(auth: DynamicAuthService) {
        self.auth = auth
    }

    private func token() throws -> String {
        guard let token = auth.accessToken else { throw MonacoAPIError.missingAccessToken }
        return token
    }

    func leaderboard() async throws -> GroupLeaderboardResponseDTO {
        try await apiClient.groupLeaderboard(accessToken: try token(), limit: 20)
    }

    func pnlHistory(range: GroupPnLRange) async throws -> MyGroupsPnLHistoryDTO {
        try await apiClient.myGroupsPnLHistory(accessToken: try token(), range: range)
    }

    func search(query: String, cursor: String?) async throws -> GroupSearchResponseDTO {
        try await apiClient.searchGroups(accessToken: try token(), query: query, limit: 20, cursor: cursor)
    }
}

/// State for the Cabals tab: P&L chart, platform board, and debounced search.
@Observable
@MainActor
final class CabalsTabModel {
    enum SearchState: Equatable {
        case idle
        case tooShort
        case loading
        case results
        case empty
        case failed
    }

    static let searchDebounce: Duration = .milliseconds(300)

    var range: GroupPnLRange = .oneMonth
    private(set) var series: [GroupPnLSeriesDTO] = []
    private(set) var isChartLoading = false
    private(set) var chartFailed = false

    private(set) var leaderboard: [GroupLeaderboardRowDTO] = []
    private(set) var isLeaderboardLoading = false
    private(set) var leaderboardFailed = false

    private(set) var query = ""
    private(set) var searchState: SearchState = .idle
    private(set) var results: [GroupDiscoveryRowDTO] = []
    private(set) var nextCursor: String?
    private(set) var isLoadingMore = false

    /// Set when the server rejects the session; the view signs out.
    private(set) var sessionExpired = false

    private let dataSource: CabalsTabDataSource
    private var searchTask: Task<Void, Never>?
    private var chartTask: Task<Void, Never>?

    init(dataSource: CabalsTabDataSource) {
        self.dataSource = dataSource
    }

    var isSearching: Bool {
        !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    // MARK: Chart + board

    func reload() async {
        async let chart: Void = loadChart()
        async let board: Void = loadLeaderboard()
        _ = await (chart, board)
    }

    func selectRange(_ newRange: GroupPnLRange) {
        guard newRange != range else { return }
        range = newRange
        chartTask?.cancel()
        chartTask = Task { await loadChart() }
    }

    func loadChart() async {
        let requested = range
        isChartLoading = series.isEmpty
        defer { isChartLoading = false }
        do {
            let history = try await dataSource.pnlHistory(range: requested)
            guard requested == range, !Task.isCancelled else { return }
            series = history.series
            chartFailed = false
        } catch {
            handle(error) { chartFailed = true }
        }
    }

    func loadLeaderboard() async {
        isLeaderboardLoading = leaderboard.isEmpty
        defer { isLeaderboardLoading = false }
        do {
            leaderboard = try await dataSource.leaderboard().groups
            leaderboardFailed = false
        } catch {
            handle(error) { leaderboardFailed = true }
        }
    }

    // MARK: Search

    /// Called on every keystroke. Cancels the in-flight search and waits
    /// `searchDebounce` before asking the server.
    func updateQuery(_ raw: String) {
        query = raw
        searchTask?.cancel()
        nextCursor = nil
        guard isSearching else {
            searchState = .idle
            results = []
            return
        }
        guard let normalized = GroupSearchQuery.normalized(raw) else {
            searchState = .tooShort
            results = []
            return
        }
        searchState = .loading
        searchTask = Task { [weak self] in
            try? await Task.sleep(for: Self.searchDebounce)
            guard !Task.isCancelled else { return }
            await self?.runSearch(normalized)
        }
    }

    /// Re-runs the current query (retry after a failure).
    func retrySearch() {
        updateQuery(query)
    }

    func clearSearch() {
        updateQuery("")
    }

    private func runSearch(_ normalized: String) async {
        do {
            let page = try await dataSource.search(query: normalized, cursor: nil)
            guard !Task.isCancelled, GroupSearchQuery.normalized(query) == normalized else { return }
            results = page.groups
            nextCursor = page.nextCursor
            searchState = page.groups.isEmpty ? .empty : .results
        } catch {
            guard !Task.isCancelled, !error.isRequestCancellation else { return }
            handle(error) { searchState = .failed }
        }
    }

    func loadMoreResults() async {
        guard let cursor = nextCursor, !isLoadingMore, let normalized = GroupSearchQuery.normalized(query) else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page = try await dataSource.search(query: normalized, cursor: cursor)
            guard GroupSearchQuery.normalized(query) == normalized else { return }
            let known = Set(results.map(\.groupID))
            results += page.groups.filter { !known.contains($0.groupID) }
            nextCursor = page.nextCursor
        } catch {
            handle(error) {}
        }
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
