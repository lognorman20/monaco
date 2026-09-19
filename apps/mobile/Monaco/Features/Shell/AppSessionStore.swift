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

        if home == nil {
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
            async let homeLoad = apiClient.getHome(accessToken: token)
            async let dashboardLoad = apiClient.getHomeDashboard(accessToken: token, leaderboardRange: leaderboardRange)
            async let meLoad = apiClient.me(accessToken: token)
            async let balanceLoad = apiClient.getPlatformBalance(accessToken: token)
            home = try await homeLoad
            dashboard = try await dashboardLoad
            if let profile = try? await meLoad {
                me = profile
            }
            if let balance = try? await balanceLoad {
                platformBalance = balance
            }
            errorMessage = nil
        } catch MonacoAPIError.httpStatus(let status) where status == 401 {
            await auth.logout()
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "Could not load home (HTTP \(status))."
            home = nil
        } catch {
            if error.isRequestCancellation {
                isLoading = false
                isBalanceLoading = false
                return
            }
            errorMessage = "Could not load your boards."
            home = nil
        }

        isLoading = false
        isBalanceLoading = false
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
