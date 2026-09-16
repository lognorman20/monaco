import Foundation

/// Refreshes group view and home boards after redeem success (M5-T19).
struct RedeemBoardRefreshCoordinator {
    let apiClient: MonacoAPIClient

    func refreshAfterSuccess(accessToken: String, groupId: String) async throws {
        _ = try await apiClient.getGroupView(accessToken: accessToken, groupId: groupId)
        _ = try await apiClient.getHome(accessToken: accessToken)
    }
}
