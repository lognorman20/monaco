import Foundation

/// How long one Monaco request may take before the app gives up on it.
///
/// `URLSession.shared` gives every request the same 60s, which is wrong at both ends: a
/// poll that hangs for a minute blocks a screen's refresh for a minute, and a money POST
/// that the backend is still confirming is cut off with no answer. The budgets below are
/// picked per kind of request and are the only place the numbers live.
public enum MonacoRequestTimeout {
    /// Reads, polls and light writes (vote, chat, comment). Short on purpose: the feed
    /// refreshes every 5-15s, so a request that is still open after this is already stale.
    public static let standard: TimeInterval = 15

    /// Writes that carry an `Idempotency-Key`: fund, cash out, withdraw to balance, leave
    /// with stake. The backend confirms these on chain inside the request, so they need
    /// room. A retry after this deadline goes out under the same key, and a backend still
    /// holding that key replays the first answer instead of moving the money again.
    ///
    /// The rule keys off the header, but the dedupe it assumes is per route: only the
    /// suffixes in `idempotentPathSuffixes` (httpapi/idempotency.go) run the middleware.
    /// `postRedeem` sends a key to `/redeems`, which is not on that list, so its key is
    /// inert and the 60s it earns here buys no replay protection. Harmless while the
    /// endpoint has no callers; adding one means adding the route server-side first.
    ///
    /// 60s is the same number `URLSession.shared` already gave these routes, so by itself
    /// this constant is not a behaviour change for money: a confirm that takes 61-180s still
    /// surfaces as a failure exactly as it did before. It is deliberately not raised to the
    /// backend's 3-minute `serverWriteTimeout` — three minutes of a spinner is not a better
    /// answer than a failure the member can act on. What changed for money is the recovery
    /// classification: that failure is now `.resendSame`, so the retry rides the pending key
    /// instead of minting a new one. The rest is server-side: the routes that swap inside the
    /// request should answer 202 with a job to poll, as withdraw-to-balance already does, and
    /// then this budget and `serverWriteTimeout` can meet in the middle.
    public static let moneyWrite: TimeInterval = 60

    /// Uploads (profile photo): megabytes on a phone network.
    public static let upload: TimeInterval = 60

    /// Opening a session (`POST /v1/auth/session`). The app's cold-start gate: the handler
    /// verifies with Privy and provisions a wallet on first sign-in, so it is slow and it
    /// is not a read. Budgets are enumerated by hand, which means every route that does not
    /// name one inherits `standard` — a slow non-idempotent route has to name its own.
    public static let signIn: TimeInterval = 60

    /// Ceiling for one URLSession task, set on the session. Deliberately NOT a ceiling for
    /// one logical request: a 401 refresh-and-retry runs two tasks (`sendRefreshingToken` in
    /// `MonacoHTTPTransport`), each getting its own 180s, with the auth provider's `refresh`
    /// between them on no deadline at all. A money POST that refreshes can therefore run for
    /// `moneyWrite` + an unbounded refresh + `moneyWrite`. Bounding the whole sequence needs
    /// a deadline around `send`, not a larger number here.
    static let resource: TimeInterval = 180

    /// The longest budget any single request may ask for. The session is configured with
    /// this rather than with `standard`, because `URLSession` does not promise that a
    /// request's own `timeoutInterval` outranks the session's `timeoutIntervalForRequest` —
    /// on Darwin the effective deadline is commonly the session's, or the stricter of the
    /// two. Configuring the session with the longest budget makes the per-request stamp
    /// able only to *shorten* a request: reads still get `standard` under either rule, and
    /// a money write can never be cut below `moneyWrite` by the session default.
    static var sessionCeiling: TimeInterval { max(moneyWrite, upload) }

    /// The budget for `request`: `override` when the caller named one, otherwise the money
    /// budget for a request carrying an idempotency key and `standard` for everything else.
    static func seconds(for request: URLRequest, override: TimeInterval?) -> TimeInterval {
        if let override { return override }
        let isIdempotentWrite = request.value(forHTTPHeaderField: IdempotentSubmission.keyHeader) != nil
        return isIdempotentWrite ? moneyWrite : standard
    }
}

extension MonacoRequestTimeout {
    /// The configuration behind `URLSession.monaco`, built here so a test can exercise the
    /// real timeout values against a stub protocol.
    static func sessionConfiguration() -> URLSessionConfiguration {
        let configuration = URLSessionConfiguration.default
        configuration.timeoutIntervalForRequest = sessionCeiling
        configuration.timeoutIntervalForResource = resource
        // A money POST must fail fast and be retried under its key, not sit queued until
        // the network comes back and land long after the member gave up on the screen.
        configuration.waitsForConnectivity = false
        return configuration
    }
}

extension URLSession {
    /// The session every Monaco request goes through: own configuration, explicit timeouts,
    /// no shared cookie or cache state with anything else in the process.
    public static let monaco = URLSession(configuration: MonacoRequestTimeout.sessionConfiguration())
}
