import Foundation
import MonacoCore

/// A server reply that rejected the session, carrying the access token that request sent.
///
/// A screen model that learns of a 401 keeps this token rather than a bare flag, so its view
/// can end the session through `signOutAfterRejectedSession(rejectedToken:)`. That overload
/// ignores a token the current sign-in never used, so a 401 that outlived its sign-in cannot
/// end the session that replaced it.
struct RejectedSession: Error, Equatable {
    let token: String

    /// True for the replies that mean the server no longer accepts the token: a 401, with or
    /// without an error body, from either client. The app-target client and the `MonacoCore`
    /// client each declare their own error type, and a 401 from either ends the session.
    static func isRejection(_ error: Error) -> Bool {
        switch error {
        case MonacoAPIError.httpStatus(401), MonacoAPIError.apiError(status: 401, message: _):
            return true
        case let coreError as MonacoCore.MonacoAPIError:
            return coreError.statusCode == 401
        default:
            return false
        }
    }

    /// Sends `request` with `token`. A 401 comes back as `RejectedSession` naming that token.
    ///
    /// The one place a rejection is tied to the token that request carried. It does not end
    /// the session itself: the caller reports the token through the guarded
    /// `signOutAfterRejectedSession(rejectedToken:)`, which is the one sign-out contract.
    static func sending<T>(token: String, _ request: (String) async throws -> T) async throws -> T {
        do {
            return try await request(token)
        } catch where isRejection(error) {
            throw RejectedSession(token: token)
        }
    }
}

extension DynamicAuthService {
    /// Runs `request` with the current access token. A 401 comes back as `RejectedSession`
    /// naming that token.
    func sendingAccessToken<T>(_ request: (String) async throws -> T) async throws -> T {
        guard let token = accessToken else { throw MonacoAPIError.missingAccessToken }
        return try await RejectedSession.sending(token: token, request)
    }
}
