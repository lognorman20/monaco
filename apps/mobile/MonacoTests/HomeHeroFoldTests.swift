import Foundation
import Testing
@testable import Monaco

private struct StubSeriesFailure: Error {}

/// Answers `getHomePnLSeries` from a canned table and counts what was asked for.
private final class StubSeriesSource: AppSessionDataSource {
    var requests: [HomeLeaderboardRange] = []
    /// Ranges that throw instead of answering.
    var failingRanges: Set<HomeLeaderboardRange> = []

    func getHomePnLSeries(accessToken: String, range: HomeLeaderboardRange) async throws -> HomePnLSeriesDTO {
        requests.append(range)
        if failingRanges.contains(range) {
            throw StubSeriesFailure()
        }
        return HomePnLSeriesDTO(points: StubSeriesSource.points(count: 4))
    }

    static func points(count: Int) -> [HomePnLSeriesPointDTO] {
        (0..<count).map { index in
            HomePnLSeriesPointDTO(
                ts: Date(timeIntervalSince1970: TimeInterval(index) * 300),
                equityUsd: "100.0\(index)",
                dollarPnl: "+0.0\(index)"
            )
        }
    }

    // Unused by these tests; the protocol needs them.
    func openSession(accessToken: String) async throws -> MeResponse { fatalError("unused") }
    func me(accessToken: String) async throws -> MeResponse { fatalError("unused") }
    func getPlatformBalance(accessToken: String) async throws -> PlatformBalanceDTO { fatalError("unused") }
    func getHome(accessToken: String) async throws -> HomeViewDTO { fatalError("unused") }
    func getHomeDashboard(accessToken: String, leaderboardRange: HomeLeaderboardRange) async throws -> HomeDashboardDTO {
        fatalError("unused")
    }
    func getPopularAssets(accessToken: String, limit: Int) async throws -> PopularAssetsResponse { fatalError("unused") }
}

@MainActor
struct HomeHeroRangeModelTests {
    /// Waits for the model's in-flight read to land. The load is a detached `Task`, so a plain
    /// `await Task.yield()` is not enough on a loaded machine.
    private func settle(_ model: HomeHeroRangeModel) async {
        for _ in 0..<50 where model.isLoading {
            try? await Task.sleep(for: .milliseconds(10))
        }
    }

    @Test func oneHourComesFromTheStoreAndNeverFromTheNetwork() async {
        let model = HomeHeroRangeModel()
        let source = StubSeriesSource()
        let stored = StubSeriesSource.points(count: 5)

        model.load(accessToken: "token", client: source)
        await settle(model)

        #expect(source.requests.isEmpty)
        #expect(model.series(oneHourSeries: stored)?.count == 5)
    }

    /// The hero used to be stuck on 1H. A pill tap has to actually ask the API for that window.
    @Test func pickingAWindowAsksForThatWindow() async {
        let model = HomeHeroRangeModel()
        let source = StubSeriesSource()

        model.select(.oneWeek, accessToken: "token", client: source)
        await settle(model)

        #expect(source.requests == [.oneWeek])
        #expect(model.range == .oneWeek)
        #expect(model.series(oneHourSeries: nil)?.count == 4)
        #expect(model.failed == false)
    }

    /// A window is an immutable slice of history, so flicking back to one already drawn must not
    /// put the member back on a spinner or spend a second request.
    @Test func aWindowIsFetchedOnce() async {
        let model = HomeHeroRangeModel()
        let source = StubSeriesSource()

        model.select(.oneWeek, accessToken: "token", client: source)
        await settle(model)
        model.select(.oneMonth, accessToken: "token", client: source)
        await settle(model)
        model.select(.oneWeek, accessToken: "token", client: source)
        await settle(model)

        #expect(source.requests == [.oneWeek, .oneMonth])
        #expect(model.isLoading == false)
    }

    /// The failure that matters: a window that could not be loaded must not fall back to another
    /// window's shape. A curve labelled with the wrong window is a lie about money.
    @Test func aFailedWindowReportsItselfAndBorrowsNothing() async {
        let model = HomeHeroRangeModel()
        let source = StubSeriesSource()
        source.failingRanges = [.oneMonth]
        let oneHour = StubSeriesSource.points(count: 6)

        model.select(.oneMonth, accessToken: "token", client: source)
        await settle(model)

        #expect(model.failed)
        #expect(model.range == .oneMonth)
        #expect(model.series(oneHourSeries: oneHour) == nil)
    }

    /// Signed out is not a failed read: the gate is about to replace the screen, so nothing is
    /// claimed about the window either way.
    @Test func noTokenIsNotAFailure() async {
        let model = HomeHeroRangeModel()
        let source = StubSeriesSource()

        model.select(.oneDay, accessToken: nil, client: source)
        await settle(model)

        #expect(source.requests.isEmpty)
        #expect(model.failed == false)
        #expect(model.isLoading == false)
    }
}

struct HomeVoteCountdownRingTests {
    private func expiry(inSeconds seconds: TimeInterval) -> Date {
        Date(timeIntervalSince1970: 1_000_000 + seconds)
    }

    private var now: Date { Date(timeIntervalSince1970: 1_000_000) }

    /// The amber ring on a deck card is "the last hour", and it is the only amber on the card.
    @Test func theRingIsTheLastHourAndNotAMinuteMore() {
        #expect(HomeVoteCountdown.isClosingSoon(expiresAt: expiry(inSeconds: 3599), now: now))
        #expect(HomeVoteCountdown.isClosingSoon(expiresAt: expiry(inSeconds: 3600), now: now))
        #expect(HomeVoteCountdown.isClosingSoon(expiresAt: expiry(inSeconds: 3601), now: now) == false)
    }

    /// A vote that has already closed is dropped by `HomeMissedVotes.open`, so it must never earn
    /// the ring on its way out.
    @Test func aClosedVoteIsNotClosingSoon() {
        #expect(HomeVoteCountdown.isClosingSoon(expiresAt: expiry(inSeconds: 0), now: now) == false)
        #expect(HomeVoteCountdown.isClosingSoon(expiresAt: expiry(inSeconds: -60), now: now) == false)
    }
}

struct SettingsAdvancedLinkDisplayTests {
    /// Both explorer links carry the same title, so without the path Advanced is two identical
    /// rows with no way to tell which is which.
    @Test func theTwoExplorerRowsAreTellableApart() {
        let subtitles = SettingsAdvancedLinks.explorerLinks.map { SettingsAdvancedLinks.displayURL($0.url) }
        #expect(subtitles.allSatisfy { $0?.isEmpty == false })
        #expect(Set(subtitles.compactMap { $0 }).count == SettingsAdvancedLinks.explorerLinks.count)
    }

    @Test func theSchemeIsDroppedAndThePathIsKept() {
        let url = URL(string: "https://basescan.org/address/")!
        #expect(SettingsAdvancedLinks.displayURL(url) == "basescan.org/address/")
    }
}
