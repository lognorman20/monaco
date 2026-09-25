import Foundation
import MonacoCore

/// The invite reads and writes the app makes: the cabal's code on its details sheet, and the
/// preview and join on the join screen.
///
/// Behind a protocol so the sample harness and the tests drive every state (loading, a dead
/// code, a rate limit) without a backend.
@MainActor
protocol InviteSource {
    /// The cabal's live code, made on the spot if it has none. Members only.
    func currentInvite(groupId: String) async throws -> InviteDTO
    /// A new code; the old one stops working.
    func newInvite(groupId: String) async throws -> InviteDTO
    /// The public preview of the cabal behind a code.
    func preview(code: String) async throws -> InvitePreviewDTO
    /// Joins, or asks to join, through a code.
    func join(code: String) async throws -> InviteJoinResult
}

/// Talks to the API with the signed-in member's token, read per call: a sheet can outlive
/// the token it was opened with.
@MainActor
struct LiveInviteSource: InviteSource {
    let accessToken: () -> String?

    init(auth: PrivyAuthService) {
        accessToken = { [weak auth] in auth?.accessToken }
    }

    private func client() -> MonacoCore.MonacoAPIClient {
        let token = accessToken()
        return MonacoCore.MonacoAPIClient(baseURL: Config.apiBaseURL, accessTokenProvider: { token })
    }

    private func signedInClient() throws -> MonacoCore.MonacoAPIClient {
        guard let token = accessToken(), !token.isEmpty else { throw MonacoAPIError.missingAccessToken }
        return MonacoCore.MonacoAPIClient(baseURL: Config.apiBaseURL, accessTokenProvider: { token })
    }

    func currentInvite(groupId: String) async throws -> InviteDTO {
        try await signedInClient().currentInvite(groupId: groupId)
    }

    func newInvite(groupId: String) async throws -> InviteDTO {
        try await signedInClient().createInvite(groupId: groupId)
    }

    func preview(code: String) async throws -> InvitePreviewDTO {
        // Public: works before sign-in finishes too.
        try await client().invitePreview(code: code)
    }

    func join(code: String) async throws -> InviteJoinResult {
        try await signedInClient().joinGroup(inviteCode: code)
    }
}

/// The status a failed invite call answered with, from either API client's error type.
enum InviteErrorStatus {
    static func of(_ error: Error) -> Int? {
        if let core = error as? MonacoCore.MonacoAPIError {
            return core.statusCode
        }
        switch error as? MonacoAPIError {
        case .httpStatus(let status): return status
        case .apiError(let status, _): return status
        default: return nil
        }
    }

    static func isOffline(_ error: Error) -> Bool {
        guard let urlError = error as? URLError else { return false }
        return [.notConnectedToInternet, .networkConnectionLost, .timedOut, .cannotConnectToHost, .cannotFindHost]
            .contains(urlError.code)
    }
}
