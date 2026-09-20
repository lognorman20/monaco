import MonacoCore
import Observation
import os
import SwiftUI

/// The reads every tab shares. `MonacoAPIClient` is the production implementation;
/// tests inject a stub so store behaviour can be checked without a server.
@MainActor
protocol AppSessionDataSource {
    func openSession(accessToken: String) async throws -> MeResponse
    func me(accessToken: String) async throws -> MeResponse
    func getPlatformBalance(accessToken: String) async throws -> PlatformBalanceDTO
    func getHome(accessToken: String) async throws -> HomeViewDTO
    func getHomeDashboard(accessToken: String, leaderboardRange: HomeLeaderboardRange) async throws -> HomeDashboardDTO
    func getHomePnLSeries(accessToken: String, range: HomeLeaderboardRange) async throws -> HomePnLSeriesDTO
    func getPopularAssets(accessToken: String, limit: Int) async throws -> PopularAssetsResponse
}

extension MonacoAPIClient: AppSessionDataSource {}

/// The session the store reads tokens from and reports rejected ones to. `PrivyAuthService`
/// is the only implementation outside tests.
@MainActor
protocol SessionAuthenticating: AnyObject {
    var accessToken: String? { get }
    func shouldInvalidateBackendSession(serverUserId: String) -> Bool
    func recordBackendSession(userId: String)
    func refreshedAccessToken(replacing rejectedToken: String) async throws -> String?
    func signOut(reason: String) async
    func signOutAfterRejectedSession(rejectedToken: String) async
}

extension PrivyAuthService: SessionAuthenticating {}

/// Shared post-auth home + profile payload. Tabs read this instead of a one-shot DTO.
@Observable
@MainActor
final class AppSessionStore {
    var home: HomeViewDTO?
    var dashboard: HomeDashboardDTO?
    var me: MeResponse?
    var platformBalance: PlatformBalanceDTO?
    var popularAssets: [MarketAssetDTO] = []
    var homePnLSeries: [HomePnLSeriesPointDTO]?
    var isHomePnLSeriesLoading = false
    var isBalanceLoading = false
    var errorMessage: String?
    #if DEBUG
    /// Status code / URLError code + API base URL, shown under `errorMessage` in
    /// DEBUG builds only so devs can debug straight from the gate screen.
    var errorDebugDetail: String?
    #endif
    var isLoading = true

    /// The leaderboard range on screen, owned here so it survives every other refresh.
    /// Home used to keep its own copy and pass it in, so a refresh started anywhere else
    /// (Profile pull-to-refresh, a name save, joining a cabal, the hourly token rotation)
    /// quietly reloaded the all-time board under Home's 1W chip and every poll after it
    /// kept the wrong board. Change it through `selectLeaderboardRange` only.
    private(set) var leaderboardRange: HomeLeaderboardRange = .all

    private let apiClient: AppSessionDataSource
    private var refreshGeneration = 0
    /// Orders dashboard responses on their own, so two quick range taps can't land out of
    /// order and leave the older board under the newer chip.
    private var dashboardGeneration = 0
    /// Home boards, popular assets and the P&L curve: started by a refresh but not awaited by
    /// it. Owned here so the next refresh cancels what the last one left running, instead of
    /// letting a session the member has left behind keep writing.
    private var deferredWork: [Task<Void, Never>] = []
    /// Bumped by every self-profile write, so a `/v1/me` read that started before the write
    /// cannot put the old name back.
    private var profileWriteGeneration = 0

    init(apiClient: AppSessionDataSource = MonacoAPIClient()) {
        self.apiClient = apiClient
    }

    var joinedCabals: [HomeGroupBoardRowDTO] {
        (home?.groups ?? []).filter(\.isJoined)
    }

    func bootstrap(auth: SessionAuthenticating) async {
        await bootstrap(auth: auth, retryingRejectedToken: true)
    }

    private func bootstrap(auth: SessionAuthenticating, retryingRejectedToken: Bool) async {
        guard let token = auth.accessToken else {
            home = nil
            me = nil
            errorMessage = "Missing sign-in token."
            isLoading = false
            return
        }

        if me == nil {
            isLoading = true
        }
        errorMessage = nil
        #if DEBUG
        errorDebugDetail = nil
        #endif

        do {
            let session = try await apiClient.openSession(accessToken: token)
            if auth.shouldInvalidateBackendSession(serverUserId: session.userId) {
                await auth.signOutAfterRejectedSession(rejectedToken: token)
                return
            }
            auth.recordBackendSession(userId: session.userId)
            me = session
            isLoading = false
            // `openSession` just returned the profile: don't ask for it again.
            await refresh(auth: auth, accessToken: token, includeProfile: false)
        } catch {
            if error.isRequestCancellation { return }
            await handleSessionOpenFailure(
                error,
                rejectedToken: token,
                auth: auth,
                mayRetry: retryingRejectedToken
            )
        }
    }

    /// A 401 here means the backend would not accept the access token. Tokens last about
    /// an hour, so first ask Privy for a fresh one and retry with it. Only when Privy has
    /// nothing newer is the token really bad — most often a Privy app-id / verification-key
    /// mismatch between the app build and the backend env — and we sign out, saying *why*
    /// on the login screen. `mayRetry` is false on the retry itself, so a backend that
    /// rejects every token Privy mints cannot loop.
    private func handleSessionOpenFailure(
        _ error: Error,
        rejectedToken: String,
        auth: SessionAuthenticating,
        mayRetry: Bool
    ) async {
        var failure = error
        if case MonacoAPIError.httpStatus(401) = error, mayRetry {
            do {
                if let fresh = try await auth.refreshedAccessToken(replacing: rejectedToken), fresh != rejectedToken {
                    // Retry here: bootstrap is keyed on who is signed in, not on the token
                    // string, so a rotation no longer restarts the session by itself.
                    await bootstrap(auth: auth, retryingRejectedToken: false)
                    return
                }
            } catch {
                // Couldn't reach Privy to refresh. That's a connection problem, not a bad session.
                failure = error
            }
        }

        isLoading = false
        let mapped = SessionErrorMapping.describe(failure, apiBaseURL: Config.apiBaseURL)
        AppLogger.session.error("POST /v1/auth/session failed: \(mapped.debugDetail, privacy: .public)")

        if case MonacoAPIError.httpStatus(401) = failure {
            await auth.signOut(reason: mapped.message)
            return
        }

        errorMessage = mapped.message
        #if DEBUG
        errorDebugDetail = "\(mapped.debugDetail)\n\(Config.api.debugSummary)"
        #endif
    }

    /// Reloads what Home and Profile show. `leaderboardRange` is a *selection*: pass it only
    /// when the member picked a range, and leave it out everywhere else so the board they are
    /// looking at survives the refresh.
    func refresh(
        auth: SessionAuthenticating,
        accessToken: String? = nil,
        leaderboardRange: HomeLeaderboardRange? = nil,
        includeProfile: Bool = true
    ) async {
        let token = accessToken ?? auth.accessToken
        guard let token else {
            errorMessage = "Missing sign-in token."
            isLoading = false
            return
        }
        if let leaderboardRange {
            self.leaderboardRange = leaderboardRange
        }

        cancelDeferredWork()
        refreshGeneration += 1
        let generation = refreshGeneration
        let request = beginDashboardRequest()
        let profileGeneration = profileWriteGeneration

        do {
            isBalanceLoading = platformBalance == nil
            async let dashboardLoad = apiClient.getHomeDashboard(
                accessToken: token,
                leaderboardRange: request.range
            )
            async let meLoad: MeResponse? = includeProfile ? await self.loadProfile(accessToken: token) : nil
            async let balanceLoad = apiClient.getPlatformBalance(accessToken: token)
            let loadedDashboard = try await dashboardLoad
            guard generation == refreshGeneration else { return }
            apply(loadedDashboard, for: request)
            // A rename that landed while this was in flight is newer than what /v1/me says.
            if let profile = await meLoad, profileGeneration == profileWriteGeneration {
                me = profile
            }
            if let balance = try? await balanceLoad {
                platformBalance = balance
            }
            errorMessage = nil
            isBalanceLoading = false

            startDeferredWork { [self] in
                async let deferred: Void = refreshDeferredHomePayloads(auth: auth, accessToken: token)
                async let pnlSeries: Void = refreshHomePnLSeries(auth: auth, accessToken: token)
                _ = await (deferred, pnlSeries)
            }
        } catch MonacoAPIError.httpStatus(let status) where status == 401 {
            await auth.signOutAfterRejectedSession(rejectedToken: token)
        } catch MonacoAPIError.httpStatus {
            guard generation == refreshGeneration else { return }
            errorMessage = "Couldn't load this. Try again."
        } catch {
            if error.isRequestCancellation { return }
            guard generation == refreshGeneration else { return }
            errorMessage = "No connection. Check your internet and try again."
        }

        if generation == refreshGeneration {
            isBalanceLoading = false
        }
    }

    private func loadProfile(accessToken: String) async -> MeResponse? {
        try? await apiClient.me(accessToken: accessToken)
    }

    /// One background poll of what Home and Profile show: dashboard, balance, joined cabals, and
    /// the 1H curve. Unlike `refresh` it never raises an error or a loading flag, and it
    /// only writes values the server actually changed — a poll that fails, or that comes back
    /// identical, leaves the screen exactly as the member last saw it. A poll that lands does
    /// clear a stale error banner, since the condition it described is over.
    ///
    /// Throws when the dashboard read fails so the caller's poll loop can back off. That includes
    /// a 401: signing the member out is for a request they made, not one they never saw.
    func pollLive(auth: SessionAuthenticating) async throws {
        guard let token = auth.accessToken else { return }
        let generation = refreshGeneration
        let request = currentDashboardRequest()

        async let dashboardLoad = apiClient.getHomeDashboard(accessToken: token, leaderboardRange: request.range)
        async let balanceLoad = apiClient.getPlatformBalance(accessToken: token)
        async let homeLoad = apiClient.getHome(accessToken: token)
        async let seriesLoad = apiClient.getHomePnLSeries(accessToken: token, range: .oneHour)

        let loadedDashboard = try await dashboardLoad
        let balance = try? await balanceLoad
        let boards = try? await homeLoad
        let series = try? await seriesLoad

        // A pull-to-refresh or a range change started while this was in flight: theirs is newer.
        guard generation == refreshGeneration, isCurrent(request), !Task.isCancelled else { return }
        QuietUpdate.apply(loadedDashboard, over: dashboard) { dashboard = $0 }
        if let balance { QuietUpdate.apply(balance, over: platformBalance) { platformBalance = $0 } }
        if let boards { QuietUpdate.apply(boards, over: home) { home = $0 } }
        if let series { QuietUpdate.apply(series.points, over: homePnLSeries) { homePnLSeries = $0 } }
        errorMessage = nil
    }

    /// Legacy home boards + popular strip. Does not block Home first paint.
    func refreshDeferredHomePayloads(auth: SessionAuthenticating, accessToken: String? = nil) async {
        await refreshHomeBoards(accessToken: accessToken ?? auth.accessToken)
        await refreshPopular(auth: auth)
    }

    /// Loads GET /v1/home for profile/cabals surfaces. Create-group flows (209) can call this alone.
    func refreshHomeBoards(accessToken: String?) async {
        guard let accessToken else { return }
        do {
            home = try await apiClient.getHome(accessToken: accessToken)
        } catch {
            if error.isRequestCancellation { return }
            if case MonacoAPIError.httpStatus(let status) = error, status == 401 {
                return
            }
        }
    }

    /// GET /v1/home/pnl-series for the Home chart. Does not block login or dashboard shell.
    func refreshHomePnLSeries(auth: SessionAuthenticating, accessToken: String? = nil) async {
        let token = accessToken ?? auth.accessToken
        guard let token else { return }
        isHomePnLSeriesLoading = homePnLSeries == nil
        defer { isHomePnLSeriesLoading = false }
        do {
            let series = try await apiClient.getHomePnLSeries(accessToken: token, range: .oneHour)
            homePnLSeries = series.points
        } catch {
            if error.isRequestCancellation { return }
            if case MonacoAPIError.httpStatus(let status) = error, status == 401 {
                await auth.signOutAfterRejectedSession(rejectedToken: token)
            }
        }
    }

    /// After POST /v1/groups: patch joined cabals locally, then refresh home/dashboard
    /// in the background. Skips popular assets so create does not stampede Jupiter.
    func refreshAfterCreate(auth: SessionAuthenticating, created: CreateGroupResponse) {
        insertJoinedCabal(from: created)
        startDeferredWork { [self] in await deferredRefreshAfterCreate(auth: auth) }
    }

    private func insertJoinedCabal(from created: CreateGroupResponse) {
        let row = HomeGroupBoardRowDTO(
            groupId: created.groupId,
            name: created.name,
            potValueUsd: "0.00",
            percentReturn: nil,
            dollarPnl: "+0.00",
            isJoined: true
        )
        if let current = home {
            guard !current.groups.contains(where: { $0.groupId == created.groupId }) else { return }
            home = HomeViewDTO(groups: [row] + current.groups, people: current.people)
        } else {
            home = HomeViewDTO(groups: [row], people: [])
        }
    }

    private func deferredRefreshAfterCreate(auth: SessionAuthenticating) async {
        guard let token = auth.accessToken else { return }
        let request = beginDashboardRequest()
        do {
            async let homeLoad = apiClient.getHome(accessToken: token)
            async let dashboardLoad = apiClient.getHomeDashboard(
                accessToken: token,
                leaderboardRange: request.range
            )
            async let balanceLoad = apiClient.getPlatformBalance(accessToken: token)
            home = try await homeLoad
            apply(try await dashboardLoad, for: request)
            if let balance = try? await balanceLoad {
                platformBalance = balance
            }
        } catch {
            if error.isRequestCancellation { return }
            if case MonacoAPIError.httpStatus(let status) = error, status == 401 {
                await auth.signOutAfterRejectedSession(rejectedToken: token)
            }
        }
    }

    func refreshPopular(auth: SessionAuthenticating) async {
        guard let token = auth.accessToken else { return }
        do {
            let popular = try await apiClient.getPopularAssets(accessToken: token, limit: 10)
            popularAssets = popular.assets
        } catch {
            if error.isRequestCancellation { return }
            if case MonacoAPIError.httpStatus(let status) = error, status == 401 {
                await auth.signOutAfterRejectedSession(rejectedToken: token)
            }
        }
    }

    // MARK: Profile writes

    /// Called by a self-profile write before it applies the server's copy, so a `/v1/me`
    /// read already in flight cannot overwrite it.
    func noteProfileWrite() {
        profileWriteGeneration += 1
    }

    /// After a profile write: the member's name and photo are stale on every board. Reload
    /// them in the background and quietly — the save already succeeded, so neither a spinner
    /// nor a failed reload belongs on screen.
    func refreshBoardsAfterProfileWrite(auth: SessionAuthenticating) {
        startDeferredWork { [self] in
            try? await pollLive(auth: auth)
        }
    }

    /// The member picked a range on Home. Records the selection so every later read — polls
    /// included — asks for that board, then reloads it.
    func selectLeaderboardRange(_ range: HomeLeaderboardRange, auth: SessionAuthenticating) async {
        await refreshDashboard(auth: auth, leaderboardRange: range)
    }

    func refreshDashboard(auth: SessionAuthenticating, leaderboardRange: HomeLeaderboardRange) async {
        guard let token = auth.accessToken else { return }
        self.leaderboardRange = leaderboardRange
        let request = beginDashboardRequest()
        do {
            let loaded = try await apiClient.getHomeDashboard(
                accessToken: token,
                leaderboardRange: request.range
            )
            apply(loaded, for: request)
        } catch {
            if error.isRequestCancellation {
                return
            }
            if case MonacoAPIError.httpStatus(let status) = error, status == 401 {
                await auth.signOutAfterRejectedSession(rejectedToken: token)
            }
        }
    }

    // MARK: Dashboard ordering

    /// A dashboard read in flight: the range it asked for, and where it sits in the order
    /// of reads so a slower older one can't overwrite a newer board.
    private struct DashboardRequest {
        let range: HomeLeaderboardRange
        let generation: Int
    }

    /// For reads the member asked for. Supersedes everything already in flight.
    private func beginDashboardRequest() -> DashboardRequest {
        dashboardGeneration += 1
        return DashboardRequest(range: leaderboardRange, generation: dashboardGeneration)
    }

    /// For background polls, which must never supersede a read the member asked for.
    private func currentDashboardRequest() -> DashboardRequest {
        DashboardRequest(range: leaderboardRange, generation: dashboardGeneration)
    }

    private func isCurrent(_ request: DashboardRequest) -> Bool {
        request.generation == dashboardGeneration && request.range == leaderboardRange
    }

    private func apply(_ loaded: HomeDashboardDTO, for request: DashboardRequest) {
        guard isCurrent(request) else { return }
        dashboard = loaded
    }

    /// Runs background work owned by this session.
    private func startDeferredWork(_ work: @escaping @MainActor () async -> Void) {
        deferredWork.removeAll(where: \.isCancelled)
        deferredWork.append(Task { await work() })
    }

    /// Drops whatever the last refresh left running, so it cannot write over what the
    /// refresh that replaced it is about to load.
    private func cancelDeferredWork() {
        for task in deferredWork {
            task.cancel()
        }
        deferredWork.removeAll()
    }
}
