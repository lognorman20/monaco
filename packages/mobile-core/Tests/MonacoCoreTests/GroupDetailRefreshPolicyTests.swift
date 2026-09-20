import Foundation
import Testing
@testable import MonacoCore

@Suite("Cabal screen cadence")
struct GroupDetailCadenceTests {
    @Test("A quiet cabal only keeps its balances current")
    func restingByDefault() {
        #expect(GroupDetailCadence.interval(for: .init()) == LiveRefreshCadence.resting)
    }

    @Test("An open vote is watched closely")
    func openVotesAreInPlay() {
        #expect(GroupDetailCadence.interval(for: .init(hasOpenVotes: true)) == LiveRefreshCadence.inPlay)
    }

    @Test("A swap on its way is watched closely")
    func pendingSwapIsInPlay() {
        #expect(GroupDetailCadence.interval(for: .init(hasPendingSwap: true)) == LiveRefreshCadence.inPlay)
    }

    @Test("Money on its way into the pot is watched at the sweep cadence")
    func pendingDepositUsesSweepCadence() {
        let inputs = GroupDetailCadence.Inputs(hasOpenVotes: true, hasPendingDeposit: true)
        #expect(GroupDetailCadence.interval(for: inputs) == DepositPolling.sweepStatusInterval)
    }

    /// The bug: the deciding vote drops the proposal out of the open list, and the screen went
    /// quiet for a full resting interval at the moment the swap it caused was starting.
    @Test("The outcome of a vote that just closed is still watched closely")
    func votesThatJustClosedStayInPlay() {
        let justVoted = GroupDetailCadence.Inputs(
            hasOpenVotes: false,
            hasPendingSwap: false,
            isWatchingVoteOutcome: true
        )
        #expect(GroupDetailCadence.interval(for: justVoted) == LiveRefreshCadence.inPlay)
    }

    @Test("Once the settling window is over the screen goes back to resting")
    func settlingWindowEnds() {
        let settled = GroupDetailCadence.Inputs(isWatchingVoteOutcome: false)
        #expect(GroupDetailCadence.interval(for: settled) == LiveRefreshCadence.resting)
        #expect(GroupDetailCadence.voteSettlingWindow > LiveRefreshCadence.resting)
    }
}

@Suite("Join requests card")
struct GroupDetailJoinRequestsPolicyTests {
    /// The bug: any failure emptied the card, so a timeout blinked a pending request away from
    /// the admin who was looking at it.
    @Test("A transport failure leaves the card alone")
    func transportFailureKeepsTheCard() {
        #expect(GroupDetailRefreshPolicy.joinRequestsOutcome(failureStatus: nil, wasCancelled: false) == .keep)
    }

    @Test("A server error leaves the card alone")
    func serverErrorKeepsTheCard() {
        #expect(GroupDetailRefreshPolicy.joinRequestsOutcome(failureStatus: 500, wasCancelled: false) == .keep)
        #expect(GroupDetailRefreshPolicy.joinRequestsOutcome(failureStatus: 429, wasCancelled: false) == .keep)
    }

    @Test("A request dropped by navigation leaves the card alone")
    func cancellationKeepsTheCard() {
        #expect(GroupDetailRefreshPolicy.joinRequestsOutcome(failureStatus: nil, wasCancelled: true) == .keep)
        #expect(GroupDetailRefreshPolicy.joinRequestsOutcome(failureStatus: 403, wasCancelled: true) == .keep)
    }

    @Test("Only the server saying the card is not yours empties it")
    func notAnAdminClearsTheCard() {
        #expect(GroupDetailRefreshPolicy.joinRequestsOutcome(failureStatus: 403, wasCancelled: false) == .clear)
        #expect(GroupDetailRefreshPolicy.joinRequestsOutcome(failureStatus: 404, wasCancelled: false) == .clear)
    }

    @Test("A 403 stops the screen asking again")
    func forbiddenStopsAsking() {
        #expect(GroupDetailRefreshPolicy.viewerMayBeAdmin(afterFailureStatus: 403) == false)
    }

    @Test("Any other failure leaves the viewer's admin standing open")
    func otherFailuresKeepAsking() {
        #expect(GroupDetailRefreshPolicy.viewerMayBeAdmin(afterFailureStatus: nil))
        #expect(GroupDetailRefreshPolicy.viewerMayBeAdmin(afterFailureStatus: 500))
        #expect(GroupDetailRefreshPolicy.viewerMayBeAdmin(afterFailureStatus: 404))
    }

    /// The bug: a request answered on another device stayed on screen with live buttons that
    /// could only ever fail.
    @Test("A refused answer means the request is already gone")
    func alreadyAnswered() {
        #expect(GroupDetailRefreshPolicy.joinRequestAlreadyAnswered(failureStatus: 404))
        #expect(GroupDetailRefreshPolicy.joinRequestAlreadyAnswered(failureStatus: 409))
        #expect(GroupDetailRefreshPolicy.joinRequestAlreadyAnswered(failureStatus: 410))
    }

    @Test("A failure that could be retried keeps the row")
    func retryableFailuresKeepTheRow() {
        #expect(GroupDetailRefreshPolicy.joinRequestAlreadyAnswered(failureStatus: nil) == false)
        #expect(GroupDetailRefreshPolicy.joinRequestAlreadyAnswered(failureStatus: 500) == false)
        #expect(GroupDetailRefreshPolicy.joinRequestAlreadyAnswered(failureStatus: 503) == false)
    }
}
