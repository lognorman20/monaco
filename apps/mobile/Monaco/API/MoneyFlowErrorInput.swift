import Foundation
import MonacoCore

extension FlowErrorInput {
    /// Reduces whatever a money request threw to what `MoneyFlowCopy` words its failures from.
    /// Anything without an HTTP status that isn't provably "never sent" stays status-less, so
    /// the copy treats it as unconfirmed rather than inviting a second transfer.
    init(_ error: Error) {
        switch error {
        case MonacoAPIError.httpStatus(let status):
            self.init(status: status)
        case MonacoAPIError.apiError(let status, let message):
            self.init(status: status, serverMessage: message)
        case MonacoCore.MonacoAPIError.httpStatus(let status, _):
            self.init(status: status)
        case MonacoCore.MonacoAPIError.rejected(let status, let message, _):
            self.init(status: status, serverMessage: message)
        case MonacoCore.MonacoAPIError.rateLimited(let retryAfterSeconds, _):
            self.init(status: 429, retryAfterSeconds: retryAfterSeconds)
        // Nothing was sent: the app never confirmed the member is signed in.
        case MonacoAPIError.missingAccessToken:
            self.init(isSignInUnavailable: true)
        // Before the offline check: a refresh failure is rewritten into a URLError that
        // keeps the underlying code, so the commonest one (.notConnectedToInternet) matches
        // `neverSentURLErrorCodes` too. Ordered the other way the sign-in branch is dead for
        // exactly the case it was added for.
        case let error where error.isTokenRefreshFailure:
            self.init(isSignInUnavailable: true)
        case let urlError as URLError where Self.neverSentURLErrorCodes.contains(urlError.code):
            self.init(isOffline: true)
        // `invalidResponse` and `leaveBlocked` deliberately have no case: an unreadable
        // reply is genuinely unconfirmed, and a blocked leave never reaches money copy
        // (GroupDetailView catches it with its own wording). Giving either a fabricated
        // status here would word it as an in-flight money request. Pinned by tests.
        default:
            self.init()
        }
    }
}
