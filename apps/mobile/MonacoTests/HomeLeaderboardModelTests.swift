import Testing
@testable import Monaco

/// Stands in for `AppSessionStore`: it records what Home asked for and, like
/// `refreshDashboard`, writes nothing when the read fails or is cancelled.
@MainActor
private final class StubLeaderboardSource: HomeLeaderboardDashboardSource {
    private(set) var requested: [HomeLeaderboardRange] = []
    var loadedRange: HomeLeaderboardRange?
    /// Ranges whose read fails; the dashboard keeps whatever it held.
    var failing: Set<HomeLeaderboardRange> = []
    var delays: [HomeLeaderboardRange: Duration] = [:]

    func loadDashboard(range: HomeLeaderboardRange) async {
        requested.append(range)
        if let delay = delays[range] {
            do {
                try await Task.sleep(for: delay)
            } catch {
                return
            }
        }
        guard !failing.contains(range) else { return }
        loadedRange = range
    }
}

@MainActor
struct HomeLeaderboardModelTests {
    private func settle() async throws {
        try await Task.sleep(for: .milliseconds(60))
    }

    @Test func aSlowRangeCannotLandAfterANewerTap() async throws {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        source.delays = [.oneMonth: .milliseconds(200)]
        let model = HomeLeaderboardModel()

        model.select(.oneMonth, from: source)
        model.select(.oneDay, from: source)
        try await Task.sleep(for: .milliseconds(400))

        #expect(model.selectedRange == .oneDay)
        #expect(source.loadedRange == .oneDay)
        #expect(model.failed == false)
        #expect(model.isLoading == false)
    }

    @Test func aFailedRangeReadOffersARetry() async throws {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        source.failing = [.oneWeek]
        let model = HomeLeaderboardModel()

        model.select(.oneWeek, from: source)
        try await settle()

        #expect(model.failed)
        #expect(model.isLoading == false)

        source.failing = []
        model.retry(from: source)
        try await settle()

        #expect(model.failed == false)
        #expect(source.loadedRange == .oneWeek)
    }

    @Test func aRefreshFromAnotherTabDoesNotLeaveAllTimeRowsUnderTheWeekChip() async throws {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        let model = HomeLeaderboardModel()

        model.select(.oneWeek, from: source)
        try await settle()
        #expect(source.loadedRange == .oneWeek)

        // Profile pulls to refresh, which reloads the dashboard with the default board.
        source.loadedRange = .all
        model.reconcile(from: source)
        try await settle()

        #expect(source.requested == [.oneWeek, .oneWeek])
        #expect(source.loadedRange == .oneWeek)
        #expect(model.selectedRange == .oneWeek)
        #expect(model.failed == false)
    }

    @Test func aServerThatKeepsAnsweringAnotherRangeIsAskedOnlyOnce() async throws {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        source.failing = [.oneWeek]
        let model = HomeLeaderboardModel()

        model.select(.oneWeek, from: source)
        try await settle()
        #expect(model.failed)

        model.reconcile(from: source)
        model.reconcile(from: source)
        try await settle()

        #expect(source.requested == [.oneWeek])
    }

    @Test func aPollThatLandsWithTheChosenRangeClearsTheFailure() async throws {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        source.failing = [.oneWeek]
        let model = HomeLeaderboardModel()

        model.select(.oneWeek, from: source)
        try await settle()
        #expect(model.failed)

        source.loadedRange = .oneWeek
        model.reconcile(from: source)
        try await settle()

        #expect(model.failed == false)
        #expect(source.requested == [.oneWeek])
    }

    @Test func tappingTheSelectedChipAsksForNothing() async throws {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        let model = HomeLeaderboardModel()

        model.select(.all, from: source)
        try await settle()

        #expect(source.requested.isEmpty)
    }
}
