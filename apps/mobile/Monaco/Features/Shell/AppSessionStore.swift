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

        if dashboard == nil {
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
            await refresh(auth: auth, accessToken: token)
        } catch MonacoAPIError.httpStatus(let status) where status == 401 {
            await auth.logout()
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "Could not open session (HTTP \(status))."
            isLoading = false
        } catch {
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

        do {
            isBalanceLoading = platformBalance == nil
            async let dashboardLoad = apiClient.getHomeDashboard(accessToken: token, leaderboardRange: leaderboardRange)
            async let meLoad = apiClient.me(accessToken: token)
            async let balanceLoad = apiClient.getPlatformBalance(accessToken: token)
            dashboard = try await dashboardLoad
            if let profile = try? await meLoad {
                me = profile
            }
            if let balance = try? await balanceLoad {
                platformBalance = balance
            }
            errorMessage = nil
            isLoading = false
            isBalanceLoading = false

            Task {
                async let deferred: Void = refreshDeferredHomePayloads(auth: auth, accessToken: token)
                async let pnlSeries: Void = refreshHomePnLSeries(auth: auth, accessToken: token)
                _ = await (deferred, pnlSeries)
            }
        } catch MonacoAPIError.httpStatus(let status) where status == 401 {
            await auth.logout()
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "Could not load home (HTTP \(status))."
            dashboard = nil
        } catch {
            if error.isRequestCancellation {
                isLoading = false
                isBalanceLoading = false
                return
            }
            errorMessage = "Could not load your boards."
            dashboard = nil
        }

        isLoading = false
        isBalanceLoading = false
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
