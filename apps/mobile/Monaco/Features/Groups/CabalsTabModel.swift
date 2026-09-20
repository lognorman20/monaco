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
    let auth: PrivyAuthService
    private let apiClient = MonacoAPIClient()

    init(auth: PrivyAuthService) {
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

    /// A scrub-worthy line needs at least 3 points, and a comparison needs at
    /// least two cabals. Lives here, not in the view, so the chart section and
    /// its tests agree on one rule.
    static func isChartable(_ series: [GroupPnLSeriesDTO]) -> Bool {
        GroupPnLChartModel.drawable(series).filter { $0.points.count >= 3 }.count >= 2
    }

    private(set) var range: GroupPnLRange = .oneMonth
    private(set) var series: [GroupPnLSeriesDTO] = []
    /// The range a load is in flight for, or nil. A superseded load can no
    /// longer clear a newer load's spinner.
    private(set) var loadingRange: GroupPnLRange?
    private(set) var chartFailed = false
    /// Some range has drawn a real chart in this session, so the section has
    /// earned its place. Switching to a range with thin history then shows an
    /// in-card note instead of deleting the section and its range picker.
    private(set) var hasChartableHistory = false

    private(set) var leaderboard: [GroupLeaderboardRowDTO] = []
    private(set) var isLeaderboardLoading = false
    private(set) var leaderboardFailed = false

    private(set) var query = ""
    private(set) var searchState: SearchState = .idle
    private(set) var results: [GroupDiscoveryRowDTO] = []
    private(set) var nextCursor: String?
    private(set) var isLoadingMore = false
    private(set) var loadMoreFailed = false

    /// Set when the server rejects the session; the view signs out.
    private(set) var sessionExpired = false

    private let dataSource: CabalsTabDataSource
    private var searchTask: Task<Void, Never>?
    private var chartTask: Task<Void, Never>?
    private var reloadTask: Task<Void, Never>?
    private var loadMoreTask: Task<Void, Never>?
    /// Bumped on every search the member starts. A page that belongs to an
    /// older search cannot append itself to the current results.
    private var searchGeneration = 0
    /// Bumped on every board load, so a slow response cannot overwrite a newer one.
    private var leaderboardGeneration = 0

    init(dataSource: CabalsTabDataSource) {
        self.dataSource = dataSource
    }

    var isSearching: Bool {
        !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    /// True while the chart has nothing to draw and a first load is running.
    var isChartLoading: Bool {
        loadingRange != nil && series.isEmpty
    }

    /// True while a load for the selected range is in flight over lines that are
    /// already drawn — a range switch, or a refresh. The chart on screen is not
    /// yet the chart the highlighted chip promises.
    var isChartReloading: Bool {
        guard let loadingRange else { return false }
        return !series.isEmpty && loadingRange == range
    }

    // MARK: Chart + board

    /// Reloads the chart and the board. Cancel-and-replace: a reload started
    /// while another is in flight supersedes it, so a membership change and a
    /// pull-to-refresh in the same breath cost one round of requests, not two.
    func reload(hasCabals: Bool = true) async {
        reloadTask?.cancel()
        let task = Task { [weak self] in
            guard let self else { return }
            // No cabals means no chart on screen; don't pay for the history read.
            guard hasCabals else {
                await self.loadLeaderboard()
                return
            }
            async let chart: Void = self.loadChart()
            async let board: Void = self.loadLeaderboard()
            _ = await (chart, board)
        }
        reloadTask = task
        await task.value
    }

    func selectRange(_ newRange: GroupPnLRange) {
        guard newRange != range else { return }
        range = newRange
        chartTask?.cancel()
        chartTask = Task { await loadChart() }
    }

    func loadChart() async {
        let requested = range
        loadingRange = requested
        defer {
            // Only the newest load clears the flag; a superseded one leaves it be.
            if loadingRange == requested { loadingRange = nil }
        }
        do {
            let history = try await dataSource.pnlHistory(range: requested)
            guard requested == range, !Task.isCancelled else { return }
            series = history.series
            hasChartableHistory = hasChartableHistory || Self.isChartable(history.series)
            chartFailed = false
        } catch {
            guard requested == range, !Task.isCancelled else { return }
            handle(error) { chartFailed = true }
        }
    }

    func loadLeaderboard() async {
        leaderboardGeneration += 1
        let generation = leaderboardGeneration
        isLeaderboardLoading = leaderboard.isEmpty
        defer {
            if generation == leaderboardGeneration { isLeaderboardLoading = false }
        }
        do {
            let rows = try await dataSource.leaderboard().groups
            guard generation == leaderboardGeneration, !Task.isCancelled else { return }
            leaderboard = rows
            leaderboardFailed = false
        } catch {
            guard generation == leaderboardGeneration, !Task.isCancelled else { return }
            handle(error) { leaderboardFailed = true }
        }
    }

    /// The viewer just joined this cabal. Patches the rows on screen so the
    /// board and the search results say "You're in" straight away, instead of
    /// waiting for the next reload — or, for search, for the member to retype.
    func markJoined(groupID: String) {
        leaderboard = leaderboard.map { $0.groupID == groupID ? $0.markingJoined() : $0 }
        results = results.map { $0.groupID == groupID ? $0.markingJoined() : $0 }
    }

    // MARK: Search

    /// Called on every keystroke. Cancels the in-flight search and waits
    /// `searchDebounce` before asking the server.
    func updateQuery(_ raw: String) {
        query = raw
        cancelSearchWork()
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
        let generation = searchGeneration
        searchTask = Task { [weak self] in
            try? await Task.sleep(for: Self.searchDebounce)
            guard !Task.isCancelled else { return }
            await self?.runSearch(normalized, generation: generation)
        }
    }

    /// Re-runs the current query after a failure. Skips the debounce: the
    /// member already waited once and tapped "Try again" deliberately.
    func retrySearch() {
        guard let normalized = GroupSearchQuery.normalized(query) else {
            updateQuery(query)
            return
        }
        cancelSearchWork()
        searchState = .loading
        let generation = searchGeneration
        searchTask = Task { [weak self] in
            await self?.runSearch(normalized, generation: generation)
        }
    }

    func clearSearch() {
        updateQuery("")
    }

    /// Cancels everything the previous query started and resets its paging
    /// state, so a stale "Show more" can neither land nor keep the new
    /// results' button disabled.
    private func cancelSearchWork() {
        searchGeneration += 1
        searchTask?.cancel()
        loadMoreTask?.cancel()
        loadMoreTask = nil
        isLoadingMore = false
        loadMoreFailed = false
        nextCursor = nil
    }

    private func runSearch(_ normalized: String, generation: Int) async {
        do {
            let page = try await dataSource.search(query: normalized, cursor: nil)
            guard !Task.isCancelled, generation == searchGeneration else { return }
            results = page.groups
            nextCursor = page.nextCursor
            searchState = page.groups.isEmpty ? .empty : .results
        } catch {
            guard !Task.isCancelled, generation == searchGeneration else { return }
            handle(error) { searchState = .failed }
        }
    }

    /// Loads the next page of search results. The model owns the task so that
    /// changing the query cancels it.
    func loadMore() {
        guard let cursor = nextCursor, !isLoadingMore,
              let normalized = GroupSearchQuery.normalized(query) else { return }
        let generation = searchGeneration
        isLoadingMore = true
        loadMoreFailed = false
        loadMoreTask = Task { [weak self] in
            await self?.loadMoreResults(normalized, cursor: cursor, generation: generation)
        }
    }

    private func loadMoreResults(_ normalized: String, cursor: String, generation: Int) async {
        defer {
            if generation == searchGeneration { isLoadingMore = false }
        }
        do {
            let page = try await dataSource.search(query: normalized, cursor: cursor)
            // The cursor check catches a retry of the same query: the page that
            // comes back must still be the page we are waiting for.
            guard !Task.isCancelled, generation == searchGeneration, nextCursor == cursor else { return }
            let known = Set(results.map(\.groupID))
            results += page.groups.filter { !known.contains($0.groupID) }
            nextCursor = page.nextCursor
        } catch {
            guard !Task.isCancelled, generation == searchGeneration else { return }
            handle(error) { loadMoreFailed = true }
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

private extension GroupLeaderboardRowDTO {
    func markingJoined() -> GroupLeaderboardRowDTO {
        GroupLeaderboardRowDTO(
            rank: rank, groupID: groupID, name: name, memberCount: memberCount,
            potValueUsd: potValueUsd, percentReturn: percentReturn, dollarPnl: dollarPnl,
            isJoined: true, joinMode: joinMode
        )
    }
}

private extension GroupDiscoveryRowDTO {
    func markingJoined() -> GroupDiscoveryRowDTO {
        GroupDiscoveryRowDTO(
            groupID: groupID, name: name, memberCount: memberCount, potValueUsd: potValueUsd,
            percentReturn: percentReturn, dollarPnl: dollarPnl, isJoined: true, joinMode: joinMode
        )
    }
}
