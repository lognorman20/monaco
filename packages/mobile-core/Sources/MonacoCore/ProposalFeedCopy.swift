import Foundation

/// User-facing strings for the proposal feed, detail thread, and composer.
/// Kept here so host tests can audit them against `MainFlowCopyAudit`.
public enum ProposalFeedCopy {
    public static let feedTitle = "Proposals"
    public static let feedLinkTitle = "All proposals"
    public static let emptyOpen = "No open votes. Propose a buy to get the cabal voting."
    public static let emptyClosed = "No closed votes yet."
    public static let loadFailed = "Could not load proposals."

    public static let needsYourVote = "Needs your vote"
    public static let voteYes = "Vote yes"
    public static let voteNo = "Vote no"
    public static let voteRecorded = "Vote recorded"
    public static let voteClosed = "Voting on this buy has closed."
    public static let voteNotEligible = "You can't vote on this buy."
    public static let voteFailed = "Could not submit vote."

    public static let commentsTitle = "Discussion"
    public static let emptyThread = "No comments yet. Make the case for or against this buy."
    public static let composerPlaceholder = "Add a comment"
    public static let reply = "Reply"
    public static let send = "Post"
    public static let commentPosted = "Comment posted"
    public static let replyPosted = "Reply posted"
    public static let commentsLoadFailed = "Could not load comments."
    public static let commentTooLong = "Comments can be up to 1,000 characters."
    public static let commentRejected = "That comment could not be posted. Edit it and try again."
    public static let commentRateLimited = "You're commenting fast. Try again in a minute."
    public static let commentUnavailable = "This proposal is no longer available."
    public static let commentFailed = "Could not post comment."

    public static func replyingTo(_ name: String) -> String {
        "Replying to \(name)"
    }

    public static func commentCount(_ count: Int) -> String {
        count == 1 ? "1 comment" : "\(count) comments"
    }

    public static func proposedBy(_ name: String) -> String {
        "Proposed by \(name)"
    }

    public static func buyHeadline(symbol: String, amount: String) -> String {
        "Buy \(amount) of \(symbol)"
    }

    public static func sellHeadline(symbol: String, shares: String) -> String {
        "Sell \(shares) \(symbol)"
    }

    public static let agentTitle = "Cabal agent"

    /// Card title: the stock for trades, the agent's name for agent governance proposals.
    public static func title(for proposal: ProposalDTO) -> String {
        if proposal.isTrade {
            return AssetSymbolFormatter.format(proposal.symbol)
        }
        let name = proposal.agentDisplayName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? agentTitle : name
    }

    /// Card headline per kind, e.g. "Buy $25.00 of AAPLx", "Sell 0.5 AAPLx",
    /// "Add agent Scout with $500.00 budget", "Pause the cabal trading agent".
    public static func headline(for proposal: ProposalDTO) -> String {
        let symbol = AssetSymbolFormatter.format(proposal.symbol)
        switch proposal.resolvedKind {
        case "sell":
            return sellHeadline(symbol: symbol, shares: ProposalShareFormatter.shares(fromAtomics: proposal.tokenAmount ?? "0"))
        case "add_agent":
            let name = proposal.agentDisplayName ?? proposal.symbol
            let budget = ProposalAmountFormatter.dollars(fromMicros: proposal.allocationUsdcMicros ?? "0")
            return "Add agent \(name) with \(budget) budget"
        case "pause_agent":
            return "Pause the cabal trading agent"
        case "resume_agent":
            return "Resume the cabal trading agent"
        case "revoke_agent":
            return "Revoke the cabal trading agent"
        default:
            return buyHeadline(symbol: symbol, amount: ProposalAmountFormatter.dollars(fromMicros: proposal.usdcMicros ?? "0"))
        }
    }

    public static func openCount(_ count: Int) -> String {
        count == 1 ? "1 open vote" : "\(count) open votes"
    }

    /// Every static string plus representative formatted ones, for copy audits.
    public static let auditedStrings: [String] = [
        feedTitle, feedLinkTitle, emptyOpen, emptyClosed, loadFailed,
        needsYourVote, voteYes, voteNo, voteRecorded, voteClosed, voteNotEligible, voteFailed,
        commentsTitle, emptyThread, composerPlaceholder, reply, send,
        commentPosted, replyPosted, commentsLoadFailed, commentTooLong,
        commentRejected, commentRateLimited, commentUnavailable, commentFailed,
        replyingTo("Ada"), commentCount(2), proposedBy("Ada"),
        buyHeadline(symbol: "AAPLx", amount: "$25.00"), sellHeadline(symbol: "AAPLx", shares: "0.5"), openCount(3), agentTitle,
        headline(for: ProposalDTO(id: "a", symbol: "", status: "open", kind: "add_agent", agentDisplayName: "Scout", allocationUsdcMicros: "500000000")),
        headline(for: ProposalDTO(id: "p", symbol: "", status: "open", kind: "pause_agent")),
    ]
}
