import Foundation

/// Header that carries the correlation id to the API, which logs it and echoes it back.
public let monacoRequestIDHeader = "X-Request-Id"

/// Why a request never produced an HTTP status.
public enum APITransportErrorCategory: String, Equatable, Sendable {
    case offline
    case timeout
    case cancelled
    case tls
    case other

    public init(_ error: Error) {
        if error is CancellationError {
            self = .cancelled
            return
        }
        let nsError = error as NSError
        guard nsError.domain == NSURLErrorDomain else {
            self = .other
            return
        }
        switch URLError.Code(rawValue: nsError.code) {
        case .cancelled:
            self = .cancelled
        case .timedOut:
            self = .timeout
        case .notConnectedToInternet, .networkConnectionLost, .dataNotAllowed, .internationalRoamingOff:
            self = .offline
        case .secureConnectionFailed, .serverCertificateHasBadDate, .serverCertificateUntrusted,
             .serverCertificateHasUnknownRoot, .serverCertificateNotYetValid,
             .clientCertificateRejected, .clientCertificateRequired:
            self = .tls
        default:
            self = .other
        }
    }
}

public enum APIRequestOutcome: Equatable, Sendable {
    /// The server answered. Decoding the body happens later and is not part of the outcome.
    case status(Int)
    case transportError(APITransportErrorCategory)
}

/// One finished API request. Holds only what is safe to log: no tokens, bodies,
/// query strings or raw path ids.
public struct APIRequestEvent: Equatable, Sendable {
    public let method: String
    /// Route template such as `/v1/groups/{id}/fund`.
    public let route: String
    public let outcome: APIRequestOutcome
    public let durationMs: Double
    /// Id minted by the client and sent as `X-Request-Id`.
    public let requestID: String
    /// Id the server echoed back. Differs from `requestID` only if the server replaced it;
    /// `nil` when the response never reached the API (offline, proxy error page).
    public let serverRequestID: String?

    public init(
        method: String,
        route: String,
        outcome: APIRequestOutcome,
        durationMs: Double,
        requestID: String,
        serverRequestID: String?
    ) {
        self.method = method
        self.route = route
        self.outcome = outcome
        self.durationMs = durationMs
        self.requestID = requestID
        self.serverRequestID = serverRequestID
    }

    public var statusCode: Int? {
        if case .status(let code) = outcome { return code }
        return nil
    }

    /// 4xx, 5xx and transport errors. A cancelled request is the app changing its mind, not a failure.
    public var isFailure: Bool {
        switch outcome {
        case .status(let code): return code >= 400
        case .transportError(let category): return category != .cancelled
        }
    }
}

/// Receives one event per request sent through `MonacoHTTPTransport`. A token refresh
/// and its retry count as the same request. Called on the requesting task, so keep it cheap.
public protocol APITelemetry: Sendable {
    func record(_ event: APIRequestEvent)
}

/// Where the app registers its telemetry sink. API clients are created ad hoc across the
/// app, so the transport looks the sink up here per request. Nothing registered means
/// nothing is recorded.
public final class APITelemetryRegistry: @unchecked Sendable {
    public static let shared = APITelemetryRegistry()

    private let lock = NSLock()
    private var telemetry: APITelemetry?

    public init() {}

    public func register(_ telemetry: APITelemetry?) {
        lock.lock()
        defer { lock.unlock() }
        self.telemetry = telemetry
    }

    public var current: APITelemetry? {
        lock.lock()
        defer { lock.unlock() }
        return telemetry
    }
}

/// Turns a request path into a route template when the caller did not supply one.
///
/// Works as an allowlist: a segment is kept only when it looks like a route literal
/// (`v1`, `groups`, `withdraw-to-balance`). Everything else, including UUIDs, numeric ids,
/// wallet addresses and asset symbols, becomes `{id}`. Query strings are never included.
public enum APIRouteTemplate {
    public static let placeholder = "{id}"

    public static func redacting(path: String) -> String {
        let segments = path.split(separator: "/", omittingEmptySubsequences: true)
        guard !segments.isEmpty else { return "/" }
        return "/" + segments.map { isRouteLiteral($0) ? String($0) : placeholder }.joined(separator: "/")
    }

    public static func redacting(url: URL?) -> String {
        redacting(path: url?.path ?? "")
    }

    private static let maxLiteralLength = 32

    private static func isRouteLiteral(_ segment: Substring) -> Bool {
        guard segment.count <= maxLiteralLength, let first = segment.first else { return false }
        if first == "v", segment.count > 1, segment.dropFirst().allSatisfy(\.isASCIIDigit) {
            return true
        }
        return first.isASCIILowercaseLetter
            && segment.allSatisfy { $0.isASCIILowercaseLetter || $0 == "-" }
    }
}

private extension Character {
    var isASCIIDigit: Bool { isASCII && isNumber }
    var isASCIILowercaseLetter: Bool { isASCII && isLowercase && isLetter }
}

// MARK: Request id on errors

/// `userInfo` key under which the transport stamps the request id on a rethrown `URLError`.
public let monacoRequestIDErrorKey = "MonacoRequestID"

/// `userInfo` flag the transport sets when the failure came from refreshing the access
/// token, not from the request itself: the request never left the device.
public let monacoTokenRefreshFailedErrorKey = "MonacoTokenRefreshFailed"

public extension Error {
    /// True when this failure is a token refresh that did not come back, so whatever the
    /// caller was trying to do provably did not run.
    var isTokenRefreshFailure: Bool {
        (self as NSError).userInfo[monacoTokenRefreshFailedErrorKey] as? Bool == true
    }

    /// Request id of the API call that produced this error, when known.
    var apiRequestID: String? {
        if let apiError = self as? MonacoAPIError {
            return apiError.requestID
        }
        return (self as NSError).userInfo[monacoRequestIDErrorKey] as? String
    }

    /// Short code a user can read out to support, e.g. `ref: 3f9a1c20`. It is the head of
    /// the request id, which the API logs in full.
    var apiSupportReference: String? {
        guard let id = apiRequestID, !id.isEmpty else { return nil }
        return "ref: \(id.prefix(8))"
    }
}
