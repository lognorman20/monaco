import Foundation

/// Persists Monaco backend session markers so Privy restore cannot bypass login after logout or DB reset.
struct MonacoSessionStore {
    private enum Keys {
        static let monacoUserId = "monaco.session.userId"
        static let hasExplicitLogin = "monaco.session.hasExplicitLogin"
    }

    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    var hasExplicitLogin: Bool {
        defaults.bool(forKey: Keys.hasExplicitLogin)
    }

    var storedUserId: String? {
        defaults.string(forKey: Keys.monacoUserId)
    }

    mutating func markExplicitLogin() {
        defaults.set(true, forKey: Keys.hasExplicitLogin)
    }

    mutating func recordSession(userId: String) {
        defaults.set(userId, forKey: Keys.monacoUserId)
    }

    mutating func clear() {
        defaults.removeObject(forKey: Keys.monacoUserId)
        defaults.removeObject(forKey: Keys.hasExplicitLogin)
    }

    func shouldInvalidateSession(serverUserId: String) -> Bool {
        guard let storedUserId, !storedUserId.isEmpty else { return false }
        return storedUserId != serverUserId
    }
}
