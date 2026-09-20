import Foundation
import MonacoCore
import Testing
@testable import Monaco

@MainActor
private final class RecordingDataSource: CabalsTabDataSource {
    var searches: [(query: String, cursor: String?)] = []
    var searchError: Error?
    var pnlRanges: [GroupPnLRange] = []
    var boardRows: [GroupLeaderboardRowDTO] = []
    var boardLoads = 0
    /// Points per cabal, per range, for the chart tests.
    var seriesByRange: [GroupPnLRange: [GroupPnLSeriesDTO]] = [:]
    /// Held open so a test can drive what happens while a search is in flight.
    var searchGate: (() async -> Void)?
    /// Cursor the next page comes back with.
    var nextCursor: String?

    var queries: [String] { searches.map(\.query) }

    func leaderboard() async throws -> GroupLeaderboardResponseDTO {
        boardLoads += 1
        return GroupLeaderboardResponseDTO(groups: boardRows)
    }

    func pnlHistory(range: GroupPnLRange) async throws -> MyGroupsPnLHistoryDTO {
        pnlRanges.append(range)
        return MyGroupsPnLHistoryDTO(range: range.rawValue, series: seriesByRange[range] ?? [])
    }

    func search(query: String, cursor: String?) async throws -> GroupSearchResponseDTO {
        searches.append((query, cursor))
        if let searchGate { await searchGate() }
        if let searchError { throw searchError }
        let suffix = cursor.map { "-\($0)" } ?? ""
        let row = GroupDiscoveryRowDTO(
            groupID: "g-\(query)\(suffix)", name: "Weekend \(query)", memberCount: 2, potValueUsd: "10.00",
            percentReturn: nil, dollarPnl: "+0.00", isJoined: false, joinMode: .open
        )
        return GroupSearchResponseDTO(groups: query == "none" ? [] : [row], nextCursor: nextCursor)
    }
}

private func sampleSeries(id: String, points: Int) -> GroupPnLSeriesDTO {
    let now = Date()
    return GroupPnLSeriesDTO(
        groupID: id,
        name: "Cabal \(id)",
        range: GroupPnLRange.oneMonth.rawValue,
        points: (0..<points).map { index in
            GroupPnLPointDTO(
                at: now.addingTimeInterval(TimeInterval(-3600 * (points - index))),
                potValueUsd: "100.00",
                netInUsd: "100.00",
                dollarPnl: "+1.00"
            )
        }
    )
}

@MainActor
struct CabalsTabModelTests {
    private func settle() async throws {
        try await Task.sleep(for: .milliseconds(450))
    }

    @Test func typingQuicklySendsOnlyTheLastQuery() async throws {
        // Arrange
        let source = RecordingDataSource()
        let model = CabalsTabModel(dataSource: source)

        // Act
        model.updateQuery("we")
        model.updateQuery("wee")
        model.updateQuery("week")
        try await settle()

        // Assert
        #expect(source.queries == ["week"])
        #expect(model.searchState == .results)
        #expect(model.results.map(\.groupID) == ["g-week"])
    }

    @Test func singleCharacterNeverHitsTheServer() async throws {
        let source = RecordingDataSource()
        let model = CabalsTabModel(dataSource: source)

        model.updateQuery(" w ")
        try await settle()

        #expect(source.queries.isEmpty)
        #expect(model.searchState == .tooShort)
    }

    @Test func clearingTheQueryCancelsThePendingSearch() async throws {
        let source = RecordingDataSource()
        let model = CabalsTabModel(dataSource: source)

        model.updateQuery("weekend")
        model.clearSearch()
        try await settle()

        #expect(source.queries.isEmpty)
        #expect(model.searchState == .idle)
        #expect(!model.isSearching)
    }

    @Test func noMatchesShowsEmptyState() async throws {
        let source = RecordingDataSource()
        let model = CabalsTabModel(dataSource: source)

        model.updateQuery("none")
        try await settle()

        #expect(model.searchState == .empty)
    }

    @Test func serverFailureShowsRetryableError() async throws {
        let source = RecordingDataSource()
        source.searchError = Monaco.MonacoAPIError.httpStatus(500)
        let model = CabalsTabModel(dataSource: source)

        model.updateQuery("weekend")
        try await settle()
        #expect(model.searchState == .failed)

        source.searchError = nil
        model.retrySearch()
        try await settle()
        #expect(model.searchState == .results)
    }

    @Test func expiredSessionIsReportedInsteadOfAnError() async throws {
        let source = RecordingDataSource()
        source.searchError = Monaco.MonacoAPIError.httpStatus(401)
        let model = CabalsTabModel(dataSource: source)

        model.updateQuery("weekend")
        try await settle()

        #expect(model.sessionExpired)
    }

    @Test func changingRangeReloadsTheChart() async throws {
        let source = RecordingDataSource()
        let model = CabalsTabModel(dataSource: source)

        model.selectRange(.threeMonths)
        try await settle()

        #expect(source.pnlRanges == [.threeMonths])
        #expect(model.range == .threeMonths)
    }

    // MARK: - P&L chart (#294)

    @Test func aRangeWithThinHistoryKeepsTheChartSection() async throws {
        // Arrange: 1M has two full lines, 1D has a single point per cabal —
        // exactly what a quiet day looks like for young cabals.
        let source = RecordingDataSource()
        source.seriesByRange = [
            .oneMonth: [sampleSeries(id: "a", points: 8), sampleSeries(id: "b", points: 6)],
            .oneDay: [sampleSeries(id: "a", points: 1), sampleSeries(id: "b", points: 1)],
        ]
        let model = CabalsTabModel(dataSource: source)

        // Act
        await model.reload()
        #expect(CabalsTabModel.isChartable(model.series))
        model.selectRange(.oneDay)
        try await settle()

        // Assert: the day itself has nothing to draw, but the section — and so
        // the range picker that gets you back out — has earned its place.
        #expect(!CabalsTabModel.isChartable(model.series))
        #expect(model.hasChartableHistory)
    }

    @Test func aSupersededRangeLoadDoesNotClearTheNewerSpinner() async throws {
        let source = RecordingDataSource()
        let model = CabalsTabModel(dataSource: source)

        model.selectRange(.oneDay)
        model.selectRange(.threeMonths)
        try await settle()

        #expect(model.range == .threeMonths)
        #expect(model.loadingRange == nil)
    }

    // MARK: - Membership (#293)

    @Test func joiningPatchesTheBoardAndTheSearchResults() async throws {
        // Arrange
        let source = RecordingDataSource()
        source.boardRows = [
            GroupLeaderboardRowDTO(
                rank: 1, groupID: "g-week", name: "Weekend week", memberCount: 2, potValueUsd: "10.00",
                percentReturn: nil, dollarPnl: "+0.00", isJoined: false, joinMode: .open
            )
        ]
        let model = CabalsTabModel(dataSource: source)
        await model.reload()
        model.updateQuery("week")
        try await settle()
        #expect(model.results.first?.isJoined == false)

        // Act
        model.markJoined(groupID: "g-week")

        // Assert: both lists say "you're in" without waiting for a reload or
        // for the member to retype the query.
        #expect(model.results.first?.isJoined == true)
        #expect(model.leaderboard.first?.isJoined == true)
    }

    // MARK: - Search pagination (#332)

    @Test func changingTheQueryCancelsAnInFlightLoadMore() async throws {
        // Arrange: page one comes back with a cursor, then the next page hangs.
        let source = RecordingDataSource()
        source.nextCursor = "cursor-1"
        let model = CabalsTabModel(dataSource: source)
        model.updateQuery("week")
        try await settle()
        #expect(model.nextCursor == "cursor-1")

        source.searchGate = { try? await Task.sleep(for: .milliseconds(600)) }
        model.loadMore()
        #expect(model.isLoadingMore)

        // Act: the member edits the query while page two is still out.
        source.searchGate = nil
        source.nextCursor = nil
        model.updateQuery("rent")
        try await settle()

        // Assert: the new results are not stuck behind the orphaned page.
        #expect(!model.isLoadingMore)
        #expect(model.results.map(\.groupID) == ["g-rent"])
    }

    @Test func aFailedLoadMoreSaysSoInsteadOfGoingQuiet() async throws {
        let source = RecordingDataSource()
        source.nextCursor = "cursor-1"
        let model = CabalsTabModel(dataSource: source)
        model.updateQuery("week")
        try await settle()

        source.searchError = Monaco.MonacoAPIError.httpStatus(500)
        model.loadMore()
        try await settle()

        #expect(model.loadMoreFailed)
        #expect(!model.isLoadingMore)
        #expect(model.results.map(\.groupID) == ["g-week"])
    }

    @Test func retryingASearchSkipsTheDebounce() async throws {
        let source = RecordingDataSource()
        source.searchError = Monaco.MonacoAPIError.httpStatus(500)
        let model = CabalsTabModel(dataSource: source)
        model.updateQuery("week")
        try await settle()
        #expect(model.searchState == .failed)

        // Act: retry, then look before a debounce window could have elapsed.
        source.searchError = nil
        model.retrySearch()
        try await Task.sleep(for: .milliseconds(150))

        #expect(model.searchState == .results)
    }
}

@MainActor
struct JoinCabalCopyTests {
    @Test func aReadOnlyCabalDoesNotAskTheMemberToRetry() {
        let message = JoinCabalCopy.failureMessage(for: Monaco.MonacoAPIError.httpStatus(403), enteredCode: true)

        #expect(message.contains("demo"))
        #expect(!message.contains("Try again"))
    }

    @Test func aBadCodeIsAboutTheCodeOnlyWhenOneWasTyped() {
        let typed = JoinCabalCopy.failureMessage(for: Monaco.MonacoAPIError.httpStatus(404), enteredCode: true)
        let fromRow = JoinCabalCopy.failureMessage(for: Monaco.MonacoAPIError.httpStatus(404), enteredCode: false)

        #expect(typed.contains("invite code"))
        #expect(!fromRow.contains("invite code"))
    }

    @Test func anUnknownFailureStaysRetryable() {
        let message = JoinCabalCopy.failureMessage(for: Monaco.MonacoAPIError.httpStatus(500), enteredCode: true)

        #expect(message == "Couldn't join this cabal. Try again.")
    }
}

@MainActor
struct CabalsRouteTests {
    @Test func aRowTheViewerIsInOpensTheCabal() {
        let route = CabalsRoute(row: "g1", name: "Weekend investors", isJoined: true, joinMode: .request)

        #expect(route == .cabal(id: "g1", name: "Weekend investors"))
    }

    @Test func anApprovalCabalRoutesToTheRequestForm() {
        let route = CabalsRoute(row: "g2", name: "Rent money", isJoined: false, joinMode: .request)

        #expect(route == .join(id: "g2", name: "Rent money", mode: .request))
    }

    @Test func anOpenCabalRoutesToTheJoinForm() {
        let route = CabalsRoute(row: "g3", name: "Apple heads", isJoined: false, joinMode: .open)

        #expect(route == .join(id: "g3", name: "Apple heads", mode: .open))
    }
}
