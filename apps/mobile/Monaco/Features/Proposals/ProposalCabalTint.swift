import SwiftUI

/// The one place a proposal surface turns a group id into a colour.
///
/// A cabal's tint is its identity, and identity that changes between two screens is not identity.
/// `MonacoTheme.CabalTint.forGroupId` is a plain hash: it answers on its own, it answers the same
/// way every time, and two of the viewer's cabals can still land on the same colour — which is
/// exactly the collision `CabalTintAssignment.resolve` exists to walk out of. A screen that calls
/// the hash directly therefore disagrees with every screen that went through the resolver, and the
/// disagreement is invisible until two cabals are on screen at once.
///
/// So the proposal screens ask here instead, and the answer is the resolver's — over the viewer's
/// own cabals, with the resolver's own fallback for a cabal they are not in.
///
/// The assignment is derived from `AppSessionStore.joinedCabals` rather than stored on it, and it
/// is derived rather than cached because `resolve` is a pure function of the id set: this answer
/// and the one the cabal list computes are the same answer, and there is no third copy to go
/// stale. When the session store starts publishing the resolved map (cross-chunk contract 4,
/// Chunk D), this reads it from there and no colour moves, because the input has not.
enum ProposalCabalTint {
    /// The resolved tint for a cabal, given the viewer's session.
    ///
    /// With no session — the debug harnesses, the previews, a card drawn before the store has
    /// answered — the assignment is empty and the resolver falls back to the hash, which is the
    /// colour the card drew before any of this.
    @MainActor
    static func tint(forGroupId groupId: String, in session: AppSessionStore?) -> MonacoTheme.CabalTint {
        tint(forGroupId: groupId, joinedGroupIds: session?.joinedCabals.map(\.groupId) ?? [])
    }

    /// The resolved tint for a cabal against an explicit cabal list. Pure, so it is testable and
    /// so the sample harnesses can reach it without a store.
    static func tint(forGroupId groupId: String, joinedGroupIds: [String]) -> MonacoTheme.CabalTint {
        CabalTintAssignment.tint(
            forGroupId: groupId,
            in: CabalTintAssignment.resolve(orderedGroupIds: joinedGroupIds)
        )
    }
}
