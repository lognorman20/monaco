import Foundation

/// Validates persisted Monaco backend session against a fresh open-session response.
public enum MonacoSessionValidation {
    /// Returns true when a previously stored user id no longer matches the server (e.g. local DB reset).
    public static func shouldInvalidateSession(storedUserId: String?, serverUserId: String) -> Bool {
        guard let storedUserId, !storedUserId.isEmpty else { return false }
        return storedUserId != serverUserId
    }
}
