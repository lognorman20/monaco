import Combine

/// Publishes complete refreshes and prevents stale requests from restoring old session data.
@MainActor
public final class RefreshableSnapshot<Value: Sendable>: ObservableObject {
    @Published public private(set) var value: Value?
    @Published public private(set) var revision = 0
    private var requestID = 0

    public init() {}

    public func refresh(load: @MainActor () async throws -> Value) async throws {
        requestID += 1
        let currentRequest = requestID
        do {
            let next = try await load()
            guard currentRequest == requestID, !Task.isCancelled else { return }
            value = next
            revision += 1
        } catch {
            guard currentRequest == requestID, !Task.isCancelled else { return }
            throw error
        }
    }

    public func clear() {
        requestID += 1
        value = nil
        revision += 1
    }
}
