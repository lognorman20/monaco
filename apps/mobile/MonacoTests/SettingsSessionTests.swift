// Only what this file needs from MonacoCore: importing all of it would make the DTO names the
// app also declares (HomeDashboardDTO, PlatformBalanceDTO…) ambiguous here.
import struct MonacoCore.MeDTO
import enum MonacoCore.SettingsCopy
import Testing
@testable import Monaco

/// Opens every session with the error the test queued.
@MainActor
private final class DeletedAccountDataSource: AppSessionDataSource {
    var openSessionError: Error = MonacoAPIError.httpStatus(410)

    func openSession(accessToken: String) async throws -> MeResponse { throw openSessionError }
    func me(accessToken: String) async throws -> MeResponse { throw openSessionError }

    func getPlatformBalance(accessToken: String) async throws -> PlatformBalanceDTO {
        PlatformBalanceDTO(availableUsdcMicros: 0, memberWalletAddress: "wallet", pendingAllocationMicros: 0)
    }

    func getHome(accessToken: String) async throws -> HomeViewDTO {
        HomeViewDTO(groups: [], people: [])
    }

    func getHomeDashboard(accessToken: String, leaderboardRange: HomeLeaderboardRange) async throws -> HomeDashboardDTO {
        HomeDashboardDTO(
            netWorthUsd: "0.00",
            netWorthDollarPnl: "+0.00",
            netWorthPercentReturn: nil,
            myGroups: [],
            pnlSeries1H: [],
            leaderboard: HomeLeaderboardSectionDTO(range: leaderboardRange.rawValue, people: []),
            missedProposals: []
        )
    }

    func getHomePnLSeries(accessToken: String, range: HomeLeaderboardRange) async throws -> HomePnLSeriesDTO {
        HomePnLSeriesDTO(points: [])
    }

    func getPopularAssets(accessToken: String, limit: Int) async throws -> PopularAssetsResponse {
        PopularAssetsResponse(assets: [])
    }
}

@MainActor
private final class RecordingAuth: SessionAuthenticating {
    var accessToken: String? = "token-a"
    private(set) var signOuts: [(reason: String, token: String)] = []
    private(set) var rejected: [String] = []

    func shouldInvalidateBackendSession(serverUserId: String) -> Bool { false }
    func recordBackendSession(userId: String) {}
    func refreshedAccessToken(replacing rejectedToken: String) async throws -> String? { nil }

    func signOut(reason: String, rejectedToken: String) async {
        signOuts.append((reason, rejectedToken))
        accessToken = nil
    }

    func signOutAfterRejectedSession(rejectedToken: String) async {
        rejected.append(rejectedToken)
    }
}

/// A login whose account was deleted gets 410 from `POST /v1/auth/session`. The gate must sign
/// out and say why on sign-in, not sit on "Your account didn't load" with a Try again that can
/// never work.
@MainActor
struct DeletedAccountSessionTests {
    @Test func a410SignsOutWithTheDeletedMessage() async {
        let store = AppSessionStore(apiClient: DeletedAccountDataSource())
        let auth = RecordingAuth()

        await store.bootstrap(auth: auth)

        #expect(auth.signOuts.count == 1)
        #expect(auth.signOuts.first?.reason == SettingsCopy.deletedElsewhere)
        #expect(auth.signOuts.first?.token == "token-a")
        #expect(store.errorMessage == nil)
    }

    @Test func otherFailuresStillShowTheGate() async {
        let source = DeletedAccountDataSource()
        source.openSessionError = MonacoAPIError.httpStatus(500)
        let store = AppSessionStore(apiClient: source)
        let auth = RecordingAuth()

        await store.bootstrap(auth: auth)

        #expect(auth.signOuts.isEmpty)
        #expect(store.errorMessage != nil)
    }
}
