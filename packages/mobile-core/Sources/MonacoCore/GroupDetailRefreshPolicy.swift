import Foundation

/// The rules the cabal screen follows while it keeps itself fresh: how closely to watch, and what
/// a failed background read is allowed to do to what the member is already looking at.
///
/// Pure values with no clock and no networking, so `GroupDetailRefreshPolicyTests` can drive every
/// case on the host instead of against a live screen.

// MARK: - Cadence

/// How often the cabal screen re-reads itself.
public enum GroupDetailCadence {
    /// How long the screen keeps watching closely after the last open vote disappears.
    ///
    /// A proposal leaves the open list the moment it passes, and the swap it triggered only
    /// reaches the pot and the activity feed a few seconds later. Dropping straight back to the
    /// resting cadence at that exact moment is what makes a winning vote look like it did nothing.
    public static let voteSettlingWindow: Duration = .seconds(30)

    /// Everything the cabal screen currently has in play.
    public struct Inputs: Equatable, Sendable {
        /// A proposal is collecting votes right now.
        public var hasOpenVotes: Bool
        /// Something in the activity feed is still on its way through: a buy or sell executing,
        /// or money on its way into the pot. Both land within seconds and both move the pot.
        public var hasPendingActivity: Bool
        /// The last open vote closed within `voteSettlingWindow`, so its outcome is still landing.
        public var isWatchingVoteOutcome: Bool

        public init(
            hasOpenVotes: Bool = false,
            hasPendingActivity: Bool = false,
            isWatchingVoteOutcome: Bool = false
        ) {
            self.hasOpenVotes = hasOpenVotes
            self.hasPendingActivity = hasPendingActivity
            self.isWatchingVoteOutcome = isWatchingVoteOutcome
        }
    }

    public static func interval(for inputs: Inputs) -> Duration {
        if inputs.hasOpenVotes || inputs.hasPendingActivity || inputs.isWatchingVoteOutcome {
            return LiveRefreshCadence.inPlay
        }
        return LiveRefreshCadence.resting
    }

    /// Whether an activity row's status says it has not settled yet.
    ///
    /// The server writes `pending` for a swap executing, a deposit on its way into the pot and a
    /// proposal's swap waiting to run; everything else (`confirmed`, `failed`, `failed: …`) is
    /// settled and needs no closer watching.
    public static func isStillGoingThrough(status: String) -> Bool {
        status.lowercased() == "pending"
    }
}

// MARK: - The admin-only join requests card

/// What a join-requests read tells the screen to do with the card the admin is looking at.
public enum JoinRequestsLoadOutcome: Equatable, Sendable {
    /// The server answered: show what it sent.
    case replace
    /// The server says this viewer has no business seeing the card.
    case clear
    /// Nothing trustworthy came back. Leave the card exactly as the admin last saw it.
    case keep
}

public enum GroupDetailRefreshPolicy {
    /// What a failed join-requests read means for the card.
    ///
    /// Only the server saying "not yours" empties it. A dropped request, a timeout or an offline
    /// blip must not blink a pending request out from under the admin who was about to answer it.
    ///
    /// - Parameters:
    ///   - failureStatus: the HTTP status the request failed with, or nil for a transport failure.
    ///   - wasCancelled: the request never got an answer because the screen moved on.
    public static func joinRequestsOutcome(failureStatus: Int?, wasCancelled: Bool) -> JoinRequestsLoadOutcome {
        if wasCancelled { return .keep }
        switch failureStatus {
        case 403, 404: return .clear
        default: return .keep
        }
    }

    /// Whether the viewer could still be an admin after a failed read.
    ///
    /// A 403 is the server's final word, so the screen stops asking for the rest of the visit
    /// rather than sending a request it knows will be refused on every load and every poll tick.
    public static func viewerMayBeAdmin(afterFailureStatus status: Int?) -> Bool {
        status != 403
    }

    /// Whether a refused approve/deny means the request is no longer there to answer — someone
    /// else got to it, or the person withdrew it. The row goes, rather than sitting on screen
    /// with live buttons that can only fail.
    public static func joinRequestAlreadyAnswered(failureStatus: Int?) -> Bool {
        switch failureStatus {
        case 404, 409, 410: return true
        default: return false
        }
    }

    // MARK: - Leaving

    /// Whether a leave that did not come back as done may still have sold the member's slice.
    ///
    /// The server sells the slice first and only then removes the member, and the sale can take
    /// most of a minute. A request that never got an answer, or a server that failed partway (a
    /// 5xx), may well have sold it. A 4xx is the server refusing before it sold anything, and a
    /// leave with nothing to sell has no money at stake either way. (A blocked leave is answered
    /// with its own reason and never reaches this.)
    ///
    /// - Parameters:
    ///   - sellsSlice: the member chose "Sell and leave".
    ///   - failureStatus: the HTTP status the request failed with, or nil when it got no answer.
    public static func leaveMayHaveSoldSlice(sellsSlice: Bool, failureStatus: Int?) -> Bool {
        guard sellsSlice else { return false }
        guard let failureStatus else { return true }
        return failureStatus >= 500
    }

    /// What the cabal screen says when a leave did not come back as done.
    ///
    /// No idempotency key rides on a leave, so pressing "Sell and leave" again is a brand-new
    /// request, not a replay of the lost one. When the sale may already have gone through, the
    /// member is sent to look at their slice before anything else, and is never told to simply
    /// try again.
    public static func leaveFailureMessage(sellsSlice: Bool, failureStatus: Int?) -> String {
        if leaveMayHaveSoldSlice(sellsSlice: sellsSlice, failureStatus: failureStatus) {
            return "We couldn't confirm that. Check your slice below before trying again"
        }
        return "Couldn't leave this cabal. Try again"
    }
}
