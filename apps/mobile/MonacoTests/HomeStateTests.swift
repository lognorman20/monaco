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
        #expect(HomeHeroChart.resolve(points: [], hasCabals: false) == .hidden)
        #expect(HomeHeroChart.resolve(points: points(5), hasCabals: false) == .hidden)
    }

    /// The slot is the same height before, during and after the series lands, so Home does
    /// not shift when the curve arrives — or when it turns out to be too short to draw.
    @Test func theSlotIsHeldFromTheFirstFrameUntilTheCurveCanBeDrawn() {
        #expect(HomeHeroChart.resolve(points: [], hasCabals: true) == .reserved)
        #expect(HomeHeroChart.resolve(points: points(2), hasCabals: true) == .reserved)
        #expect(HomeHeroChart.resolve(points: points(1), hasCabals: true) == .reserved)
    }

    @Test func threePointsEarnTheCurve() {
        let series = points(3)
        #expect(HomeHeroChart.resolve(points: series, hasCabals: true) == .curve(series))
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
}
