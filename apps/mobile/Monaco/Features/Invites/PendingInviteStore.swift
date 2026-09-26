import Foundation
import MonacoCore
import Observation

/// An invite link the app was opened with, held until the member can act on it.
///
/// A link can arrive before sign-in, or before a new account has a name. The code is kept
/// (in memory and in `UserDefaults`, so a relaunch during sign-in does not lose it) until the
/// signed-in tabs are on screen, which then present the join sheet and take it. A code older
/// than `lifetime` is dropped rather than surprising the member days later.
@MainActor
@Observable
final class PendingInviteStore {
    struct Pending: Equatable, Identifiable {
        /// The canonical code, or a legacy cabal id.
        let text: String
        let receivedAt: Date
        var id: String { text + "@\(receivedAt.timeIntervalSince1970)" }
    }

    static let shared = PendingInviteStore()
    static let lifetime: TimeInterval = 7 * 24 * 60 * 60
    private static let textKey = "monaco.pendingInvite.text"
    private static let dateKey = "monaco.pendingInvite.receivedAt"

    private(set) var pending: Pending?

    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private let now: () -> Date
    /// The last link received, so one tap delivered twice (as a URL and as a browsing
    /// activity) opens one sheet, not a second one after the first closes.
    @ObservationIgnored private var lastReceived: Pending?
    static let duplicateWindow: TimeInterval = 10

    init(defaults: UserDefaults = .standard, now: @escaping () -> Date = Date.init) {
        self.defaults = defaults
        self.now = now
        if let text = defaults.string(forKey: Self.textKey),
           let seconds = defaults.object(forKey: Self.dateKey) as? Double {
            pending = Pending(text: text, receivedAt: Date(timeIntervalSince1970: seconds))
        }
    }

    /// A URL the app was opened with. Returns false (and keeps nothing) for any URL that is
    /// not an invite, so other handlers can have it.
    @discardableResult
    func receive(_ url: URL) -> Bool {
        guard let link = InviteLink.parse(url: url) else { return false }
        let text: String
        switch link {
        case .code(let code): text = code
        case .groupId(let id): text = id
        }
        let arrived = Pending(text: text, receivedAt: now())
        if let lastReceived, lastReceived.text == text,
           arrived.receivedAt.timeIntervalSince(lastReceived.receivedAt) < Self.duplicateWindow {
            return true
        }
        lastReceived = arrived
        pending = arrived
        defaults.set(arrived.text, forKey: Self.textKey)
        defaults.set(arrived.receivedAt.timeIntervalSince1970, forKey: Self.dateKey)
        return true
    }

    /// Hands over the waiting invite, once. Nil when there is none or it has gone stale.
    func take() -> Pending? {
        guard let waiting = pending else { return nil }
        clear()
        guard now().timeIntervalSince(waiting.receivedAt) <= Self.lifetime else { return nil }
        return waiting
    }

    func clear() {
        pending = nil
        defaults.removeObject(forKey: Self.textKey)
        defaults.removeObject(forKey: Self.dateKey)
    }
}
