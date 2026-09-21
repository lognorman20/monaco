// Only MeDTO: importing all of MonacoCore would make the DTO names the app also
// declares (HomeDashboardDTO, HomeLeaderboardRange…) ambiguous in this file.
import struct MonacoCore.MeDTO
import Testing
@testable import Monaco

/// Records what the store asked the server for, lets a test hold a response back so two
/// reads can be landed out of order, and lets a test queue failures so the error paths are
/// reachable without a server.
@MainActor
private final class StubDataSource: AppSessionDataSource {
    var dashboardRequests: [HomeLeaderboardRange] = []
    var meRequests = 0
    var openSessionRequests = 0
    /// Ranges whose response is held until the test releases it.
    var holdRanges: Set<HomeLeaderboardRange> = []
    /// Thrown by `openSession`, one per call, oldest first. Empty means succeed.
    var openSessionErrors: [Error] = []
    /// Holds `openSession` until the test releases it, so a reply can be made to outlive
    /// the sign-in that asked for it.
    var holdOpenSession = false
    /// Thrown by `getHomeDashboard`, one per call, oldest first. Empty means succeed.
    var dashboardErrors: [Error] = []

    private var pendingDashboards: [HomeLeaderboardRange: CheckedContinuation<Void, Never>] = [:]
    private var arrivedRanges: Set<HomeLeaderboardRange> = []
    private var arrivalWaiters: [HomeLeaderboardRange: CheckedContinuation<Void, Never>] = [:]
    private var pendingOpenSession: CheckedContinuation<Void, Never>?
    private var openSessionArrived = false
    private var openSessionWaiter: CheckedContinuation<Void, Never>?

    func openSession(accessToken: String) async throws -> MeResponse {
        openSessionRequests += 1
        openSessionArrived = true
        openSessionWaiter?.resume()
        openSessionWaiter = nil
        if holdOpenSession {
            await withCheckedContinuation { continuation in
                pendingOpenSession = continuation
            }
        }
        if !openSessionErrors.isEmpty {
            throw openSessionErrors.removeFirst()
        }
        return Self.profile
    }

    /// Returns once the store has asked to open a session.
    func awaitOpenSession() async {
        guard !openSessionArrived else { return }
        await withCheckedContinuation { continuation in
            openSessionWaiter = continuation
        }
    }

    func releaseOpenSession() {
        holdOpenSession = false
        pendingOpenSession?.resume()
        pendingOpenSession = nil
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
        noteArrival(of: leaderboardRange)
        if holdRanges.contains(leaderboardRange) {
            await withCheckedContinuation { continuation in
                pendingDashboards[leaderboardRange] = continuation
            }
        }
        if !dashboardErrors.isEmpty {
            throw dashboardErrors.removeFirst()
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

    /// Returns once the store has asked for `range`. Lets a test sequence an in-flight read
    /// against a later one without betting on a sleep being long enough under CI load.
    func awaitRequest(for range: HomeLeaderboardRange) async {
        guard !arrivedRanges.contains(range) else { return }
        await withCheckedContinuation { continuation in
            arrivalWaiters[range] = continuation
        }
    }

    private func noteArrival(of range: HomeLeaderboardRange) {
        arrivedRanges.insert(range)
        arrivalWaiters.removeValue(forKey: range)?.resume()
    }

    static let profile = MeResponse(userId: "user-1", displayName: "Ada", memberWalletAddress: "wallet")

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

/// Stands in for `DynamicAuthService`, including its guard: a sign-out is only carried out
/// when the token the server rejected still belongs to the open session. `SessionTokenLedger`
/// is what proves that guard itself; here it is mirrored so the store's *own* half — naming
/// the token each request actually used — is what the assertions are reading.
@MainActor
private final class StubAuth: SessionAuthenticating {
    var accessToken: String? = "token-a"
    /// The tokens the open session will accept a sign-out for.
    var liveTokens: Set<String> = ["token-a"]
    /// Fresh tokens `refreshedAccessToken` hands back, oldest first. Empty means Dynamic has
    /// nothing newer, which is how a really-dead session behaves.
    var freshTokens: [String] = []

    /// Tokens reported through `signOutAfterRejectedSession`, in order.
    var rejectedTokens: [String] = []
    /// Tokens `refreshedAccessToken` was asked about, in order.
    var refreshRequests: [String] = []
    /// Sign-outs that actually took effect, as (reason, token).
    private(set) var signOuts: [(reason: String, token: String)] = []

    func shouldInvalidateBackendSession(serverUserId: String) -> Bool { false }
    func recordBackendSession(userId: String) {}

    func refreshedAccessToken(replacing rejectedToken: String) async throws -> String? {
        refreshRequests.append(rejectedToken)
        guard liveTokens.contains(rejectedToken), !freshTokens.isEmpty else { return nil }
        let fresh = freshTokens.removeFirst()
        liveTokens.insert(fresh)
        accessToken = fresh
        return fresh
    }

    func signOut(reason: String, rejectedToken: String) async {
        guard liveTokens.contains(rejectedToken) else { return }
        signOuts.append((reason, rejectedToken))
        liveTokens.removeAll()
        accessToken = nil
    }

    func signOutAfterRejectedSession(rejectedToken: String) async {
        rejectedTokens.append(rejectedToken)
    }
}

/// The store owns the leaderboard range Home shows. Before this, Home kept its own copy and
/// passed it in, so every refresh started anywhere else quietly reloaded the all-time board.
@MainActor
struct AppSessionStoreLeaderboardRangeTests {
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
        await source.awaitRequest(for: .oneWeek)
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
        await store.awaitDeferredWork()

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

    /// The anti-loop guard. A backend that rejects every token Dynamic mints used to be able to
    /// keep bootstrap refreshing and retrying; it gets exactly one retry and then ends the
    /// session with something to show.
    @Test func bootstrapRetriesOnceAndThenStops() async throws {
        let source = StubDataSource()
        source.openSessionErrors = [MonacoAPIError.httpStatus(401), MonacoAPIError.httpStatus(401)]
        let store = AppSessionStore(apiClient: source)
        let auth = StubAuth()
        auth.freshTokens = ["token-b"]

        await store.bootstrap(auth: auth)

        // One attempt, one retry with the fresh token, and no third.
        #expect(source.openSessionRequests == 2)
        // Only the first failure asked Dynamic for a token; the retry is not allowed to.
        #expect(auth.refreshRequests == ["token-a"])
        #expect(auth.signOuts.map(\.token) == ["token-b"])
    }

    /// A bootstrap whose 401 outlived its sign-in. The member signed out and someone else
    /// signed in while `openSession` was in flight; the rejection that finally lands belongs
    /// to a session that no longer exists. It must not sign out the account signed in now,
    /// nor stamp their login screen with a reason meant for the previous one.
    @Test func aBootstrap401ThatOutlivedItsSignInLeavesTheNewSessionAlone() async throws {
        let source = StubDataSource()
        source.holdOpenSession = true
        source.openSessionErrors = [MonacoAPIError.httpStatus(401)]
        let store = AppSessionStore(apiClient: source)
        let auth = StubAuth()

        let boot = Task { await store.bootstrap(auth: auth) }
        await source.awaitOpenSession()
        // Sign out, then a different member signs in, all while the 401 is on its way back.
        auth.liveTokens = ["token-next"]
        auth.accessToken = "token-next"
        source.releaseOpenSession()
        await boot.value

        #expect(auth.refreshRequests == ["token-a"])
        #expect(auth.signOuts.isEmpty)
        // Still signed in: the stub clears the token on a sign-out that takes effect.
        #expect(auth.accessToken == "token-next")
    }
}

/// A 401 has to name the token the request actually carried. Naming whatever token is
/// current when the reply lands is what lets a stale rejection end a live session.
@MainActor
struct AppSessionStoreRejectedTokenTests {
    @Test func aRefreshReportsTheTokenItsRequestUsed() async throws {
        let source = StubDataSource()
        source.holdRanges = [.all]
        source.dashboardErrors = [MonacoAPIError.httpStatus(401)]
        let store = AppSessionStore(apiClient: source)
        let auth = StubAuth()

        let refresh = Task { await store.refresh(auth: auth) }
        await source.awaitRequest(for: .all)
        // The hourly rotation lands while the read is in flight.
        auth.accessToken = "token-b"
        source.release(.all)
        await refresh.value

        #expect(auth.rejectedTokens == ["token-a"])
    }

    @Test func aDashboardReadReportsTheTokenItsRequestUsed() async throws {
        let source = StubDataSource()
        source.holdRanges = [.oneWeek]
        source.dashboardErrors = [MonacoAPIError.httpStatus(401)]
        let store = AppSessionStore(apiClient: source)
        let auth = StubAuth()

        let select = Task { await store.selectLeaderboardRange(.oneWeek, auth: auth) }
        await source.awaitRequest(for: .oneWeek)
        auth.accessToken = "token-b"
        source.release(.oneWeek)
        await select.value

        #expect(auth.rejectedTokens == ["token-a"])
    }

    /// A poll is not a request the member made. Its 401 is raised so the caller's loop can
    /// back off, and it must not sign anyone out on its own.
    @Test func aPollRaisesIts401RatherThanSigningOut() async throws {
        let source = StubDataSource()
        source.dashboardErrors = [MonacoAPIError.httpStatus(401)]
        let store = AppSessionStore(apiClient: source)
        let auth = StubAuth()

        await #expect(throws: MonacoAPIError.self) {
            try await store.pollLive(auth: auth)
        }

        #expect(auth.rejectedTokens.isEmpty)
        #expect(auth.signOuts.isEmpty)
    }
}
