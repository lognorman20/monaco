import Foundation

/// What the screen should offer after a money request failed.
public enum FlowRecovery: Equatable, Sendable {
    /// Sending again is a fresh submission. Either the request provably never ran, or the
    /// backend has released its idempotency key and a retry will reach the handler again.
    case retry
    /// The outcome is unknown: the request may be running or may already have finished.
    /// Resending the *same* payload is the safest move available — it goes out under the
    /// pending idempotency key, so a backend that is still holding that key replays its
    /// answer or reports the attempt as in progress rather than moving the money again.
    /// Changing the amount or the address is worse: that mints a new key, which is a second
    /// submission.
    ///
    /// It is not an absolute guarantee of single execution, and copy must not promise one:
    /// the backend abandons an `in_progress` claim after 5 minutes (`idempotencyAbandonedAfter`
    /// in `httpapi/idempotency.go`), which is exactly the process-died case where the money
    /// state is genuinely unknown, and a payload derived from live data can change the
    /// fingerprint with no member action at all. Tell the member to check the balance.
    case resendSame
    /// The same request would fail the same way (bad address, not a member, signed out).
    case none
}

/// A failure a member can read and act on, plus what to offer them next.
public struct FlowFailure: Equatable, Sendable {
    public let message: String
    public let recovery: FlowRecovery
    /// One short line telling the member what to do next, when there is something to do.
    public let nextStep: String?

    /// False only when there is nothing worth sending again. True covers both a fresh
    /// submission and a replay, so it says whether to offer a way on — never whether a new
    /// idempotency key would be safe. Anything minting a key must read `recovery` itself.
    public var isRetryable: Bool { recovery != .none }

    /// True when the retry has to be the same payload, under the pending key.
    public var mustResendSameSubmission: Bool { recovery == .resendSame }

    /// The only initialiser. There is deliberately no `isRetryable:` convenience: on a money
    /// path "a fresh submission is safe" must never be something a call site gets by default,
    /// because the compiler cannot tell a considered `.retry` from an unconsidered one. Every
    /// failure states its own recovery, and a new branch does not compile until it does.
    public init(message: String, recovery: FlowRecovery, nextStep: String? = nil) {
        self.message = message
        self.recovery = recovery
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
    /// True when the app could not check the member's sign-in before sending (the token
    /// refresh failed, or there was no token). The request never ran, so nothing moved.
    public let isSignInUnavailable: Bool
    /// The server's `Retry-After` on a 429, when it sent one. Chat already tells the member
    /// how long to wait; the money flows said "a moment" for the same header.
    public let retryAfterSeconds: Int?

    public init(
        status: Int? = nil,
        serverMessage: String? = nil,
        isOffline: Bool = false,
        isSignInUnavailable: Bool = false,
        retryAfterSeconds: Int? = nil
    ) {
        self.status = status
        self.serverMessage = serverMessage?.trimmingCharacters(in: .whitespacesAndNewlines)
        self.isOffline = isOffline
        self.isSignInUnavailable = isSignInUnavailable
        self.retryAfterSeconds = retryAfterSeconds
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
        if input.isSignInUnavailable { return signInUnavailableFailure(action: "cash out") }
        if input.isOffline { return offlineFailure(action: "cash out") }
        switch input.status {
        case 400 where matches(input, "amount exceeds available platform balance"):
            return FlowFailure(
                message: "That's more than your account balance.",
                recovery: .none,
                nextStep: "Cash out of a cabal to your balance first, or send a smaller amount."
            )
        case 400 where matches(input, "invalid destination address"):
            return FlowFailure(
                message: "That destination isn't a Solana wallet address.",
                recovery: .none,
                nextStep: "Paste the address again — it should be 32 to 44 characters."
            )
        case 400 where matches(input, "cannot withdraw to your deposit address"):
            return FlowFailure(
                message: "That's your own Monaco deposit address.",
                recovery: .none,
                nextStep: "Paste the outside wallet you want the USDC sent to."
            )
        // Two different conditions answer 409 here: the handler's own "a platform withdrawal
        // is already in progress" (`platform_withdrawals.go`), and the idempotency middleware
        // when the key the app just sent is still claimed. Telling them apart needs the
        // `Idempotency-Status: in_progress` header, which the transport does not surface yet.
        // Until it does this stays `.none`, which fails closed: the screen offers no resend at
        // all, so neither condition can turn into a second withdrawal. The copy is true of
        // both, and both clear on their own.
        case 409:
            return FlowFailure(
                message: "A cash out is already on its way.",
                recovery: .none,
                nextStep: "Wait for it to land — about a minute — then start another."
            )
        default:
            return generic(input, action: "cash out")
        }
    }

    // MARK: - Fund a cabal (POST /v1/groups/{id}/fund)

    public static func fundCabalFailure(_ input: FlowErrorInput) -> FlowFailure {
        if input.isSignInUnavailable { return signInUnavailableFailure(action: "add that money") }
        if input.isOffline { return offlineFailure(action: "add that money") }
        switch input.status {
        // The deposit the member already sent is still running under its idempotency key.
        // Reported as a flat failure this reads as "nothing happened", while the money may
        // be landing; the retry has to go back under the same key.
        case 409:
            return FlowFailure(
                message: "Your last deposit is still finishing.",
                recovery: .resendSame,
                nextStep: "Give it a minute, then check the cabal's balance."
            )
        case 400 where matches(input, "amount exceeds available platform balance"):
            return FlowFailure(
                message: "That's more than your account balance.",
                recovery: .none,
                nextStep: "Add USDC to your balance, or use the Max button."
            )
        case 403:
            return FlowFailure(
                message: "You have to be a member of this cabal to add money to it.",
                recovery: .none,
                nextStep: "Join the cabal first."
            )
        case 404:
            return FlowFailure(message: "That cabal no longer exists.", recovery: .none)
        default:
            return generic(input, action: "add that money")
        }
    }

    // MARK: - Cash out of a cabal to the account balance

    public static func sellStakeFailure(_ input: FlowErrorInput) -> FlowFailure {
        if input.isSignInUnavailable { return signInUnavailableFailure(action: "cash out") }
        if input.isOffline { return offlineFailure(action: "cash out") }
        switch input.status {
        // Still running, under the key the app already sent: not a fresh submission.
        case 409:
            return FlowFailure(
                message: "Your last cash out is still finishing.",
                recovery: .resendSame,
                nextStep: "Give it a minute, then check your balance."
            )
        case 404:
            return FlowFailure(message: "You're not a member of this cabal.", recovery: .none)
        // The only bodiless 400 this endpoint sends is the dust floor.
        case 400 where memberFacingMessage(input.serverMessage) == nil:
            return FlowFailure(message: "Cash out at least $0.10.", recovery: .none)
        default:
            return generic(input, action: "cash out")
        }
    }

    // MARK: - Shared shapes

    /// The app never got as far as sending: it could not confirm the member is signed in.
    /// Nothing moved, so this is a plain retry, not an unknown outcome.
    public static func signInUnavailableFailure(action: String) -> FlowFailure {
        FlowFailure(
            message: "We couldn't check your sign-in, so we didn't \(action).",
            recovery: .retry,
            nextStep: "Try again in a moment — nothing was sent."
        )
    }

    public static func offlineFailure(action: String) -> FlowFailure {
        FlowFailure(
            message: "No connection, so we didn't \(action).",
            recovery: .retry,
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
        // Rejected by the rate limiter before the handler claimed a key, so nothing moved and
        // a fresh send is safe. The server usually says how long to wait; chat has always
        // shown that, and the money flows now do too instead of saying "a moment".
        if input.status == 429 {
            let wait: String
            if let seconds = input.retryAfterSeconds, seconds > 0 {
                wait = "Try again in \(seconds) second\(seconds == 1 ? "" : "s")."
            } else {
                wait = "Wait a moment and try again."
            }
            return FlowFailure(message: "Too many tries in a row.", recovery: .retry, nextStep: wait)
        }
        if input.status == 401 {
            return FlowFailure(
                message: "Your session expired.",
                recovery: .none,
                nextStep: "Sign in again to \(action)."
            )
        }
        // A 409 on a money route means the idempotency key the app just sent is still
        // claimed by an attempt that has not finished. That attempt may well be landing the
        // money, so this is never a fresh submission: the retry has to ride the same key.
        if input.status == 409 {
            return FlowFailure(
                message: "Your last request is still finishing.",
                recovery: .resendSame,
                nextStep: "Give it a minute, then check your balance."
            )
        }
        if let status = input.status, (400..<500).contains(status),
           let message = memberFacingMessage(input.serverMessage) {
            return FlowFailure(message: message, recovery: .none)
        }
        guard input.status != nil else { return unconfirmed }
        // A 5xx is not an unknown outcome held under the key: the backend deliberately
        // RELEASES the key on 5xx and panics (see `Idempotency.run` in httpapi), so the
        // retry reaches the handler again as a fresh run. Calling it `.resendSame` would
        // promise a replay that the server has already thrown away.
        return FlowFailure(
            message: "We couldn't \(action).",
            recovery: .retry,
            nextStep: "Try again in a moment."
        )
    }

    /// No status at all (timeout, dropped connection, unreadable reply): the request may
    /// have gone through. Resending the same amount is the safest way on — it carries the
    /// pending idempotency key, so a backend still holding that key answers with the first
    /// result instead of moving the money again. Changing the amount first is what would
    /// certainly move it twice.
    ///
    /// The next step deliberately stops short of promising single execution, and sends the
    /// member to their balance first: see `FlowRecovery.resendSame` for the cases where the
    /// key no longer protects the retry.
    public static let unconfirmed = FlowFailure(
        message: "We couldn't confirm that went through.",
        recovery: .resendSame,
        nextStep: "Check your balance first — if it didn't arrive, send the same amount again."
    )

    private static func matches(_ input: FlowErrorInput, _ expected: String) -> Bool {
        input.serverMessage?.lowercased() == expected
    }
}
