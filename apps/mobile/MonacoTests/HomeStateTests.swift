import Foundation
import Testing
@testable import Monaco

@MainActor
struct HomeScreenStateTests {
    private func dashboard(netWorthUsd: String = "1248.50") -> HomeDashboardDTO {
        HomeDashboardDTO(
            netWorthUsd: netWorthUsd,
            netWorthDollarPnl: "+48.20",
            netWorthPercentReturn: "0.040",
            myGroups: [],
            pnlSeries1H: [],
            leaderboard: HomeLeaderboardSectionDTO(range: "ALL", people: []),
            missedProposals: []
        )
    }

    @Test func nothingLoadedYetIsLoading() {
        #expect(HomeScreenState.resolve(dashboard: nil, errorMessage: nil) == .loading)
    }

    @Test func aFailedFirstLoadShowsTheFailure() {
        #expect(HomeScreenState.resolve(dashboard: nil, errorMessage: "No connection.") == .failed("No connection."))
    }

    /// The old ladder had an unreachable branch that rendered a made-up "$0.00" dashboard.
    /// Every combination now resolves to a state built from real data.
    @Test func aLoadedBoardIsNeverReplacedByAFabricatedZero() {
        let loaded = dashboard()
        #expect(HomeScreenState.resolve(dashboard: loaded, errorMessage: nil) == .loaded(loaded))
        #expect(HomeScreenState.resolve(dashboard: loaded, errorMessage: "No connection.") == .loaded(loaded))
    }
}

@MainActor
struct HomeHeroChartTests {
    private func points(_ count: Int) -> [HomePnLSeriesPointDTO] {
        (0..<count).map { index in
            HomePnLSeriesPointDTO(
                ts: Date(timeIntervalSince1970: TimeInterval(index * 300)),
                equityUsd: "1000.00",
                dollarPnl: "+\(index).00"
            )
        }
    }

    @Test func aMemberWithNoCabalsGetsNoSlot() {
        #expect(HomeHeroChart.resolve(loaded: nil, embedded: [], hasCabals: false) == .hidden)
        #expect(HomeHeroChart.resolve(loaded: points(5), embedded: [], hasCabals: false) == .hidden)
    }

    /// The slot is the same height before, during and after the series lands, so Home does
    /// not shift when the curve arrives — or when it turns out to be too short to draw.
    @Test func theSlotIsHeldFromTheFirstFrameUntilTheCurveCanBeDrawn() {
        #expect(HomeHeroChart.resolve(loaded: nil, embedded: [], hasCabals: true) == .reserved(hasResolved: false))
        #expect(HomeHeroChart.resolve(loaded: points(2), embedded: [], hasCabals: true) == .reserved(hasResolved: true))
        #expect(HomeHeroChart.resolve(loaded: points(1), embedded: [], hasCabals: true) == .reserved(hasResolved: true))
    }

    /// The cold-start case. The backend hard-codes the dashboard's own `pnlSeries1H` to an
    /// empty array and the real series arrives on a later read, so "nothing yet" and "came
    /// back with one point" have to be different answers — otherwise the hero tells every
    /// member with a cabal "No curve yet" on every launch and takes it back a second later.
    @Test func aSeriesThatHasNotComeBackIsNotASeriesThatCameBackEmpty() {
        let pending = HomeHeroChart.resolve(loaded: nil, embedded: [], hasCabals: true)
        let answered = HomeHeroChart.resolve(loaded: [], embedded: [], hasCabals: true)

        #expect(pending == .reserved(hasResolved: false))
        #expect(answered == .reserved(hasResolved: true))
        #expect(pending != answered)
    }

    /// The dashboard's embedded copy is empty on every real read, so it is only an answer
    /// when it carries points — otherwise it would resolve the pending case for us.
    @Test func theDashboardsEmptyCopyDoesNotCountAsAnAnswer() {
        #expect(HomeHeroChart.resolve(loaded: nil, embedded: [], hasCabals: true) == .reserved(hasResolved: false))

        let embedded = points(4)
        #expect(HomeHeroChart.resolve(loaded: nil, embedded: embedded, hasCabals: true) == .curve(embedded))
    }

    @Test func threePointsEarnTheCurve() {
        let series = points(3)
        #expect(HomeHeroChart.resolve(loaded: series, embedded: [], hasCabals: true) == .curve(series))
    }

    /// The separate 1H read wins over the dashboard's stale copy once it lands.
    @Test func theLoadedSeriesWinsOverTheDashboardsCopy() {
        let loaded = points(5)
        #expect(HomeHeroChart.resolve(loaded: loaded, embedded: points(3), hasCabals: true) == .curve(loaded))
    }
}

@MainActor
struct HomeMissedVotesTests {
    private let now = Date(timeIntervalSince1970: 1_700_000_000)

    private func row(_ id: String, closesIn seconds: TimeInterval) -> HomeMissedProposalRowDTO {
        HomeMissedProposalRowDTO(
            groupId: "g1",
            groupName: "Weekend investors",
            proposalId: id,
            symbol: "AAPL",
            status: "OPEN",
            createdAt: now.addingTimeInterval(-3600),
            expiresAt: now.addingTimeInterval(seconds)
        )
    }

    /// Home gates the section on these, not on the payload: the section renders nothing once
    /// every row has expired, and an empty section still takes a spacing on each side of it.
    @Test func onlyTheVotesStillCollectingAreOpen() {
        let rows = [row("a", closesIn: -1), row("b", closesIn: 600), row("c", closesIn: 0)]

        #expect(HomeMissedVotes.open(rows, now: now).map(\.proposalId) == ["b"])
    }

    @Test func aPayloadWhoseVotesHaveAllClosedLeavesNothingToShow() {
        let rows = [row("a", closesIn: -600), row("b", closesIn: -1)]

        #expect(HomeMissedVotes.open(rows, now: now).isEmpty)
    }

    /// The clock wakes on the earliest close still ahead, so an expired row leaves the section
    /// then rather than at the next dashboard poll.
    @Test func theNextExpiryIsTheEarliestOneStillAhead() {
        let rows = [row("a", closesIn: 900), row("b", closesIn: -30), row("c", closesIn: 300)]

        #expect(HomeMissedVotes.nextExpiry(rows, after: now) == now.addingTimeInterval(300))
    }

    @Test func nothingLeftToCloseNeedsNoClock() {
        #expect(HomeMissedVotes.nextExpiry([], after: now) == nil)
        #expect(HomeMissedVotes.nextExpiry([row("a", closesIn: -1)], after: now) == nil)
    }
}

@MainActor
struct HomeVoteCountdownTests {
    private let now = Date(timeIntervalSince1970: 1_700_000_000)

    private func label(inSeconds seconds: TimeInterval) -> String? {
        HomeVoteCountdown.label(expiresAt: now.addingTimeInterval(seconds), now: now)
    }

    @Test func aClosedVoteHasNoCountdown() {
        #expect(label(inSeconds: -1) == nil)
        #expect(label(inSeconds: 0) == nil)
    }

    @Test func theLastMinuteSaysSo() {
        #expect(label(inSeconds: 30) == "closes in under a minute")
    }

    @Test func minutesRoundUpSoACountdownNeverReadsZero() {
        #expect(label(inSeconds: 90) == "closes in 2m")
        #expect(label(inSeconds: 59 * 60) == "closes in 59m")
    }

    /// Flooring used to read 1 h 59 m as "closes in 1h".
    @Test func hoursRoundToTheNearestHour() {
        #expect(label(inSeconds: 3600) == "closes in 1h")
        #expect(label(inSeconds: 3600 + 59 * 60) == "closes in 2h")
        #expect(label(inSeconds: 23 * 3600) == "closes in 23h")
    }

    /// A three-day vote used to read "closes in 71h".
    @Test func longVotesCountDownInDays() {
        #expect(label(inSeconds: 71 * 3600) == "closes in 3d")
        #expect(label(inSeconds: 24 * 3600) == "closes in 1d")
    }

    /// The unit boundaries themselves, which only interior values were covering. Rounding up
    /// inside minutes printed "closes in 60m" a second short of the hour, and rounding inside
    /// hours printed "closes in 24h" a second short of the day.
    @Test func aUnitIsPromotedRatherThanAllowedToOverflow() {
        #expect(label(inSeconds: 3599) == "closes in 1h")
        #expect(label(inSeconds: 86_399) == "closes in 1d")
    }

    /// And the values below each boundary still read in the smaller unit.
    @Test func theValuesBelowEachBoundaryKeepTheirUnit() {
        #expect(label(inSeconds: 58 * 60 + 30) == "closes in 59m")
        #expect(label(inSeconds: 59) == "closes in under a minute")
        #expect(label(inSeconds: 60) == "closes in 1m")
        #expect(label(inSeconds: 23 * 3600 + 29 * 60) == "closes in 23h")
    }
}
