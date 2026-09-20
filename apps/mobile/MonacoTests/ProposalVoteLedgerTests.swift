import MonacoCore
import Testing
@testable import Monaco

@MainActor
struct ProposalVoteLedgerTests {
    private func proposal(id: String = "p-1", votes: [ProposalVoteDTO]? = nil) -> ProposalDTO {
        ProposalDTO(id: id, symbol: "AAPLx", status: "open", kind: "buy", usdcMicros: "25000000", canVote: false, votes: votes)
    }

    @Test func aProposalTheMemberHasNotVotedOnHasNoChoice() {
        let ledger = ProposalVoteLedger()
        #expect(ledger.choice(for: proposal(), viewerId: "me") == nil)
    }

    @Test func aBallotCastOnOneScreenIsKnownOnEveryOther() {
        // Feed rows carry no ballots, so the card only knows the member voted because the ledger does.
        let ledger = ProposalVoteLedger()
        ledger.record(.yes, for: "p-1")
        #expect(ledger.choice(for: proposal(), viewerId: "me") == "yes")
        #expect(ledger.choice(for: proposal(id: "p-2"), viewerId: "me") == nil)
    }

    @Test func noIsRememberedAsNo() {
        let ledger = ProposalVoteLedger()
        ledger.record(.no, for: "p-1")
        #expect(ledger.choice(for: proposal(), viewerId: "me") == "no")
    }

    @Test func theServersBallotWins() {
        // Detail payloads carry the real ballots; the ledger never overrides them.
        let ledger = ProposalVoteLedger()
        ledger.record(.no, for: "p-1")
        let voted = proposal(votes: [ProposalVoteDTO(voterId: "me", displayName: "You", choice: "Yes")])
        #expect(ledger.choice(for: voted, viewerId: "me") == "yes")
    }

    @Test func anotherMembersBallotIsNotTheViewers() {
        let ledger = ProposalVoteLedger()
        let voted = proposal(votes: [ProposalVoteDTO(voterId: "ben", displayName: "Ben", choice: "yes")])
        #expect(ledger.choice(for: voted, viewerId: "me") == nil)
    }
}
