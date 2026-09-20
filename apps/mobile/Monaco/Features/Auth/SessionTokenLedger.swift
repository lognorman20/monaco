import Foundation

/// The access tokens the open sign-in has used.
///
/// A 401 naming a token that is not in here is answering a request made by a session that
/// has already ended — a sign-out, or another account signing in. Such a reply must neither
/// mint a fresh token (that would run one member's request under another member's session)
/// nor sign out whoever is signed in now.
///
/// Only the current token and the one it replaced are kept. Privy rotates roughly hourly,
/// and the only tokens an in-flight reply can legitimately name are the one it was sent with
/// and the one that just superseded it. Holding every token a multi-day session ever used
/// would keep dozens of live credentials in the heap for no added protection.
///
/// A plain value type, so the guard can be tested without standing up Privy.
struct SessionTokenLedger: Equatable {
    private var current: String?
    private var previous: String?

    var isEmpty: Bool { current == nil && previous == nil }

    /// True when `token` belongs to the sign-in that is still open.
    func contains(_ token: String) -> Bool {
        token == current || token == previous
    }

    /// Records the token the session is now using. Re-adopting the current token is a no-op,
    /// so a refresh that hands back what we already hold cannot push the previous one out.
    mutating func adopt(_ token: String) {
        guard token != current else { return }
        previous = current
        current = token
    }

    mutating func clear() {
        current = nil
        previous = nil
    }
}
