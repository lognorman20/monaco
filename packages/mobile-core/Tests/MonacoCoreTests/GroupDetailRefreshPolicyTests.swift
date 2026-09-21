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

    @Test("A swap or deposit on its way is watched closely")
    func pendingActivityIsInPlay() {
        #expect(GroupDetailCadence.interval(for: .init(hasPendingActivity: true)) == LiveRefreshCadence.inPlay)
    }

    /// The bug: the deciding vote drops the proposal out of the open list, and the screen went
    /// quiet for a full resting interval at the moment the swap it caused was starting.
    @Test("The outcome of a vote that just closed is still watched closely")
    func votesThatJustClosedStayInPlay() {
        let justVoted = GroupDetailCadence.Inputs(
            hasOpenVotes: false,
            hasPendingActivity: false,
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

    @Test("Only a pending row is still going through")
    func pendingStatus() {
        #expect(GroupDetailCadence.isStillGoingThrough(status: "pending"))
        #expect(GroupDetailCadence.isStillGoingThrough(status: "Pending"))
        #expect(GroupDetailCadence.isStillGoingThrough(status: "confirmed") == false)
        #expect(GroupDetailCadence.isStillGoingThrough(status: "failed") == false)
        #expect(GroupDetailCadence.isStillGoingThrough(status: "failed: submit_sweep") == false)
        #expect(GroupDetailCadence.isStillGoingThrough(status: "") == false)
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

@Suite("Leave that did not come back as done")
struct GroupDetailLeaveFailureTests {
    private let unconfirmed = "We couldn't confirm that. Check your slice below before trying again"
    private let refused = "Couldn't leave this cabal. Try again"

    /// The bug: a lost response on "Sell and leave" said "Try again", but the server sells the
    /// slice before it answers, and with no idempotency key a second tap is a second request.
    @Test("A sell-and-leave that got no answer sends the member to their slice")
    func lostResponseIsUnconfirmed() {
        #expect(GroupDetailRefreshPolicy.leaveMayHaveSoldSlice(sellsSlice: true, failureStatus: nil))
        #expect(GroupDetailRefreshPolicy.leaveFailureMessage(sellsSlice: true, failureStatus: nil) == unconfirmed)
    }

    @Test("A sell-and-leave the server failed partway through is unconfirmed too")
    func serverFailureIsUnconfirmed() {
        for status in [500, 502, 503, 504] {
            #expect(GroupDetailRefreshPolicy.leaveMayHaveSoldSlice(sellsSlice: true, failureStatus: status))
            #expect(GroupDetailRefreshPolicy.leaveFailureMessage(sellsSlice: true, failureStatus: status) == unconfirmed)
        }
    }

    @Test("A sell-and-leave refused up front can be tried again")
    func clientRefusalIsFinal() {
        for status in [400, 403, 404, 429] {
            #expect(GroupDetailRefreshPolicy.leaveMayHaveSoldSlice(sellsSlice: true, failureStatus: status) == false)
            #expect(GroupDetailRefreshPolicy.leaveFailureMessage(sellsSlice: true, failureStatus: status) == refused)
        }
    }

    @Test("A leave with nothing to sell has no money at stake")
    func plainLeaveIsNeverUnconfirmed() {
        for status in [nil, 400, 500, 503] as [Int?] {
            #expect(GroupDetailRefreshPolicy.leaveMayHaveSoldSlice(sellsSlice: false, failureStatus: status) == false)
            #expect(GroupDetailRefreshPolicy.leaveFailureMessage(sellsSlice: false, failureStatus: status) == refused)
        }
    }

    @Test("The unconfirmed copy never promises a retry is safe")
    func unconfirmedCopyDoesNotInviteARetry() {
        let copy = GroupDetailRefreshPolicy.leaveFailureMessage(sellsSlice: true, failureStatus: nil)
        #expect(copy.contains("Check your slice"))
        #expect(!copy.lowercased().hasSuffix("try again"))
        #expect(!copy.lowercased().contains("safe"))
    }
}

@Suite("Leave that never left the phone")
struct GroupDetailLeaveNeverSentTests {
    /// With no connection at all the server never saw the request, so nothing was sold and the
    /// next tap is the first sale, not a second one. "Check your slice" there would send the
    /// member looking for a sale that cannot have happened.
    @Test("A sell-and-leave that never left the phone sold nothing and can be tried again")
    func neverSentIsARetry() {
        #expect(GroupDetailRefreshPolicy.leaveMayHaveSoldSlice(sellsSlice: true, failureStatus: nil, neverSent: true) == false)
        let copy = GroupDetailRefreshPolicy.leaveFailureMessage(sellsSlice: true, failureStatus: nil, neverSent: true)
        #expect(copy == "No connection, so nothing was sold. Check your internet and try again")
    }

    @Test("A request that did leave the phone and got no answer is still unconfirmed")
    func sentButUnansweredIsUnconfirmed() {
        #expect(GroupDetailRefreshPolicy.leaveMayHaveSoldSlice(sellsSlice: true, failureStatus: nil, neverSent: false))
    }

    @Test("A plain leave that never left the phone says only to try again")
    func plainLeaveNeverSent() {
        let copy = GroupDetailRefreshPolicy.leaveFailureMessage(sellsSlice: false, failureStatus: nil, neverSent: true)
        #expect(copy == "Couldn't leave this cabal. Try again")
    }
}
