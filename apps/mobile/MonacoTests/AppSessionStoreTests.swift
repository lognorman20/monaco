import MonacoCore
import Testing
@testable import Monaco

/// Records what the store asked the server for, and lets a test hold a response back so
/// two reads can be landed out of order.
@MainActor
private final class StubDataSource: AppSessionDataSource {
    var dashboardRequests: [HomeLeaderboardRange] = []
    var meRequests = 0
    var openSessionRequests = 0
    /// Range -> the leaderboard range the server answers with, defaults to what was asked.
    var pendingDashboards: [HomeLeaderboardRange: CheckedContinuation<Void, Never>] = [:]
    var holdRanges: Set<HomeLeaderboardRange> = []

    func openSession(accessToken: String) async throws -> MeResponse {
        openSessionRequests += 1
        return Self.profile
    }

    func me(accessToken: String) async throws -> MeResponse {
        meRequests += 1
        return Self.profile
    }

    func getPlatformBalance(accessToken: String) async throws -> PlatformBalanceDTO {
        PlatformBalanceDTO(availableUsdcMicros: 0, memberWalletAddress: "wallet", pendingAllocationMicros: 0)
    }

    func getHome(accessToken: String) async throws -> HomeViewDTO {
        HomeViewDTO(groups: [], people: [])
    }

    func getHomeDashboard(
        accessToken: String,
        leaderboardRange: HomeLeaderboardRange
    ) async throws -> HomeDashboardDTO {
        dashboardRequests.append(leaderboardRange)
        if holdRanges.contains(leaderboardRange) {
            await withCheckedContinuation { continuation in
                pendingDashboards[leaderboardRange] = continuation
            }
        }
        return Self.dashboard(range: leaderboardRange)
    }

    func getHomePnLSeries(accessToken: String, range: HomeLeaderboardRange) async throws -> HomePnLSeriesDTO {
        HomePnLSeriesDTO(points: [])
    }

    func getPopularAssets(accessToken: String, limit: Int) async throws -> PopularAssetsResponse {
        PopularAssetsResponse(assets: [])
    }

    func release(_ range: HomeLeaderboardRange) {
        pendingDashboards.removeValue(forKey: range)?.resume()
    }

    static let profile = MeDTO(userId: "user-1", displayName: "Ada", memberWalletAddress: "wallet")

    static func dashboard(range: HomeLeaderboardRange) -> HomeDashboardDTO {
        HomeDashboardDTO(
            netWorthUsd: "100.00",
            netWorthDollarPnl: "+0.00",
            netWorthPercentReturn: nil,
            myGroups: [],
            pnlSeries1H: [],
            leaderboard: HomeLeaderboardSectionDTO(range: range.rawValue, people: []),
            missedProposals: []
        )
    }
}

@MainActor
private final class StubAuth: SessionAuthenticating {
    var accessToken: String? = "token-a"
    var rejectedTokens: [String] = []

    func shouldInvalidateBackendSession(serverUserId: String) -> Bool { false }
    func recordBackendSession(userId: String) {}
    func refreshedAccessToken(replacing rejectedToken: String) async throws -> String? { nil }
    func signOut(reason: String) async {}
    func signOutAfterRejectedSession(rejectedToken: String) async {
        rejectedTokens.append(rejectedToken)
    }
}

/// The store owns the leaderboard range Home shows. Before this, Home kept its own copy and
/// passed it in, so every refresh started anywhere else quietly reloaded the all-time board.
@MainActor
struct AppSessionStoreLeaderboardRangeTests {
    private func settle() async throws {
        try await Task.sleep(for: .milliseconds(50))
    }

    @Test func aRefreshFromAnotherTabKeepsTheSelectedRange() async throws {
        let source = StubDataSource()
        let store = AppSessionStore(apiClient: source)
        let auth = StubAuth()

        await store.selectLeaderboardRange(.oneWeek, auth: auth)
        // What Profile, Cabals and the profile writes do: refresh with no range at all.
        await store.refresh(auth: auth)

        #expect(store.leaderboardRange == .oneWeek)
        #expect(source.dashboardRequests == [.oneWeek, .oneWeek])
        #expect(store.dashboard?.leaderboard.range == HomeLeaderboardRange.oneWeek.rawValue)
    }

    @Test func pollingKeepsAskingForTheSelectedRange() async throws {
        let source = StubDataSource()
        let store = AppSessionStore(apiClient: source)
        let auth = StubAuth()

        await store.selectLeaderboardRange(.oneMonth, auth: auth)
        await store.refresh(auth: auth)
        try await store.pollLive(auth: auth)

        #expect(source.dashboardRequests.allSatisfy { $0 == .oneMonth })
        #expect(store.dashboard?.leaderboard.range == HomeLeaderboardRange.oneMonth.rawValue)
    }

    @Test func anOlderRangeResponseCannotLandOverANewerOne() async throws {
        let source = StubDataSource()
        source.holdRanges = [.oneWeek]
        let store = AppSessionStore(apiClient: source)
        let auth = StubAuth()

        // Two quick taps: 1W is still in flight when 1M is asked for and answered.
        let slow = Task { await store.selectLeaderboardRange(.oneWeek, auth: auth) }
        try await settle()
        await store.selectLeaderboardRange(.oneMonth, auth: auth)
        source.release(.oneWeek)
        await slow.value

        #expect(store.leaderboardRange == .oneMonth)
        #expect(store.dashboard?.leaderboard.range == HomeLeaderboardRange.oneMonth.rawValue)
    }

    @Test func creatingACabalRefreshesTheSelectedBoard() async throws {
        let source = StubDataSource()
        let store = AppSessionStore(apiClient: source)
        let auth = StubAuth()

        await store.selectLeaderboardRange(.oneDay, auth: auth)
        store.refreshAfterCreate(
            auth: auth,
            created: CreateGroupResponse(groupId: "g-1", name: "Weekend", treasuryAddress: "addr")
        )
        try await settle()

        #expect(source.dashboardRequests == [.oneDay, .oneDay])
        #expect(store.leaderboardRange == .oneDay)
    }
}

@MainActor
struct AppSessionStoreBootstrapTests {
    @Test func bootstrapDoesNotAskForTheProfileTwice() async throws {
        let source = StubDataSource()
        let store = AppSessionStore(apiClient: source)
        let auth = StubAuth()

        await store.bootstrap(auth: auth)

        #expect(source.openSessionRequests == 1)
        // openSession already returned the profile.
        #expect(source.meRequests == 0)
        #expect(store.me == StubDataSource.profile)
    }
}
