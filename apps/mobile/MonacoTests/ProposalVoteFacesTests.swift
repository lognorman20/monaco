import MonacoCore
import SwiftUI
import Testing
@testable import Monaco

/// The tally's marks, the closing countdown and the status wash — the three pure decisions the
/// v3 proposal card makes, pinned here so they can be reasoned about without a screen.
struct ProposalVoteFacesTests {
    private func proposal(
        status: String = "open",
        votes: [ProposalVoteDTO]? = nil,
        yes: Int = 0,
        no: Int = 0,
        eligible: Int = 5
    ) -> ProposalDTO {
        ProposalDTO(
            id: "p1",
            symbol: "AAPLc",
            status: status,
            kind: "buy",
            canVote: true,
            votes: votes,
            voteSummary: ProposalVoteSummaryDTO(
                yesCount: yes,
                noCount: no,
                eligibleCount: eligible,
                threshold: "majority"
            )
        )
    }

    private func progress(_ proposal: ProposalDTO) -> ProposalVoteProgress {
        ProposalVoteProgress(summary: proposal.voteSummary!)
    }

    // MARK: The marks

    @Test func anOpenVoteShowsOneMarkPerEligibleMember() {
        let open = proposal(yes: 1, no: 1)
        let votes = ProposalVoteFaces.votes(for: open, progress: progress(open))
        #expect(votes.count == 5)
        #expect(votes.filter { $0.state == .yes }.count == 1)
        #expect(votes.filter { $0.state == .no }.count == 1)
        #expect(votes.filter { $0.state == .pending }.count == 3)
    }

    @Test func aBallotOnAListRowIsCountedButNotNamed() {
        // List payloads carry the summary and no `votes`, so a ballot is a ring with nobody in it.
        // It still says how it was cast, which is the part that must not be dropped.
        let open = proposal(yes: 2)
        let votes = ProposalVoteFaces.votes(for: open, progress: progress(open))
        #expect(votes.filter { $0.state == .yes }.allSatisfy { $0.face == nil })
    }

    @Test func aBallotOnTheDetailPayloadCarriesItsVoter() {
        let open = proposal(
            votes: [ProposalVoteDTO(voterId: "u1", displayName: "Ada Park", choice: "yes")],
            yes: 1
        )
        let votes = ProposalVoteFaces.votes(for: open, progress: progress(open))
        #expect(votes.first?.face?.displayName == "Ada Park")
        #expect(votes.first?.state == .yes)
    }

    @Test func aClosedVoteHasNoEmptySlots() {
        // Nobody owes a ballot on a settled proposal, so a dashed slot would be a lie about who
        // still has to act.
        let closed = proposal(status: "passed", yes: 1)
        let votes = ProposalVoteFaces.votes(for: closed, progress: progress(closed))
        #expect(votes.count == 1)
        #expect(!votes.contains { $0.state == .pending })
    }

    @Test func aClosedVoteWithNamedBallotsAlsoHasNoEmptySlots() {
        let closed = proposal(
            status: "passed",
            votes: [
                ProposalVoteDTO(voterId: "u1", displayName: "Ada Park", choice: "yes"),
                ProposalVoteDTO(voterId: "u2", displayName: "Ben Ortiz", choice: "no"),
            ],
            yes: 1,
            no: 1
        )
        let votes = ProposalVoteFaces.votes(for: closed, progress: progress(closed))
        #expect(votes.count == 2)
        #expect(!votes.contains { $0.state == .pending })
    }

    @Test func aCrowdFallsBackToTheCaptionAlone() {
        // Past the dot cap the marks stop being a count of people and become a texture.
        let big = proposal(yes: 3, eligible: ProposalVoteProgress.maxDots + 1)
        #expect(ProposalVoteFaces.votes(for: big, progress: progress(big)).isEmpty)
    }

    @Test func facesGiveWayToDotsPastEightVoters() {
        #expect(MonacoVoteTallyLayout.usesFaces(voterCount: 8, dynamicTypeSize: .large))
        #expect(!MonacoVoteTallyLayout.usesFaces(voterCount: 9, dynamicTypeSize: .large))
        #expect(!MonacoVoteTallyLayout.usesFaces(voterCount: 4, dynamicTypeSize: .accessibility1))
    }

    // MARK: The countdown

    @Test func theCountdownIsMinutesAndSeconds() {
        #expect(ProposalCountdown.clock(2_527) == "42:07")
        #expect(ProposalCountdown.clock(59) == "0:59")
    }

    @Test func anExpiredVoteSaysSoRatherThanCountingBelowZero() {
        #expect(ProposalCountdown.clock(0) == ProposalCountdown.closedLabel)
    }

    @Test func voiceOverHearsMinutesNotASirenOfSeconds() {
        #expect(ProposalCountdown.spoken(2_527) == "Closes in 42 minutes")
        #expect(ProposalCountdown.spoken(90) == "Closes in 1 minute")
        #expect(ProposalCountdown.spoken(20) == "Closes in under a minute")
    }

    // MARK: The status wash

    @Test func aPassedProposalIsInkAndNeverGreen() {
        // Green means profit in this product and nothing else. A cabal agreeing to spend money is
        // a decision made, not money made.
        #expect(ProposalStatusIntent.of(status: "passed", stage: nil) == .settled)
        #expect(ProposalStatusIntent.of(status: "passed", stage: .done) == .settled)
    }

    @Test func aSwapInFlightIsAmberAndAFailedOneIsRed() {
        #expect(ProposalStatusIntent.of(status: "passed", stage: .executing) == .running)
        #expect(ProposalStatusIntent.of(status: "passed", stage: .failed) == .failed)
    }

    @Test func aVoteThatDidNotPassIsQuiet() {
        #expect(ProposalStatusIntent.of(status: "failed", stage: nil) == .quiet)
        #expect(ProposalStatusIntent.of(status: "expired", stage: nil) == .quiet)
    }
}
