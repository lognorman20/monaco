import Foundation

/// What the app shows once sign-in has succeeded.
public enum FirstRunDestination: Equatable, Sendable {
    /// The backend session has not opened yet: skeleton, or the session error.
    case session
    /// The account has no display name. Ask for one before the tabs.
    case nameSetup
    /// Straight into the tab shell.
    case app
}

/// The post-sign-in routing decision, kept pure so it can be tested without a simulator
/// and can never disagree with the screen it drives.
public enum FirstRunGate {
    /// A name that is empty once trimmed is the same as no name at all: every social
    /// surface (proposals, votes, leaderboards, chat) renders those accounts as "Member".
    ///
    /// Whitespace-only names cannot be saved — `DisplayNameRules.normalize` rejects them
    /// and so does the backend — so they only arrive from an older client or a direct
    /// database write. Treat them as missing rather than letting them through the gate.
    public static func needsDisplayName(_ displayName: String) -> Bool {
        displayName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    /// `nil` profile means the session is still opening, not that the name is missing.
    public static func destination(for me: MeDTO?) -> FirstRunDestination {
        guard let me else { return .session }
        return needsDisplayName(me.displayName) ? .nameSetup : .app
    }
}
