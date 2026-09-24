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

/// What came back for one request, plus the id it was sent under.
public struct MonacoHTTPResponse: Sendable {
    public let data: Data
    public let response: URLResponse
    /// The `X-Request-Id` this request carried; stamp it on errors raised from the response.
    public let requestID: String

    public var statusCode: Int? {
        (response as? HTTPURLResponse)?.statusCode
    }
}

/// The one place Monaco requests hit the network.
///
/// Access tokens are short-lived (about an hour). When an authenticated request comes
/// back 401, the transport asks for a fresh token and retries the request once. A 401
/// that survives the retry is a real rejection and is returned to the caller as is.
///
/// Every request is sent with a fresh `X-Request-Id` and reported once to `APITelemetry`.
/// Every request also gets an explicit deadline (see `MonacoRequestTimeout`) rather than
/// the shared session's one-size-fits-all 60s.
public struct MonacoHTTPTransport: Sendable {
    private let session: URLSession
    private let refresher: AccessTokenRefresher?
    private let telemetry: APITelemetry?

    /// - Parameters:
    ///   - session: defaults to Monaco's own session, which declares its timeouts.
    ///   - refresher: defaults to whatever is registered in `AccessTokenRefreshRegistry.shared`.
    ///   - telemetry: defaults to whatever is registered in `APITelemetryRegistry.shared`.
    public init(
        session: URLSession = .monaco,
        refresher: AccessTokenRefresher? = nil,
        telemetry: APITelemetry? = nil
    ) {
        self.session = session
        self.refresher = refresher
        self.telemetry = telemetry
    }

    public func data(from url: URL) async throws -> (Data, URLResponse) {
        try await data(for: URLRequest(url: url))
    }

    public func data(for request: URLRequest, timeout: TimeInterval? = nil) async throws -> (Data, URLResponse) {
        let result = try await send(request, timeout: timeout)
        return (result.data, result.response)
    }

    /// - Parameters:
    ///   - route: route template for telemetry, e.g. `/v1/groups/{id}/fund`.
    ///     When `nil` the request path is redacted into one (see `APIRouteTemplate`).
    ///   - timeout: deadline for this request. Defaults to the budget for its kind
    ///     (`MonacoRequestTimeout`): the money budget when it carries an idempotency key,
    ///     the short one otherwise.
    public func send(
        _ request: URLRequest,
        route: String? = nil,
        timeout: TimeInterval? = nil
    ) async throws -> MonacoHTTPResponse {
        let requestID = UUID().uuidString.lowercased()
        var request = request
        request.setValue(requestID, forHTTPHeaderField: monacoRequestIDHeader)
        request.timeoutInterval = MonacoRequestTimeout.seconds(for: request, override: timeout)

        let clock = ContinuousClock()
        let started = clock.now
        let outcome: APIRequestOutcome
        var serverRequestID: String?
        defer {
            (telemetry ?? APITelemetryRegistry.shared.current)?.record(
                APIRequestEvent(
                    method: request.httpMethod ?? "GET",
                    route: route ?? APIRouteTemplate.redacting(url: request.url),
                    outcome: outcome,
                    durationMs: Self.milliseconds(clock.now - started),
                    requestID: requestID,
                    serverRequestID: serverRequestID
                )
            )
        }

        do {
            let (data, response) = try await sendRefreshingToken(request)
            if let http = response as? HTTPURLResponse {
                outcome = .status(http.statusCode)
                serverRequestID = http.value(forHTTPHeaderField: monacoRequestIDHeader)
            } else {
                outcome = .transportError(.other)
            }
            return MonacoHTTPResponse(data: data, response: response, requestID: requestID)
        } catch {
            outcome = .transportError(APITransportErrorCategory(error))
            throw Self.stamping(requestID, on: error)
        }
    }

    private func sendRefreshingToken(_ request: URLRequest) async throws -> (Data, URLResponse) {
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
        } catch {
            throw Self.tokenRefreshFailure(error)
        }
        guard let freshToken, !freshToken.isEmpty, freshToken != rejectedToken else {
            return (data, response)
        }

        var retry = request
        retry.setValue("Bearer \(freshToken)", forHTTPHeaderField: "Authorization")
        return try await session.data(for: retry)
    }

    /// A refresh that failed says something the request's own failure cannot: the request
    /// was never sent. It stays a `URLError` so the session screens keep reading it as a
    /// connection problem, and carries `monacoTokenRefreshFailedErrorKey` so the money
    /// flows can say "nothing was sent" instead of "we couldn't confirm that went through".
    private static func tokenRefreshFailure(_ error: Error) -> Error {
        let code = (error as? URLError)?.code ?? URLError.Code.userAuthenticationRequired
        var userInfo = (error as? URLError)?.userInfo ?? [:]
        userInfo[monacoTokenRefreshFailedErrorKey] = true
        userInfo[NSUnderlyingErrorKey] = error as NSError
        return URLError(code, userInfo: userInfo)
    }

    /// Connection failures stay `URLError`s so callers keep matching on `code`; the request
    /// id rides along in `userInfo` (see `Error.apiRequestID`).
    private static func stamping(_ requestID: String, on error: Error) -> Error {
        guard let urlError = error as? URLError else { return error }
        var userInfo = urlError.userInfo
        userInfo[monacoRequestIDErrorKey] = requestID
        return URLError(urlError.code, userInfo: userInfo)
    }

    private static func milliseconds(_ duration: Duration) -> Double {
        let (seconds, attoseconds) = duration.components
        return Double(seconds) * 1_000 + Double(attoseconds) / 1e15
    }

    static func bearerToken(in request: URLRequest) -> String? {
        guard let header = request.value(forHTTPHeaderField: "Authorization") else { return nil }
        let prefix = "Bearer "
        guard header.hasPrefix(prefix) else { return nil }
        let token = String(header.dropFirst(prefix.count)).trimmingCharacters(in: .whitespacesAndNewlines)
        return token.isEmpty ? nil : token
    }
}
