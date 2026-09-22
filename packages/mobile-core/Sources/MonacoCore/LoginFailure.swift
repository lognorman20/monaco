import Foundation

/// Why a one-time-code step failed, reduced to what the user can act on.
public enum LoginFailure: Equatable, Sendable {
    /// Wrong or expired code.
    case codeRejected
    /// The device could not reach the sign-in provider.
    case offline
    /// Too many attempts; the provider is throttling this phone or email.
    case rateLimited
    /// Anything else. `detail` is provider copy, when it said something useful.
    case other(detail: String?)
}

public enum LoginStep: Equatable, Sendable {
    case sendCode
    case verifyCode
}

public enum LoginFailureCopy {
    public static let sessionExpired = "Your session expired. Sign in again."
    public static let restoreOffline = "Can't reach the sign-in service. Check your connection and try again."
    public static let tokenUnavailable = "Signed in, but couldn't finish. Check your connection and try again."

    public static func message(for failure: LoginFailure, step: LoginStep) -> String {
        switch (failure, step) {
        case (.offline, _):
            return "No connection. Check your internet and try again."
        case (.rateLimited, _):
            return "Too many attempts. Wait a minute, then try again."
        case (.codeRejected, _):
            return "That code didn't work. Check it, or send a new one."
        case (.other(let detail), .sendCode):
            return appending(detail, to: "Couldn't send the code. Try again.")
        case (.other(let detail), .verifyCode):
            return appending(detail, to: "Couldn't sign you in. Try again.")
        }
    }

    /// Maps an HTTP status from the sign-in provider onto a failure.
    public static func failure(forHTTPStatus status: Int, step: LoginStep, detail: String?) -> LoginFailure {
        switch status {
        case 429:
            return .rateLimited
        case 400, 401, 403, 404, 422:
            return step == .verifyCode ? .codeRejected : .other(detail: detail)
        default:
            return .other(detail: detail)
        }
    }

    private static func appending(_ detail: String?, to base: String) -> String {
        let trimmed = detail?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return trimmed.isEmpty ? base : "\(base) \(trimmed)"
    }
}
