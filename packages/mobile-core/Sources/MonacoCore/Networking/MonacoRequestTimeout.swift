import Foundation

/// How long one Monaco request may take before the app gives up on it.
///
/// `URLSession.shared` gives every request the same 60s, which is wrong at both ends: a
/// poll that hangs for a minute blocks a screen's refresh for a minute, and a money POST
/// that the backend is still confirming on Base is cut off with no answer. The budgets
/// below are picked per kind of request and are the only place the numbers live.
///
/// A request gets `standard` unless its call site names another budget. Nothing is
/// inferred from the method or the headers: every route that is slow on purpose says so
/// where it is built, and a test pins each one (see `MonacoHTTPTransportTests`).
public enum MonacoRequestTimeout {
    /// Reads, polls and light writes (vote, chat, comment). Short on purpose: the feed
    /// refreshes every 5-15s, so a request that is still open after this is stale.
    public static let standard: TimeInterval = 15

    /// Requests whose handler prices a trade before it answers: a buy/sell quote
    /// (`POST /v1/groups/{id}/quotes`) and proposal create (`POST /v1/groups/{id}/proposals`).
    /// Both run the same `StartBuy` path on the backend, a KyberSwap route request plus a
    /// Base RPC treasury balance read, so both name this budget. Neither moves money,
    /// so a timeout here is a missing quote, never an unconfirmed transfer.
    public static let quote: TimeInterval = 30

    /// Writes that move money inside the request: fund a cabal, cash out to a wallet,
    /// cash out of a cabal, leave with a stake, retry a swap and the dev buy. The backend
    /// submits and waits for a receipt inside the handler, so they need room.
    ///
    /// 60s is the number `URLSession.shared` already gave these routes, so this constant is
    /// not a behaviour change for money: a confirm that takes longer still surfaces as a
    /// failure exactly as before, and that failure is worded as unconfirmed ("check your
    /// balance before trying again"). The API has no idempotency key (the restore series
    /// that starts at #413 brings one back), so a resend after this deadline is a second
    /// submission — the copy must never invite one.
    public static let moneyWrite: TimeInterval = 60

    /// Uploads (profile photo): megabytes on a phone network.
    public static let upload: TimeInterval = 60

    /// Writes whose handler provisions a Dynamic server wallet through the signer before it
    /// answers: opening a session (`POST /v1/auth/session` ensures the member wallet on
    /// first sign-in) and creating a cabal (`POST /v1/groups` ensures the treasury). An MPC
    /// key generation is slow and neither is a read, so both name this budget rather than
    /// inheriting `standard`.
    public static let walletProvisioning: TimeInterval = 60

    /// Ceiling for one URLSession task, set on the session. Deliberately NOT a ceiling for
    /// one logical request: a 401 refresh-and-retry runs two tasks
    /// (`MonacoHTTPTransport.data(for:timeout:)`), each getting its own budget, with the
    /// auth provider's refresh between them on no deadline at all. Bounding the whole
    /// sequence needs a deadline around `data(for:timeout:)`, not a larger number here.
    static let resource: TimeInterval = 180

    /// The longest budget any single request may ask for. The session is configured with
    /// this rather than with `standard`, because `URLSession` does not promise that a
    /// request's own `timeoutInterval` outranks the session's `timeoutIntervalForRequest` —
    /// on Darwin the effective deadline is commonly the session's, or the stricter of the
    /// two. Configuring the session with the longest budget makes the per-request stamp
    /// able only to *shorten* a request: reads still get `standard` under either rule, and
    /// a money write can never be cut below `moneyWrite` by the session default.
    static var sessionCeiling: TimeInterval { max(quote, moneyWrite, upload, walletProvisioning) }

    /// The budget for a request: `override` when the call site named one, `standard`
    /// otherwise.
    static func seconds(override: TimeInterval?) -> TimeInterval {
        override ?? standard
    }
}

extension MonacoRequestTimeout {
    /// The configuration behind `URLSession.monaco`, built here so a test can exercise the
    /// real timeout values against a stub protocol.
    static func sessionConfiguration() -> URLSessionConfiguration {
        let configuration = URLSessionConfiguration.default
        configuration.timeoutIntervalForRequest = sessionCeiling
        configuration.timeoutIntervalForResource = resource
        // A money POST must fail fast while the member is still on the screen, not sit
        // queued until the network comes back and land long after they gave up on it.
        configuration.waitsForConnectivity = false
        return configuration
    }
}

extension URLSession {
    /// The session every Monaco request goes through: own configuration, explicit timeouts,
    /// no shared cookie or cache state with anything else in the process.
    public static let monaco = URLSession(configuration: MonacoRequestTimeout.sessionConfiguration())
}
