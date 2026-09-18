import Foundation

/// User-facing strings for the proposal feed, detail thread, and composer.
/// Kept here so host tests can audit them against `MainFlowCopyAudit`.
public enum ProposalFeedCopy {
    public static let feedTitle = "Proposals"
    public static let feedLinkTitle = "All proposals"
    public static let emptyOpen = "No open votes. Propose a buy to get the cabal voting."
    public static let emptyClosed = "No closed votes yet."
    public static let loadFailed = "Could not load proposals."

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

    /// Card headline for either side: "Buy $25.00 of AAPLx" or "Sell 0.5 AAPLx".
    public static func headline(for proposal: ProposalDTO) -> String {
        let symbol = AssetSymbolFormatter.format(proposal.symbol)
        if proposal.isSell {
            return sellHeadline(symbol: symbol, shares: ProposalShareFormatter.shares(fromAtomics: proposal.tokenAmount ?? "0"))
        }
        return buyHeadline(symbol: symbol, amount: ProposalAmountFormatter.dollars(fromMicros: proposal.usdcMicros ?? "0"))
    }

    public static func openCount(_ count: Int) -> String {
        count == 1 ? "1 open vote" : "\(count) open votes"
    }

    /// Every static string plus representative formatted ones, for copy audits.
    public static let auditedStrings: [String] = [
        feedTitle, feedLinkTitle, emptyOpen, emptyClosed, loadFailed,
        voteYes, voteNo, voteRecorded, voteClosed, voteNotEligible, voteFailed,
        commentsTitle, emptyThread, composerPlaceholder, reply, send,
        commentPosted, replyPosted, commentsLoadFailed, commentTooLong,
        commentRejected, commentRateLimited, commentUnavailable, commentFailed,
        replyingTo("Ada"), commentCount(2), proposedBy("Ada"),
        buyHeadline(symbol: "AAPLx", amount: "$25.00"), sellHeadline(symbol: "AAPLx", shares: "0.5"), openCount(3),
    ]
}
