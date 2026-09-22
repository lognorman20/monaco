import Foundation
import MonacoCore

extension FlowErrorInput {
    /// Reduces whatever a money request threw to what `MoneyFlowCopy` words its failures from.
    /// Anything without an HTTP status that isn't provably "never sent" stays status-less, so
    /// the copy treats it as unconfirmed rather than inviting a second transfer.
    ///
    /// Both error enums are named in full. This module declares its own `MonacoAPIError`, and
    /// an unqualified `MonacoAPIError` here resolves to it, not to MonacoCore's — so a pattern
    /// written for one silently never matches the other, and a MonacoCore 400 or 429 fell
    /// through to "we couldn't confirm that went through".
    init(_ error: Error) {
        switch error {
        case Monaco.MonacoAPIError.httpStatus(let status):
            self.init(status: status)
        case Monaco.MonacoAPIError.apiError(let status, let message):
            self.init(status: status, serverMessage: message)
        case Monaco.MonacoAPIError.rateLimited(let retryAfterSeconds):
            self.init(status: 429, retryAfterSeconds: retryAfterSeconds)
        case MonacoCore.MonacoAPIError.httpStatus(let status):
            self.init(status: status)
        case MonacoCore.MonacoAPIError.rejected(let status, let message):
            self.init(status: status, serverMessage: message)
        case MonacoCore.MonacoAPIError.rateLimited(let retryAfterSeconds):
            self.init(status: 429, retryAfterSeconds: retryAfterSeconds)
        // Nothing was sent: the app never had a token to authorise the request with.
        case Monaco.MonacoAPIError.missingAccessToken:
            self.init(isSignInUnavailable: true)
        // Before the offline check: a refresh failure is rewritten into a URLError that
        // keeps the underlying code, so the commonest one (.notConnectedToInternet) matches
        // `neverSentURLErrorCodes` too. Ordered the other way the sign-in branch is dead for
        // exactly the case it was added for.
        case let error where error.isTokenRefreshFailure:
            self.init(isSignInUnavailable: true)
        case let urlError as URLError where Self.neverSentURLErrorCodes.contains(urlError.code):
            self.init(isOffline: true)
        // `invalidResponse` and `leaveBlocked` (from either enum) deliberately have no case:
        // an unreadable reply is genuinely unconfirmed, and a blocked leave never reaches
        // money copy (GroupDetailView words it itself). Giving either a fabricated status
        // here would word it as an in-flight money request. Pinned by tests.
        default:
            self.init()
        }
    }
}
