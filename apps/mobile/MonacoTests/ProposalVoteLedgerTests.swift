import MonacoCore
import Testing
@testable import Monaco

@MainActor
struct ProposalVoteLedgerTests {
    private func proposal(id: String = "p-1", votes: [ProposalVoteDTO]? = nil) -> ProposalDTO {
        ProposalDTO(id: id, symbol: "AAPLc", status: "open", kind: "buy", usdcMicros: "25000000", canVote: false, votes: votes)
    }

    @Test func aProposalTheMemberHasNotVotedOnHasNoChoice() {
        let ledger = ProposalVoteLedger()
        #expect(ledger.choice(for: proposal(), viewerId: "me") == nil)
    }

    @Test func aBallotCastOnOneScreenIsKnownOnEveryOther() {
        // Feed rows carry no ballots, so the card only knows the member voted because the ledger does.
        let ledger = ProposalVoteLedger()
        ledger.record(.yes, for: "p-1", viewerId: "me")
        #expect(ledger.choice(for: proposal(), viewerId: "me") == "yes")
        #expect(ledger.choice(for: proposal(id: "p-2"), viewerId: "me") == nil)
    }

    @Test func noIsRememberedAsNo() {
        let ledger = ProposalVoteLedger()
        ledger.record(.no, for: "p-1", viewerId: "me")
        #expect(ledger.choice(for: proposal(), viewerId: "me") == "no")
    }

    @Test func theServersBallotWins() {
        // Detail payloads carry the real ballots; the ledger never overrides them.
        let ledger = ProposalVoteLedger()
        ledger.record(.no, for: "p-1", viewerId: "me")
        let voted = proposal(votes: [ProposalVoteDTO(voterId: "me", displayName: "You", choice: "Yes")])
        #expect(ledger.choice(for: voted, viewerId: "me") == "yes")
    }

    @Test func anotherMembersBallotIsNotTheViewers() {
        let ledger = ProposalVoteLedger()
        let voted = proposal(votes: [ProposalVoteDTO(voterId: "ben", displayName: "Ben", choice: "yes")])
        #expect(ledger.choice(for: voted, viewerId: "me") == nil)
    }

    @Test func aBallotDoesNotOutliveTheMemberWhoCastIt() {
        // The store lasts as long as the process: member A votes and signs out, member B signs in
        // on the same device and is eligible to vote on the same proposal. Feed and cabal rows
        // carry no ballots, so this is the only thing standing between B and "You voted yes" on a
        // proposal they never voted on — with no buttons left to vote with.
        let ledger = ProposalVoteLedger()
        ledger.record(.yes, for: "p-1", viewerId: "a")
        #expect(ledger.choice(for: proposal(), viewerId: "b") == nil)
        #expect(ledger.choice(for: proposal(), viewerId: "a") == "yes")
    }

    @Test func aBallotWithNoKnownViewerIsNotKept() {
        // Between sign-out and the next backend session the viewer id is unknown. A ballot filed
        // under nobody would be read back by whoever is signed in next.
        let ledger = ProposalVoteLedger()
        ledger.record(.yes, for: "p-1", viewerId: nil)
        #expect(ledger.choice(for: proposal(), viewerId: nil) == nil)
        #expect(ledger.choice(for: proposal(), viewerId: "me") == nil)
    }

    @Test func clearingForgetsEveryBallot() {
        let ledger = ProposalVoteLedger()
        ledger.record(.yes, for: "p-1", viewerId: "a")
        ledger.record(.no, for: "p-2", viewerId: "b")
        ledger.clear()
        #expect(ledger.choice(for: proposal(), viewerId: "a") == nil)
        #expect(ledger.choice(for: proposal(id: "p-2"), viewerId: "b") == nil)
    }
}
