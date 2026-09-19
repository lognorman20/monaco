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

/// The one place Monaco requests hit the network.
///
/// Access tokens are short-lived (about an hour). When an authenticated request comes
/// back 401, the transport asks for a fresh token and retries the request once. A 401
/// that survives the retry is a real rejection and is returned to the caller as is.
public struct MonacoHTTPTransport: Sendable {
    private let session: URLSession
    private let refresher: AccessTokenRefresher?

    /// - Parameter refresher: defaults to whatever is registered in `AccessTokenRefreshRegistry.shared`.
    public init(session: URLSession = .shared, refresher: AccessTokenRefresher? = nil) {
        self.session = session
        self.refresher = refresher
    }

    public func data(from url: URL) async throws -> (Data, URLResponse) {
        try await session.data(from: url)
    }

    public func data(for request: URLRequest) async throws -> (Data, URLResponse) {
        let (data, response) = try await session.data(for: request)

        guard (response as? HTTPURLResponse)?.statusCode == 401,
              let rejectedToken = Self.bearerToken(in: request),
              let refresh = refresher ?? AccessTokenRefreshRegistry.shared.current
        else {
            return (data, response)
        }

        let freshToken = try await refresh(rejectedToken)?
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard let freshToken, !freshToken.isEmpty, freshToken != rejectedToken else {
            return (data, response)
        }

        var retry = request
        retry.setValue("Bearer \(freshToken)", forHTTPHeaderField: "Authorization")
        return try await session.data(for: retry)
    }

    static func bearerToken(in request: URLRequest) -> String? {
        guard let header = request.value(forHTTPHeaderField: "Authorization") else { return nil }
        let prefix = "Bearer "
        guard header.hasPrefix(prefix) else { return nil }
        let token = String(header.dropFirst(prefix.count)).trimmingCharacters(in: .whitespacesAndNewlines)
        return token.isEmpty ? nil : token
    }
}
