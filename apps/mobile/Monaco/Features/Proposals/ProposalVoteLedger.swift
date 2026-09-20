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
@MainActor
@Observable
final class ProposalVoteLedger {
    static let shared = ProposalVoteLedger()

    private var choices: [String: String] = [:]

    init() {}

    func record(_ choice: ProposalVoteChoice, for proposalId: String) {
        choices[proposalId] = choice.rawValue
    }

    /// "yes" / "no" when this member's ballot is known, from the payload or from this session.
    func choice(for proposal: ProposalDTO, viewerId: String?) -> String? {
        proposal.viewerChoice(viewerId: viewerId) ?? choices[proposal.id]
    }
}
