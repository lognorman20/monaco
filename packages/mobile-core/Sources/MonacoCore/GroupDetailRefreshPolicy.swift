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
        /// A buy or sell is on its way through.
        public var hasPendingSwap: Bool
        /// Money is on its way into the pot.
        public var hasPendingDeposit: Bool
        /// The last open vote closed within `voteSettlingWindow`, so its outcome is still landing.
        public var isWatchingVoteOutcome: Bool

        public init(
            hasOpenVotes: Bool = false,
            hasPendingSwap: Bool = false,
            hasPendingDeposit: Bool = false,
            isWatchingVoteOutcome: Bool = false
        ) {
            self.hasOpenVotes = hasOpenVotes
            self.hasPendingSwap = hasPendingSwap
            self.hasPendingDeposit = hasPendingDeposit
            self.isWatchingVoteOutcome = isWatchingVoteOutcome
        }
    }

    public static func interval(for inputs: Inputs) -> Duration {
        if inputs.hasPendingDeposit { return DepositPolling.sweepStatusInterval }
        if inputs.hasOpenVotes || inputs.hasPendingSwap || inputs.isWatchingVoteOutcome {
            return LiveRefreshCadence.inPlay
        }
        return LiveRefreshCadence.resting
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
}
