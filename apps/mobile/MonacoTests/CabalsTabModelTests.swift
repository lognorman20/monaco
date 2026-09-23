import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// Holds a fake read open until the test lets it finish, one caller at a time
/// and in arrival order. Interleavings are then the test's to choose, rather
/// than whatever order the main actor happens to run its tasks in.
@MainActor
private final class CallGate {
    private var waiters: [CheckedContinuation<Void, Never>] = []
    private var banked = 0

    func wait() async {
        if banked > 0 {
            banked -= 1
            return
        }
        await withCheckedContinuation { waiters.append($0) }
    }

    /// Lets the longest-waiting caller through. Banked if nobody is waiting yet.
    func release() {
        if waiters.isEmpty {
            banked += 1
            return
        }
        waiters.removeFirst().resume()
    }
}

@MainActor
private final class RecordingDataSource: CabalsTabDataSource {
    var searches: [(query: String, cursor: String?)] = []
    var searchError: Error?
    var pnlRanges: [GroupPnLRange] = []
    /// Reads that have come back. `pnlRanges` records arrival, this records exit.
    var pnlCompletions = 0
    var pnlError: Error?
    var boardRows: [GroupLeaderboardRowDTO] = []
    var boardLoads = 0
    /// Points per cabal, per range, for the chart tests.
    var seriesByRange: [GroupPnLRange: [GroupPnLSeriesDTO]] = [:]
    /// Held open so a test can drive what happens while a search is in flight.
    var searchGate: (() async -> Void)?
    /// The same, for the chart reads.
    var pnlGate: (() async -> Void)?
    /// Cursor the next page comes back with.
    var nextCursor: String?

    var queries: [String] { searches.map(\.query) }

    func leaderboard() async throws -> GroupLeaderboardResponseDTO {
        boardLoads += 1
        return GroupLeaderboardResponseDTO(groups: boardRows)
    }

    func pnlHistory(range: GroupPnLRange) async throws -> MyGroupsPnLHistoryDTO {
        pnlRanges.append(range)
        if let pnlGate { await pnlGate() }
        pnlCompletions += 1
        if let pnlError { throw pnlError }
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

    /// Polls until something the test is waiting for is true. Used with
    /// `CallGate` so a test never guesses at how long a hop takes.
    private func waitUntil(_ description: String, _ condition: () -> Bool) async throws {
        for _ in 0..<400 {
            if condition() { return }
            try await Task.sleep(for: .milliseconds(5))
        }
        Issue.record("timed out waiting for \(description)")
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
        source.searchError = RejectedSession(token: "token-1")
        let model = CabalsTabModel(dataSource: source)

        model.updateQuery("weekend")
        try await settle()

        #expect(model.sessionExpired)
        // The view signs out through the token that read carried, not whatever is current.
        #expect(model.rejectedSession == RejectedSession(token: "token-1"))
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
        // the range picker that gets you back out — has earned its place. The
        // section rule itself is asserted, not just the flags behind it.
        #expect(!CabalsTabModel.isChartable(model.series))
        #expect(model.hasChartableHistory)
        #expect(model.showsChartSection(hasCabals: true))
        #expect(!model.showsChartSection(hasCabals: false))
    }

    @Test func aSupersededRangeLoadDoesNotClearTheNewerSpinner() async throws {
        // Arrange: only 3M has drawable history, so if the superseded 1D load
        // were the one that landed, the series would be empty. The gate makes
        // the interleaving the test's choice rather than the scheduler's.
        let source = RecordingDataSource()
        source.seriesByRange = [
            .threeMonths: [sampleSeries(id: "a", points: 8), sampleSeries(id: "b", points: 6)],
        ]
        let gate = CallGate()
        source.pnlGate = { await gate.wait() }
        let model = CabalsTabModel(dataSource: source)

        // Act: pick a range, then pick another before the first can land.
        model.selectRange(.oneDay)
        try await waitUntil("the 1D read to start") { source.pnlRanges == [.oneDay] }
        model.selectRange(.threeMonths)
        try await waitUntil("the 3M read to start") { source.pnlRanges == [.oneDay, .threeMonths] }

        gate.release()
        try await waitUntil("the superseded 1D read to return") { source.pnlCompletions == 1 }
        gate.release()
        try await waitUntil("the live 3M read to return") { source.pnlCompletions == 2 }
        try await waitUntil("the chart to settle") { model.loadingRange == nil }

        // Assert: each load knew its own range, the newer one won, and the
        // superseded one neither cleared the spinner nor left it stuck on.
        #expect(model.range == .threeMonths)
        #expect(model.seriesRange == .threeMonths)
        #expect(CabalsTabModel.isChartable(model.series))
    }

    @Test func aSupersededLoadOfTheSameRangeDoesNotClearTheNewerSpinner() async throws {
        // Arrange: the cold start every member gets. `.task` calls `reload()`,
        // then `/v1/home` lands and `onChange(of: joinedIDs)` calls it again —
        // two loads, same range, overlapping. Tracking the load by its *range*
        // could not tell them apart, so the first to exit (the cancelled one,
        // because `defer` runs on the guard-return path too) cleared the live
        // load's spinner and the chart section dropped out mid-load.
        let source = RecordingDataSource()
        source.seriesByRange = [
            .oneMonth: [sampleSeries(id: "a", points: 8), sampleSeries(id: "b", points: 6)],
        ]
        let gate = CallGate()
        source.pnlGate = { await gate.wait() }
        let model = CabalsTabModel(dataSource: source)

        // Act
        let first = Task { await model.reload() }
        try await waitUntil("the first chart read to start") { source.pnlRanges.count == 1 }
        let second = Task { await model.reload() }  // cancel-and-replace
        try await waitUntil("the second chart read to start") { source.pnlRanges.count == 2 }
        #expect(source.pnlRanges == [.oneMonth, .oneMonth])

        // The superseded load is the first to come back.
        gate.release()
        await first.value

        // Assert: it wrote nothing, and the load still running owns the spinner.
        #expect(model.series.isEmpty)
        #expect(model.loadingRange == .oneMonth)
        #expect(model.isChartLoading)
        #expect(model.showsChartSection(hasCabals: true))

        gate.release()
        await second.value

        #expect(model.loadingRange == nil)
        #expect(!model.isChartLoading)
        #expect(CabalsTabModel.isChartable(model.series))
    }

    @Test func aFailedRangeSwitchDropsLinesThatBelongToTheOldRange() async throws {
        // Arrange: 1M is drawn.
        let source = RecordingDataSource()
        source.seriesByRange = [
            .oneMonth: [sampleSeries(id: "a", points: 8), sampleSeries(id: "b", points: 6)],
        ]
        let model = CabalsTabModel(dataSource: source)
        await model.reload()
        #expect(model.seriesRange == .oneMonth)

        // Act: tap 1D and have the read fail.
        source.pnlError = Monaco.MonacoAPIError.httpStatus(500)
        model.selectRange(.oneDay)
        try await settle()

        // Assert: the 1D chip is highlighted, so the 1M lines cannot still be
        // on screen at full opacity with no error. The section — and the picker
        // that gets the member back to 1M — stays.
        #expect(model.chartFailed)
        #expect(model.series.isEmpty)
        #expect(model.seriesRange == nil)
        #expect(model.showsChartSection(hasCabals: true))
    }

    @Test func aFailedRefreshOfTheRangeOnScreenKeepsItsLines() async throws {
        // A failed refresh is not a failed switch: the lines still belong to the
        // highlighted chip, so keep them rather than blanking a good chart.
        let source = RecordingDataSource()
        source.seriesByRange = [
            .oneMonth: [sampleSeries(id: "a", points: 8), sampleSeries(id: "b", points: 6)],
        ]
        let model = CabalsTabModel(dataSource: source)
        await model.reload()

        source.pnlError = Monaco.MonacoAPIError.httpStatus(500)
        await model.loadChart()

        #expect(model.seriesRange == .oneMonth)
        #expect(CabalsTabModel.isChartable(model.series))
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

    @Test func aJoinSurvivesTheBoardReloadThatFollowsIt() async throws {
        // Arrange: the server keeps answering "not joined", which is what a
        // read-after-write lag or a cached board query looks like from here.
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

        model.markJoined(groupID: "g-week")
        #expect(model.leaderboard.first?.isJoined == true)

        // Act: joining refreshes the session, membership changes, and the tab
        // reloads the board and re-runs the search.
        await model.reload()
        model.updateQuery("week")
        try await settle()

        // Assert: the row the member just joined does not flip back to "· Open"
        // a second later.
        #expect(model.leaderboard.first?.isJoined == true)
        #expect(model.results.first?.isJoined == true)
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

/// The terminus name labels on the cabals P&L chart.
///
/// Every line ends at the same instant, so the labels sit in one column and two cabals with
/// similar P&L would print their names on top of each other — and §1.8 is that tint is never the
/// only identity signal, which on this chart means the name has to stay readable.
@MainActor
struct CabalsChartLabelLayoutTests {
    private let domain: ClosedRange<Double> = -20...80
    private let plotHeight: CGFloat = 176

    private func offsets(_ terminals: [(id: String, value: Double)]) -> [String: CGFloat] {
        CabalsPnLChartSection.labelOffsets(
            terminals: terminals, domain: domain, plotHeight: plotHeight, minimumSeparation: 15
        )
    }

    @Test func labelsThatAlreadyClearEachOtherAreNotMoved() {
        let placed = offsets([("a", 70), ("b", 20), ("c", -10)])

        #expect(placed["a"] == 0)
        #expect(placed["b"] == 0)
        #expect(placed["c"] == 0)
    }

    /// The topmost label never moves, so a name is always nearest the line it belongs to; the one
    /// below is pushed down to exactly the minimum separation and no further.
    @Test func aCrowdedPairIsPushedApartDownwards() {
        let placed = offsets([("high", 40), ("low", 38)])
        let span = domain.upperBound - domain.lowerBound
        let pointsApart = CGFloat((40.0 - 38.0) / span) * plotHeight

        #expect(placed["high"] == 0)
        #expect(abs((placed["low"] ?? 0) - (15 - pointsApart)) < 0.001)
    }

    /// Three identical termini — three cabals all flat at zero, which is what a new set of
    /// cabals looks like — stack at one separation each rather than collapsing into one label.
    @Test func aStackOfIdenticalTerminiSpreadsEvenly() {
        let placed = offsets([("a", 0), ("b", 0), ("c", 0)])
        let steps = placed.values.sorted()

        #expect(steps == [0, 15, 30])
    }

    /// Order is by value, not by arrival: the input is whatever the series array happens to hold.
    @Test func theWalkIsOrderedByValueNotByInput() {
        let placed = offsets([("low", 38), ("high", 40)])

        #expect(placed["high"] == 0)
        #expect((placed["low"] ?? 0) > 0)
    }

    @Test func aDegenerateDomainPlacesNothingRatherThanDividingByZero() {
        let placed = CabalsPnLChartSection.labelOffsets(
            terminals: [("a", 0)], domain: 0...0, plotHeight: plotHeight
        )

        #expect(placed.isEmpty)
    }
}
