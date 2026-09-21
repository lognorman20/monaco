import Foundation
import MonacoCore

/// Maps a session-open failure onto user-facing copy plus a developer-only debug
/// detail line. Pure function — no I/O — so it's simple to unit test.
///
/// Distinguishes:
///  - the access token could not be refreshed → the sign-in message, even when the
///    refresh failed for want of a connection: the request was refused, not unsent
///  - can't connect at all (URLError / NSURLErrorDomain) → "Can't reach Monaco…"
///  - 5xx from the backend → "Monaco's server hit a problem…"
///  - 401 opening the session → a distinct message meant for the *login* screen,
///    since the caller signs the user out rather than showing this on the gate.
///  - anything else → a generic, still-polished fallback (never a raw status code
///    or `localizedDescription` in the user-facing message).
enum SessionErrorMapping {
    struct Description: Equatable {
        /// Release-safe copy shown to every user.
        let message: String
        /// Status code / URLError code + API base URL. Callers only display this
        /// in DEBUG builds; it's still computed here so it's easy to log always.
        let debugDetail: String
    }

    static let signInVerificationFailureMessage = "We couldn't verify your sign-in. Try again."

    private static let cantConnectMessage = "Can't reach Monaco. Check your connection and try again."
    private static let serverErrorMessage = "Monaco's server hit a problem. Try again in a moment."
    private static let genericMessage = "Couldn't open Monaco. Try again."

    static func describe(_ error: Error, apiBaseURL: URL) -> Description {
        let origin = "API: \(apiBaseURL.absoluteString)"

        if let apiError = error as? MonacoAPIError, case .httpStatus(let status) = apiError {
            return httpDescription(status: status, origin: origin)
        }

        // Before the URLError branch: the transport rewrites a failed refresh into a URLError
        // that keeps the underlying code, which would otherwise read as "Can't reach Monaco".
        if error.isTokenRefreshFailure {
            return Description(
                message: signInVerificationFailureMessage,
                debugDetail: "Token refresh failed: \(String(describing: error)) — \(origin)"
            )
        }

        if let urlError = error as? URLError {
            return Description(
                message: cantConnectMessage,
                debugDetail: "URLError \(urlError.code.rawValue) (\(codeName(urlError.code))) — \(origin)"
            )
        }

        let nsError = error as NSError
        if nsError.domain == NSURLErrorDomain {
            return Description(
                message: cantConnectMessage,
                debugDetail: "URLError \(nsError.code) — \(origin)"
            )
        }

        return Description(
            message: genericMessage,
            debugDetail: "\(String(describing: error)) — \(origin)"
        )
    }

    private static func httpDescription(status: Int, origin: String) -> Description {
        let detail = "HTTP \(status) — \(origin)"
        switch status {
        case 401:
            return Description(message: signInVerificationFailureMessage, debugDetail: detail)
        case 500...599:
            return Description(message: serverErrorMessage, debugDetail: detail)
        default:
            return Description(message: genericMessage, debugDetail: detail)
        }
    }

    /// `URLError.Code` has no public name accessor; spell out the ones that show up
    /// in the field so the debug line reads better than a bare integer.
    private static func codeName(_ code: URLError.Code) -> String {
        switch code {
        case .notConnectedToInternet: return "notConnectedToInternet"
        case .timedOut: return "timedOut"
        case .cannotConnectToHost: return "cannotConnectToHost"
        case .cannotFindHost: return "cannotFindHost"
        case .networkConnectionLost: return "networkConnectionLost"
        case .dnsLookupFailed: return "dnsLookupFailed"
        case .secureConnectionFailed: return "secureConnectionFailed"
        default: return "code \(code.rawValue)"
        }
    }
}
