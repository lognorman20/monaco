import Foundation

/// Asks the auth layer for a fresh access token after the backend rejected `rejectedToken`.
/// - Returns: a token, or `nil` when the user is no longer signed in.
/// - Throws: when a token could not be fetched right now (offline, auth provider down).
///   The transport rethrows so callers see a connection failure, not a rejected session.
public typealias AccessTokenRefresher = @Sendable (_ rejectedToken: String) async throws -> String?

/// Where the signed-in auth service registers its token refresher. API clients are
/// created ad hoc across the app, so they look the refresher up here per request.
public final class AccessTokenRefreshRegistry: @unchecked Sendable {
    public static let shared = AccessTokenRefreshRegistry()

    private let lock = NSLock()
    private var refresher: AccessTokenRefresher?

    public init() {}

    public func register(_ refresher: AccessTokenRefresher?) {
        lock.lock()
        defer { lock.unlock() }
        self.refresher = refresher
    }

    public var current: AccessTokenRefresher? {
        lock.lock()
        defer { lock.unlock() }
        return refresher
    }
}

/// `userInfo` flag the transport sets when the failure came from refreshing the access
/// token, not from the request itself: the request had already been refused with a 401
/// and was never sent again.
public let monacoTokenRefreshFailedErrorKey = "MonacoTokenRefreshFailed"

public extension Error {
    /// True when this failure is a token refresh that did not come back, so whatever the
    /// caller was trying to do provably did not run.
    var isTokenRefreshFailure: Bool {
        (self as NSError).userInfo[monacoTokenRefreshFailedErrorKey] as? Bool == true
    }
}

/// The one place Monaco requests hit the network.
///
/// Access tokens are short-lived (about an hour). When an authenticated request comes
/// back 401, the transport asks for a fresh token and retries the request once. A 401
/// that survives the retry is a real rejection and is returned to the caller as is.
///
/// Every request also gets an explicit deadline (see `MonacoRequestTimeout`) rather than
/// the shared session's one-size-fits-all 60s.
public struct MonacoHTTPTransport: Sendable {
    private let session: URLSession
    private let refresher: AccessTokenRefresher?

    /// - Parameters:
    ///   - session: defaults to Monaco's own session, which declares its timeouts.
    ///   - refresher: defaults to whatever is registered in `AccessTokenRefreshRegistry.shared`.
    public init(session: URLSession = .monaco, refresher: AccessTokenRefresher? = nil) {
        self.session = session
        self.refresher = refresher
    }

    public func data(from url: URL, timeout: TimeInterval? = nil) async throws -> (Data, URLResponse) {
        try await data(for: URLRequest(url: url), timeout: timeout)
    }

    /// - Parameter timeout: deadline for this request. `nil` means `MonacoRequestTimeout.standard`;
    ///   a route that is slow on purpose (money, uploads, wallet provisioning) names its own.
    public func data(for request: URLRequest, timeout: TimeInterval? = nil) async throws -> (Data, URLResponse) {
        var request = request
        request.timeoutInterval = MonacoRequestTimeout.seconds(override: timeout)

        let (data, response) = try await session.data(for: request)

        guard (response as? HTTPURLResponse)?.statusCode == 401,
              let rejectedToken = Self.bearerToken(in: request),
              let refresh = refresher ?? AccessTokenRefreshRegistry.shared.current
        else {
            return (data, response)
        }

        let freshToken: String?
        do {
            freshToken = try await refresh(rejectedToken)?
                .trimmingCharacters(in: .whitespacesAndNewlines)
        } catch let cancellation as CancellationError {
            // The caller went away (a `.task(id:)` that restarted, a dismissed screen). That
            // is not a sign-in problem, and screens that ignore cancellation have to still
            // see it as cancellation.
            throw cancellation
        } catch {
            throw Self.tokenRefreshFailure(error)
        }
        guard let freshToken, !freshToken.isEmpty, freshToken != rejectedToken else {
            return (data, response)
        }

        // A copy of the request with one header swapped, so the retry keeps the body and
        // the deadline the call site chose.
        var retry = request
        retry.setValue("Bearer \(freshToken)", forHTTPHeaderField: "Authorization")
        return try await session.data(for: retry)
    }

    /// The server's `Retry-After` on a response, in whole seconds, when it sent a usable
    /// one. Both forms RFC 9110 allows are read: a delay in seconds (what the Monaco API
    /// sends) and an HTTP date. Nil for no header, a malformed one, or a date already past.
    ///
    /// Every client that turns a 429 into member copy reads the header through here, so a
    /// countdown in chat and a countdown on a money screen come from the same parse.
    public static func retryAfterSeconds(in response: URLResponse, now: Date = Date()) -> Int? {
        guard let http = response as? HTTPURLResponse,
              let raw = http.value(forHTTPHeaderField: "Retry-After")?
                .trimmingCharacters(in: .whitespacesAndNewlines),
              !raw.isEmpty
        else { return nil }
        if raw.allSatisfy({ $0.isASCII && $0.isNumber }) {
            return Int(raw)
        }
        guard let date = httpDateFormatter.date(from: raw) else { return nil }
        let seconds = date.timeIntervalSince(now).rounded(.up)
        guard seconds > 0, seconds < Double(Int32.max) else { return nil }
        return Int(seconds)
    }

    /// IMF-fixdate ("Sun, 06 Nov 1994 08:49:37 GMT"), the form RFC 9110 requires senders
    /// to use. Built once: it is only ever used to parse, which is thread-safe.
    private static let httpDateFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(identifier: "GMT")
        formatter.dateFormat = "EEE',' dd MMM yyyy HH':'mm':'ss 'GMT'"
        return formatter
    }()

    /// A refresh that failed says something the request's own failure cannot: the request
    /// was refused before it ran and was never sent again. It stays a `URLError` so the
    /// session screens keep reading it as a connection problem, and carries
    /// `monacoTokenRefreshFailedErrorKey` so the money flows can say "nothing was sent"
    /// instead of "we couldn't confirm that went through".
    private static func tokenRefreshFailure(_ error: Error) -> Error {
        let code = (error as? URLError)?.code ?? URLError.Code.userAuthenticationRequired
        var userInfo = (error as? URLError)?.userInfo ?? [:]
        userInfo[monacoTokenRefreshFailedErrorKey] = true
        userInfo[NSUnderlyingErrorKey] = error as NSError
        return URLError(code, userInfo: userInfo)
    }

    static func bearerToken(in request: URLRequest) -> String? {
        guard let header = request.value(forHTTPHeaderField: "Authorization") else { return nil }
        let prefix = "Bearer "
        guard header.hasPrefix(prefix) else { return nil }
        let token = String(header.dropFirst(prefix.count)).trimmingCharacters(in: .whitespacesAndNewlines)
        return token.isEmpty ? nil : token
    }
}
