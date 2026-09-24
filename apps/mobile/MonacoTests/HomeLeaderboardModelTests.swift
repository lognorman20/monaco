import Testing
@testable import Monaco

/// Stands in for `AppSessionStore`: it records what Home asked for and, like
/// `refreshDashboard`, writes nothing when the read fails or is cancelled.
///
/// Reads are sequenced by continuations rather than by sleeping, so a loaded CI box cannot
/// turn "the slow read is still in flight" into a race the test loses.
@MainActor
private final class StubLeaderboardSource: HomeLeaderboardDashboardSource {
    private(set) var requested: [HomeLeaderboardRange] = []
    var loadedRange: HomeLeaderboardRange?
    /// Ranges whose read fails; the dashboard keeps whatever it held.
    var failing: Set<HomeLeaderboardRange> = []
    /// Ranges answered with an echo this build cannot read — a range added server-side, or a
    /// normalised payload — which `HomeLeaderboardRange(rawValue:)` turns into nil.
    var unreadableEcho: Set<HomeLeaderboardRange> = []
    /// Ranges whose read parks until `release(_:)`. An explicit "still in flight".
    var parks: Set<HomeLeaderboardRange> = []

    private var parked: [HomeLeaderboardRange: CheckedContinuation<Void, Never>] = [:]
    private var startWaiters: [CheckedContinuation<Void, Never>] = []
    private var finishWaiters: [CheckedContinuation<Void, Never>] = []
    private var unclaimedFinishes = 0

    func loadDashboard(range: HomeLeaderboardRange) async {
        requested.append(range)
        resumeStartWaiters()

        if parks.contains(range) {
            await withCheckedContinuation { parked[range] = $0 }
        }

        // Like the store: a read that was superseded, or that failed, writes nothing.
        if !Task.isCancelled, !failing.contains(range) {
            loadedRange = unreadableEcho.contains(range) ? nil : range
        }
        signalFinish()
    }

    /// Lets a parked read answer at last.
    func release(_ range: HomeLeaderboardRange) {
        parks.remove(range)
        parked.removeValue(forKey: range)?.resume()
    }

    /// Returns once `range` has been asked for — i.e. its read is under way.
    func awaitRequest(_ range: HomeLeaderboardRange) async {
        while !requested.contains(range) {
            await withCheckedContinuation { startWaiters.append($0) }
        }
    }

    /// Returns once one more read has run to completion. The model finishes writing its own
    /// state before the main actor picks this waiter up, so the assertions that follow see
    /// the settled model.
    func awaitRead() async {
        if unclaimedFinishes > 0 {
            unclaimedFinishes -= 1
            return
        }
        await withCheckedContinuation { finishWaiters.append($0) }
    }

    private func resumeStartWaiters() {
        let waiters = startWaiters
        startWaiters.removeAll()
        waiters.forEach { $0.resume() }
    }

    private func signalFinish() {
        if finishWaiters.isEmpty {
            unclaimedFinishes += 1
        } else {
            finishWaiters.removeFirst().resume()
        }
    }
}

@MainActor
struct HomeLeaderboardModelTests {
    @Test func aSlowRangeCannotLandAfterANewerTap() async {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        source.parks = [.oneMonth]
        let model = HomeLeaderboardModel()

        model.select(.oneMonth, from: source)
        await source.awaitRequest(.oneMonth)

        // A newer tap while the first read is still parked.
        model.select(.oneDay, from: source)
        await source.awaitRead()

        // Only now does the superseded read answer.
        source.release(.oneMonth)
        await source.awaitRead()

        #expect(model.selectedRange == .oneDay)
        #expect(source.loadedRange == .oneDay)
        #expect(model.failed == false)
        #expect(model.isLoading == false)
    }

    @Test func aFailedRangeReadOffersARetry() async {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        source.failing = [.oneWeek]
        let model = HomeLeaderboardModel()

        model.select(.oneWeek, from: source)
        await source.awaitRead()

        #expect(model.failed)
        #expect(model.isLoading == false)

        source.failing = []
        model.retry(from: source)
        await source.awaitRead()

        #expect(model.failed == false)
        #expect(source.loadedRange == .oneWeek)
    }

    /// The blocking regression. `failed` was derived straight from `loadedRange != requested`,
    /// and `loadedRange` decodes a server-echoed string — so a backend that added a range, or
    /// started normalising the echo, made every *successful* read read as a failure and
    /// latched the board on "Couldn't load the board" with nothing logged.
    @Test func anEchoThisBuildCannotReadIsUnknownRatherThanFailed() async {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        source.unreadableEcho = [.oneWeek]
        let model = HomeLeaderboardModel()

        model.select(.oneWeek, from: source)
        await source.awaitRead()

        #expect(source.loadedRange == nil)
        #expect(model.failed == false)
        #expect(model.isLoading == false)
        #expect(model.selectedRange == .oneWeek)
    }

    /// And the unreadable echo must not latch through `reconcile` either — its own early
    /// return leaves the member's choice and the rows alone.
    @Test func anUnreadableEchoDoesNotStrandTheBoardOnTheNextReconcile() async {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        source.unreadableEcho = [.oneWeek]
        let model = HomeLeaderboardModel()

        model.select(.oneWeek, from: source)
        await source.awaitRead()

        model.reconcile(from: source)
        model.reconcile(from: source)

        #expect(model.failed == false)
        #expect(model.selectedRange == .oneWeek)
        #expect(source.requested == [.oneWeek])
    }

    @Test func aRefreshFromAnotherTabDoesNotLeaveAllTimeRowsUnderTheWeekChip() async {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        let model = HomeLeaderboardModel()

        model.select(.oneWeek, from: source)
        await source.awaitRead()
        #expect(source.loadedRange == .oneWeek)

        // Profile pulls to refresh, which reloads the dashboard with the default board.
        source.loadedRange = .all
        model.reconcile(from: source)
        await source.awaitRead()

        #expect(source.requested == [.oneWeek, .oneWeek])
        #expect(source.loadedRange == .oneWeek)
        #expect(model.selectedRange == .oneWeek)
        #expect(model.failed == false)
    }

    @Test func aServerThatKeepsAnsweringAnotherRangeIsAskedOnlyOnce() async {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        source.failing = [.oneWeek]
        let model = HomeLeaderboardModel()

        model.select(.oneWeek, from: source)
        await source.awaitRead()
        #expect(model.failed)

        model.reconcile(from: source)
        model.reconcile(from: source)

        #expect(source.requested == [.oneWeek])
    }

    @Test func aPollThatLandsWithTheChosenRangeClearsTheFailure() async {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        source.failing = [.oneWeek]
        let model = HomeLeaderboardModel()

        model.select(.oneWeek, from: source)
        await source.awaitRead()
        #expect(model.failed)

        source.loadedRange = .oneWeek
        model.reconcile(from: source)

        #expect(model.failed == false)
        #expect(source.requested == [.oneWeek])
    }

    @Test func tappingTheSelectedChipAsksForNothing() async {
        let source = StubLeaderboardSource()
        source.loadedRange = .all
        let model = HomeLeaderboardModel()

        model.select(.all, from: source)
        await Task.yield()

        #expect(source.requested.isEmpty)
    }
}

/// `LiveHomeLeaderboardDashboardSource.loadedRange` decodes the range the server echoed in
/// the payload, and the whole leaderboard error UI hangs off whether that decode succeeds.
/// The stub above uses a typed enum, so these are the only tests that cover the mapping.
@MainActor
struct HomeLeaderboardRangeEchoTests {
    /// The five values `buildRangedLeaderboard` echoes back
    /// (apps/backend/internal/app/home_dashboard.go). If one of them stops decoding, every
    /// successful read for that range looks like a failed one.
    @Test func everyRangeTheBackendEchoesDecodes() {
        let echoed = ["1H", "1D", "1W", "1M", "ALL"]
        for raw in echoed {
            #expect(HomeLeaderboardRange(rawValue: raw) != nil, "the backend echoes \(raw) and this build cannot read it")
        }
        #expect(Set(echoed) == Set(HomeLeaderboardRange.allCases.map(\.rawValue)))
    }

    /// And every range the app can ask for comes back as itself.
    @Test func everyRangeTheAppAsksForRoundTrips() {
        for range in HomeLeaderboardRange.allCases {
            #expect(HomeLeaderboardRange(rawValue: range.rawValue) == range)
        }
    }

    /// The shapes that do *not* decode, which is exactly why a nil echo cannot mean "failed":
    /// the payload may be perfectly good and simply newer than this build.
    @Test func anUnknownOrNormalisedEchoDoesNotDecode() {
        #expect(HomeLeaderboardRange(rawValue: "all") == nil)
        #expect(HomeLeaderboardRange(rawValue: "1Y") == nil)
        #expect(HomeLeaderboardRange(rawValue: "") == nil)
    }
}
