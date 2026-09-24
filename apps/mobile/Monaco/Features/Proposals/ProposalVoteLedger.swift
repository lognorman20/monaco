import MonacoCore
import SwiftUI

/// The ballots this member has cast, shared by every screen that draws a proposal card.
///
/// Feed rows and the cabal screen's preview rows carry no ballots, and the server stops offering
/// the buttons the moment a member has voted. Without somewhere shared to remember the ballot, a
/// card the member just voted on looks exactly like one they were never eligible to vote on, and
/// it looked different again on each of the three screens that drew it.
///
/// A ballot on the payload always wins: the detail screen carries `votes`, so once the server has
/// spoken this only fills the gap on rows that have no ballots. It is session state — the list API
/// does not yet return the viewer's choice, so a relaunch still forgets (see issue #291).
///
/// Every ballot is filed under the member who cast it. The shared store lives as long as the
/// process, longer than any one session, so a store keyed by proposal alone would hand the next
/// member to sign in on this device the previous member's ballots: their card would read "You
/// voted yes" with no buttons, on a proposal they are eligible to vote on and never voted on.
@MainActor
@Observable
final class ProposalVoteLedger {
    static let shared = ProposalVoteLedger()

    /// Viewer id → proposal id → "yes" / "no".
    private var choices: [String: [String: String]] = [:]

    init() {}

    func record(_ choice: ProposalVoteChoice, for proposalId: String, viewerId: String?) {
        // A ballot nobody owns cannot be shown to anybody: with no viewer to file it under, the
        // member sees the buttons again rather than a stranger's vote reported as their own.
        guard let viewerId else { return }
        choices[viewerId, default: [:]][proposalId] = choice.rawValue
    }

    /// "yes" / "no" when this member's ballot is known, from the payload or from this session.
    func choice(for proposal: ProposalDTO, viewerId: String?) -> String? {
        if let onPayload = proposal.viewerChoice(viewerId: viewerId) { return onPayload }
        guard let viewerId else { return nil }
        return choices[viewerId]?[proposal.id]
    }

    /// Forgets every ballot. Sign-out is the natural caller, so the store does not outlive the
    /// session at all; filing by viewer is what makes it safe until it has one.
    func clear() {
        choices.removeAll()
    }
}
