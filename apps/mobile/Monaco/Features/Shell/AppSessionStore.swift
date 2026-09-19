import MonacoCore
import Observation
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
    var isLoading = true

    private let apiClient = MonacoAPIClient()
    private var refreshGeneration = 0

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

        do {
            let session = try await apiClient.openSession(accessToken: token)
            if auth.shouldInvalidateBackendSession(serverUserId: session.userId) {
                await auth.logout()
                return
            }
            auth.recordBackendSession(userId: session.userId)
            me = session
            isLoading = false
            await refresh(auth: auth, accessToken: token)
        } catch MonacoAPIError.httpStatus(let status) where status == 401 {
            await auth.logout()
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "Could not open session (HTTP \(status))."
            isLoading = false
        } catch {
            if error.isRequestCancellation { return }
            errorMessage = "Could not connect to Monaco."
            isLoading = false
        }
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
            await auth.logout()
        } catch MonacoAPIError.httpStatus(let status) {
            guard generation == refreshGeneration else { return }
            errorMessage = "Could not load home (HTTP \(status))."
        } catch {
            if error.isRequestCancellation { return }
            guard generation == refreshGeneration else { return }
            errorMessage = "Could not load your boards."
        }

        if generation == refreshGeneration {
            isBalanceLoading = false
        }
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
                await auth.logout()
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
                await auth.logout()
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
                await auth.logout()
            }
        }
    }

    func refreshDashboard(auth: PrivyAuthService, leaderboardRange: HomeLeaderboardRange) async {
        guard let token = auth.accessToken else { return }
        do {
            dashboard = try await apiClient.getHomeDashboard(accessToken: token, leaderboardRange: leaderboardRange)
        } catch {
            if error.isRequestCancellation {
                return
            }
            if case MonacoAPIError.httpStatus(let status) = error, status == 401 {
                await auth.logout()
            }
        }
    }
}
