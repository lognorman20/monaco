import MonacoCore
import Testing
@testable import Monaco

@MainActor
private final class RecordingDataSource: CabalsTabDataSource {
    var searches: [String] = []
    var searchError: Error?
    var pnlRanges: [GroupPnLRange] = []

    func leaderboard() async throws -> GroupLeaderboardResponseDTO {
        GroupLeaderboardResponseDTO(groups: [])
    }

    func pnlHistory(range: GroupPnLRange) async throws -> MyGroupsPnLHistoryDTO {
        pnlRanges.append(range)
        return MyGroupsPnLHistoryDTO(range: range.rawValue, series: [])
    }

    func search(query: String, cursor: String?) async throws -> GroupSearchResponseDTO {
        searches.append(query)
        if let searchError { throw searchError }
        let row = GroupDiscoveryRowDTO(
            groupID: "g-\(query)", name: "Weekend \(query)", memberCount: 2, potValueUsd: "10.00",
            percentReturn: nil, dollarPnl: "+0.00", isJoined: false, joinMode: .open
        )
        return GroupSearchResponseDTO(groups: query == "none" ? [] : [row], nextCursor: nil)
    }
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
        #expect(source.searches == ["week"])
        #expect(model.searchState == .results)
        #expect(model.results.map(\.groupID) == ["g-week"])
    }

    @Test func singleCharacterNeverHitsTheServer() async throws {
        let source = RecordingDataSource()
        let model = CabalsTabModel(dataSource: source)

        model.updateQuery(" w ")
        try await settle()

        #expect(source.searches.isEmpty)
        #expect(model.searchState == .tooShort)
    }

    @Test func clearingTheQueryCancelsThePendingSearch() async throws {
        let source = RecordingDataSource()
        let model = CabalsTabModel(dataSource: source)

        model.updateQuery("weekend")
        model.clearSearch()
        try await settle()

        #expect(source.searches.isEmpty)
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
}
