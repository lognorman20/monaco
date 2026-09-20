import Foundation

/// A failure a member can read and act on, plus whether trying again is worth anything.
public struct FlowFailure: Equatable, Sendable {
    public let message: String
    /// False when the same request would fail the same way (bad address, not a member) or
    /// when the first one may have gone through: the screen must not invite a blind retry.
    public let isRetryable: Bool
    /// One short line telling the member what to do next, when there is something to do.
    public let nextStep: String?

    public init(message: String, isRetryable: Bool, nextStep: String? = nil) {
        self.message = message
        self.isRetryable = isRetryable
        self.nextStep = nextStep
    }

    /// Message and next step as one line, for a toast.
    public var summary: String {
        guard let nextStep else { return message }
        return "\(message) \(nextStep)"
    }
}

/// A transport failure reduced to the three things the copy actually depends on.
/// Both API client flavours (`MonacoCore.MonacoAPIError` and the app's own) map onto this,
/// so the wording of a failed cash out lives in one tested place.
public struct FlowErrorInput: Equatable, Sendable {
    public let status: Int?
    public let serverMessage: String?
    /// True only when the request provably never left the device. A timeout or a dropped
    /// connection is not offline: the server may have acted on it.
    public let isOffline: Bool

    public init(status: Int? = nil, serverMessage: String? = nil, isOffline: Bool = false) {
        self.status = status
        self.serverMessage = serverMessage?.trimmingCharacters(in: .whitespacesAndNewlines)
        self.isOffline = isOffline
    }

    public static func offline() -> FlowErrorInput { FlowErrorInput(isOffline: true) }

    /// `URLError` codes raised before any byte reaches the server.
    public static let neverSentURLErrorCodes: Set<URLError.Code> = [
        .notConnectedToInternet, .cannotFindHost, .cannotConnectToHost, .dnsLookupFailed,
        .dataNotAllowed, .internationalRoamingOff,
    ]
}

/// Failure copy for the money flows.
///
/// The backend already writes some 4xx bodies as member-facing sentences ("Cash out at
/// least $0.10.", "The pot could not raise enough USDC…"); those are passed straight
/// through. The rest are internal strings ("amount exceeds available platform balance"),
/// so they are translated here instead of being shown raw or swallowed by a generic
/// "Try again" that tells the member nothing.
public enum MoneyFlowCopy {
    // MARK: - Cash out to an external wallet (POST /v1/me/withdrawals)

    public static func cashOutFailure(_ input: FlowErrorInput) -> FlowFailure {
        if input.isOffline { return offlineFailure(action: "cash out") }
        switch input.status {
        case 400 where matches(input, "amount exceeds available platform balance"):
            return FlowFailure(
                message: "That's more than your account balance.",
                isRetryable: false,
                nextStep: "Cash out of a cabal to your balance first, or send a smaller amount."
            )
        case 400 where matches(input, "invalid destination address"):
            return FlowFailure(
                message: "That destination isn't a Base address.",
                isRetryable: false,
                nextStep: "Paste the address again — it should be 32 to 44 characters."
            )
        case 400 where matches(input, "cannot withdraw to your deposit address"):
            return FlowFailure(
                message: "That's your own Monaco deposit address.",
                isRetryable: false,
                nextStep: "Paste the outside wallet you want the USDC sent to."
            )
        case 409:
            return FlowFailure(
                message: "A cash out is already on its way.",
                isRetryable: false,
                nextStep: "Wait for it to land — about a minute — then start another."
            )
        default:
            return generic(input, action: "cash out")
        }
    }

    // MARK: - Fund a cabal (POST /v1/groups/{id}/fund)

    public static func fundCabalFailure(_ input: FlowErrorInput) -> FlowFailure {
        if input.isOffline { return offlineFailure(action: "add that money") }
        switch input.status {
        case 400 where matches(input, "amount exceeds available platform balance"):
            return FlowFailure(
                message: "That's more than your account balance.",
                isRetryable: false,
                nextStep: "Add USDC to your balance, or use the Max button."
            )
        case 403:
            return FlowFailure(
                message: "You have to be a member of this cabal to add money to it.",
                isRetryable: false,
                nextStep: "Join the cabal first."
            )
        case 404:
            return FlowFailure(message: "That cabal no longer exists.", isRetryable: false)
        default:
            return generic(input, action: "add that money")
        }
    }

    // MARK: - Cash out of a cabal to the account balance

    public static func sellStakeFailure(_ input: FlowErrorInput) -> FlowFailure {
        if input.isOffline { return offlineFailure(action: "cash out") }
        switch input.status {
        case 409:
            return FlowFailure(
                message: "Your last cash out is still finishing.",
                isRetryable: true,
                nextStep: "Give it a minute, then try again."
            )
        case 404:
            return FlowFailure(message: "You're not a member of this cabal.", isRetryable: false)
        // The only bodiless 400 this endpoint sends is the dust floor.
        case 400 where memberFacingMessage(input.serverMessage) == nil:
            return FlowFailure(message: "Cash out at least $0.10.", isRetryable: false)
        default:
            return generic(input, action: "cash out")
        }
    }

    // MARK: - Shared shapes

    public static func offlineFailure(action: String) -> FlowFailure {
        FlowFailure(
            message: "No connection, so we didn't \(action).",
            isRetryable: true,
            nextStep: "Check your internet and try again — nothing was sent."
        )
    }

    /// Server 4xx copy is shown only when it was written for members. Internal strings
    /// are lowercase fragments ("not a group member"); member copy is a sentence.
    public static func memberFacingMessage(_ raw: String?) -> String? {
        guard let raw, !raw.isEmpty else { return nil }
        guard let first = raw.first, first.isUppercase else { return nil }
        guard raw.hasSuffix(".") || raw.hasSuffix("!") else { return nil }
        return raw
    }

    private static func generic(_ input: FlowErrorInput, action: String) -> FlowFailure {
        if input.status == 429 {
            return FlowFailure(
                message: "Too many tries in a row.",
                isRetryable: true,
                nextStep: "Wait a moment and try again."
            )
        }
        if input.status == 401 {
            return FlowFailure(
                message: "Your session expired.",
                isRetryable: false,
                nextStep: "Sign in again to \(action)."
            )
        }
        if let status = input.status, (400..<500).contains(status),
           let message = memberFacingMessage(input.serverMessage) {
            return FlowFailure(message: message, isRetryable: false)
        }
        guard input.status != nil else { return unconfirmed }
        return FlowFailure(
            message: "We couldn't \(action).",
            isRetryable: true,
            nextStep: "Try again in a moment."
        )
    }

    /// No status at all (timeout, dropped connection, unreadable reply): the request may
    /// have gone through, so a blind retry could move the money twice.
    public static let unconfirmed = FlowFailure(
        message: "We couldn't confirm that went through.",
        isRetryable: false,
        nextStep: "Check your balance before trying again."
    )

    private static func matches(_ input: FlowErrorInput, _ expected: String) -> Bool {
        input.serverMessage?.lowercased() == expected
    }
}
