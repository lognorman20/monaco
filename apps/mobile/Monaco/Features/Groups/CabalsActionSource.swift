import MonacoCore
import SwiftUI

/// The writes the Cabals tab makes on the member's behalf: start a cabal, join
/// one.
///
/// Behind a protocol for the same reason the tab's reads are. #292's impact is a
/// duplicate cabal and a second on-chain treasury, and a flow that only exists
/// behind a `MonacoAPIClient()` held in a `let` cannot be driven by a test — not
/// the re-entrancy guard, and not what the screen does once the cabal exists.
@MainActor
protocol CabalsActionSource {
    func createGroup(
        name: String,
        joinPolicyMode: String,
        voterSetMode: String,
        voterMemberIds: [String],
        threshold: String,
        voteExpirySeconds: Int64
    ) async throws -> CreateGroupResponse

    func joinGroup(groupId: String) async throws -> JoinGroupOutcome
}

/// Talks to the API. The access token is read at call time, so a screen never
/// has to hold one or decide what a missing one means.
@MainActor
struct LiveCabalsActionSource: CabalsActionSource {
    let auth: PrivyAuthService
    private let apiClient = MonacoAPIClient()

    init(auth: PrivyAuthService) {
        self.auth = auth
    }

    private func token() throws -> String {
        guard let token = auth.accessToken else { throw MonacoAPIError.missingAccessToken }
        return token
    }

    func createGroup(
        name: String,
        joinPolicyMode: String,
        voterSetMode: String,
        voterMemberIds: [String],
        threshold: String,
        voteExpirySeconds: Int64
    ) async throws -> CreateGroupResponse {
        try await apiClient.createGroup(
            accessToken: try token(),
            name: name,
            joinPolicyMode: joinPolicyMode,
            voterSetMode: voterSetMode,
            voterMemberIds: voterMemberIds,
            threshold: threshold,
            voteExpirySeconds: voteExpirySeconds
        )
    }

    func joinGroup(groupId: String) async throws -> JoinGroupOutcome {
        try await apiClient.joinGroup(accessToken: try token(), groupId: groupId)
    }
}
