import Foundation

/// The `Idempotency-Key` for one money action the user confirmed.
///
/// The backend runs a money POST at most once per key, so the key has to mean "this
/// submission": it is minted when the request is first sent, reused for every retry while
/// the outcome is unknown (timeout, dropped connection, 5xx, the first attempt still
/// running), and dropped once the server gives a final answer. A changed payload is a new
/// submission and gets a new key.
///
/// Screens own one instance per money action (SwiftUI `@State`) and hand it to the API
/// client, which does the bookkeeping. API clients are created ad hoc, so the key cannot
/// live in them.
public final class IdempotentSubmission: @unchecked Sendable {
    public static let keyHeader = "Idempotency-Key"
    /// Set by the backend on responses it did not produce by running the request.
    public static let statusHeader = "Idempotency-Status"
    /// `statusHeader` value on the 409 sent while the first attempt is still running.
    public static let inProgressStatus = "in_progress"

    private let lock = NSLock()
    private let makeKey: @Sendable () -> String
    private var key: String?
    private var fingerprint: Data?

    /// - Parameter makeKey: key source, replaced in tests for deterministic keys.
    public init(makeKey: @escaping @Sendable () -> String = { UUID().uuidString.lowercased() }) {
        self.makeKey = makeKey
    }

    /// The key to send with `request`: the pending one when `request` repeats the pending
    /// submission (same method, URL and body), a fresh one otherwise.
    public func key(for request: URLRequest) -> String {
        let requestFingerprint = Self.fingerprint(of: request)
        lock.lock()
        defer { lock.unlock() }
        if let key, fingerprint == requestFingerprint {
            return key
        }
        let fresh = makeKey()
        key = fresh
        fingerprint = requestFingerprint
        return fresh
    }

    /// Records the server's answer to a request sent under `key`. Anything that is not a
    /// final answer keeps the key so the next attempt is recognised as a retry. A failed
    /// send (no response at all) needs no call: the key simply stays pending.
    public func record(response: URLResponse, forKey sentKey: String) {
        guard let http = response as? HTTPURLResponse, Self.isFinal(http) else { return }
        lock.lock()
        defer { lock.unlock() }
        guard key == sentKey else { return }
        key = nil
        fingerprint = nil
    }

    /// 2xx and 4xx are the request's result. 5xx is never stored by the backend, 401 and 429
    /// are answered before the key is claimed, and an in-progress 409 means the first attempt
    /// has not finished; all of those must be retried under the same key.
    static func isFinal(_ response: HTTPURLResponse) -> Bool {
        switch response.statusCode {
        case 401, 429:
            return false
        case 409:
            return response.value(forHTTPHeaderField: statusHeader) != inProgressStatus
        case 200..<500:
            return true
        default:
            return false
        }
    }

    private static func fingerprint(of request: URLRequest) -> Data {
        var data = Data((request.httpMethod ?? "").utf8)
        data.append(0)
        data.append(Data((request.url?.absoluteString ?? "").utf8))
        data.append(0)
        data.append(request.httpBody ?? Data())
        return data
    }
}

extension MonacoHTTPTransport {
    /// Sends a money POST under `submission`'s idempotency key.
    public func data(for request: URLRequest, submission: IdempotentSubmission) async throws -> (Data, URLResponse) {
        var request = request
        let key = submission.key(for: request)
        request.setValue(key, forHTTPHeaderField: IdempotentSubmission.keyHeader)
        let (data, response) = try await data(for: request)
        submission.record(response: response, forKey: key)
        return (data, response)
    }

    /// JSON bodies of money POSTs are encoded with sorted keys: a retry must produce the
    /// same bytes, or it would be taken for a new submission.
    public static func idempotentBodyEncoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return encoder
    }
}
