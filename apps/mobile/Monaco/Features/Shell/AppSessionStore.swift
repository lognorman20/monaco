import MonacoCore
import Observation
import os
import SwiftUI

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

    private let apiClient = MonacoAPIClient()
    private var refreshGeneration = 0
    /// The leaderboard range `dashboard` was last loaded with, so a background poll re-reads
    /// what is on screen instead of resetting Home's picker.
    private var dashboardLeaderboardRange: HomeLeaderboardRange = .all

    var joinedCabals: [HomeGroupBoardRowDTO] {
        (home?.groups ?? []).filter(\.isJoined)
    }

    func bootstrap(auth: PrivyAuthService) async {
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
                await auth.signOutAfterRejectedSession()
                return
            }
            auth.recordBackendSession(userId: session.userId)
            me = session
            isLoading = false
            await refresh(auth: auth, accessToken: token)
        } catch {
            if error.isRequestCancellation { return }
            await handleSessionOpenFailure(error, rejectedToken: token, auth: auth)
        }
    }

    /// A 401 here means the backend would not accept the access token. Tokens last about
    /// an hour, so first ask Privy for a fresh one: if that yields a different token the
    /// gate re-runs `bootstrap` with it. Only when Privy has nothing newer is the token
    /// really bad — most often a Privy app-id / verification-key mismatch between the app
    /// build and the backend env — and we sign out, saying *why* on the login screen.
    private func handleSessionOpenFailure(_ error: Error, rejectedToken: String, auth: PrivyAuthService) async {
        var failure = error
        if case MonacoAPIError.httpStatus(401) = error {
            do {
                if let fresh = try await auth.refreshedAccessToken(replacing: rejectedToken), fresh != rejectedToken {
                    // `auth.accessToken` changed; SessionGateView's task re-runs bootstrap.
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
        errorDebugDetail = mapped.debugDetail
        #endif
    }

    func refresh(
        auth: PrivyAuthService,
        accessToken: String? = nil,
        leaderboardRange: HomeLeaderboardRange = .all
    ) async {
        let token = accessToken ?? auth.accessToken
        guard let token else {
            errorMessage = "Missing sign-in token."
            isLoading = false
            return
        }

        refreshGeneration += 1
        let generation = refreshGeneration

        do {
            isBalanceLoading = platformBalance == nil
            async let dashboardLoad = apiClient.getHomeDashboard(accessToken: token, leaderboardRange: leaderboardRange)
            async let meLoad = apiClient.me(accessToken: token)
            async let balanceLoad = apiClient.getPlatformBalance(accessToken: token)
            let loadedDashboard = try await dashboardLoad
            guard generation == refreshGeneration else { return }
            dashboard = loadedDashboard
            dashboardLeaderboardRange = leaderboardRange
            if let profile = try? await meLoad {
                me = profile
            }
            if let balance = try? await balanceLoad {
                platformBalance = balance
            }
            errorMessage = nil
            isBalanceLoading = false

            Task {
                async let deferred: Void = refreshDeferredHomePayloads(auth: auth, accessToken: token)
                async let pnlSeries: Void = refreshHomePnLSeries(auth: auth, accessToken: token)
                _ = await (deferred, pnlSeries)
            }
        } catch MonacoAPIError.httpStatus(let status) where status == 401 {
            await auth.signOutAfterRejectedSession()
        } catch MonacoAPIError.httpStatus {
            guard generation == refreshGeneration else { return }
            errorMessage = "Couldn't load this. Pull down to try again."
        } catch {
            if error.isRequestCancellation { return }
            guard generation == refreshGeneration else { return }
            errorMessage = "No connection. Check your internet and try again."
        }

        if generation == refreshGeneration {
            isBalanceLoading = false
        }
    }

    /// One background poll of what Home and Profile show: dashboard, balance, joined cabals, and
    /// the 1H curve. Unlike `refresh` it never touches `errorMessage` or a loading flag, and it
    /// only writes values the server actually changed — a poll that fails, or that comes back
    /// identical, leaves the screen exactly as the member last saw it.
    ///
    /// Throws when the dashboard read fails so the caller's poll loop can back off. That includes
    /// a 401: signing the member out is for a request they made, not one they never saw.
    func pollLive(auth: PrivyAuthService) async throws {
        guard let token = auth.accessToken else { return }
        let generation = refreshGeneration
        let leaderboardRange = dashboardLeaderboardRange

        async let dashboardLoad = apiClient.getHomeDashboard(accessToken: token, leaderboardRange: leaderboardRange)
        async let balanceLoad = apiClient.getPlatformBalance(accessToken: token)
        async let homeLoad = apiClient.getHome(accessToken: token)
        async let seriesLoad = apiClient.getHomePnLSeries(accessToken: token, range: .oneHour)

        let loadedDashboard = try await dashboardLoad
        let balance = try? await balanceLoad
        let boards = try? await homeLoad
        let series = try? await seriesLoad

        // A pull-to-refresh or a range change started while this was in flight: theirs is newer.
        guard generation == refreshGeneration, leaderboardRange == dashboardLeaderboardRange,
              !Task.isCancelled else { return }
        QuietUpdate.apply(loadedDashboard, over: dashboard) { dashboard = $0 }
        if let balance { QuietUpdate.apply(balance, over: platformBalance) { platformBalance = $0 } }
        if let boards { QuietUpdate.apply(boards, over: home) { home = $0 } }
        if let series { QuietUpdate.apply(series.points, over: homePnLSeries) { homePnLSeries = $0 } }
    }

    /// Legacy home boards + popular strip. Does not block Home first paint.
    func refreshDeferredHomePayloads(auth: PrivyAuthService, accessToken: String? = nil) async {
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
    func refreshHomePnLSeries(auth: PrivyAuthService, accessToken: String? = nil) async {
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
                await auth.signOutAfterRejectedSession()
            }
        }
    }

    /// After POST /v1/groups: patch joined cabals locally, then refresh home/dashboard
    /// in the background. Skips popular assets so create does not stampede Jupiter.
    func refreshAfterCreate(auth: PrivyAuthService, created: CreateGroupResponse) {
        insertJoinedCabal(from: created)
        Task { await deferredRefreshAfterCreate(auth: auth) }
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

    private func deferredRefreshAfterCreate(auth: PrivyAuthService) async {
        guard let token = auth.accessToken else { return }
        do {
            async let homeLoad = apiClient.getHome(accessToken: token)
            async let dashboardLoad = apiClient.getHomeDashboard(accessToken: token)
            async let balanceLoad = apiClient.getPlatformBalance(accessToken: token)
            home = try await homeLoad
            dashboard = try await dashboardLoad
            if let balance = try? await balanceLoad {
                platformBalance = balance
            }
        } catch {
            if error.isRequestCancellation { return }
            if case MonacoAPIError.httpStatus(let status) = error, status == 401 {
                await auth.signOutAfterRejectedSession()
            }
        }
    }

    func refreshPopular(auth: PrivyAuthService) async {
        guard let token = auth.accessToken else { return }
        do {
            let popular = try await apiClient.getPopularAssets(accessToken: token, limit: 10)
            popularAssets = popular.assets
        } catch {
            if error.isRequestCancellation { return }
            if case MonacoAPIError.httpStatus(let status) = error, status == 401 {
                await auth.signOutAfterRejectedSession()
            }
        }
    }

    func refreshDashboard(auth: PrivyAuthService, leaderboardRange: HomeLeaderboardRange) async {
        guard let token = auth.accessToken else { return }
        do {
            dashboard = try await apiClient.getHomeDashboard(accessToken: token, leaderboardRange: leaderboardRange)
            dashboardLeaderboardRange = leaderboardRange
        } catch {
            if error.isRequestCancellation {
                return
            }
            if case MonacoAPIError.httpStatus(let status) = error, status == 401 {
                await auth.signOutAfterRejectedSession()
            }
        }
    }
}
