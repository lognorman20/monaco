#if DEBUG
import Foundation
import MonacoCore
import SwiftUI

/// Debug-only in-memory proposal backend for simulator QA without a Privy session.
/// Launch with `-MonacoProposalFeedSample` to open the feed on sample data (see docs/qa/149).
@MainActor
final class SampleProposalFeedService: ProposalFeedService {
    static let launchArgument = "-MonacoProposalFeedSample"

    static var isRequested: Bool {
        ProcessInfo.processInfo.arguments.contains(launchArgument)
    }

    private struct Record {
        var proposal: ProposalDTO
        var viewerVoted = false
    }

    private let viewerName = "You"
    private var records: [Record]
    private var comments: [String: [ProposalCommentDTO]]
    private let iso = ISO8601DateFormatter()

    init(now: Date = Date()) {
        let symbols = ["AAPLx", "NVDAx", "TSLAx", "MSFTx", "AMZNx", "GOOGLx", "METAx", "SPYx"]
        let proposers = ["Ada Park", "Ben Ortiz", "Cy Lin", "Dee Shah"]
        var built: [Record] = []
        for index in 0..<24 {
            let open = index < 20
            let yes = index % 3
            let no = index % 2
            built.append(Record(proposal: ProposalDTO(
                id: "sample-\(index)",
                symbol: symbols[index % symbols.count],
                status: open ? "open" : (index % 2 == 0 ? "passed" : "failed"),
                kind: index == 1 ? "sell" : (index == 2 ? "add_agent" : "buy"),
                usdcMicros: index == 1 || index == 2 ? nil : String((index + 1) * 12_500_000),
                tokenAmount: index == 1 ? "50000000" : nil,
                agentDisplayName: index == 2 ? "Scout" : nil,
                allocationUsdcMicros: index == 2 ? "500000000" : nil,
                canVote: open,
                proposerName: proposers[index % proposers.count],
                createdAt: iso.string(from: now.addingTimeInterval(TimeInterval(-900 * (index + 1)))),
                expiresAt: iso.string(from: now.addingTimeInterval(TimeInterval(3600 * (20 - index) + 1200))),
                voteSummary: ProposalVoteSummaryDTO(yesCount: yes, noCount: no, eligibleCount: 5, threshold: "majority"),
                commentCount: index == 0 ? 2 : 0
            )))
        }
        records = built
        comments = [
            "sample-0": [
                ProposalCommentDTO(id: "c-1", proposalId: "sample-0", authorId: "ben", authorName: "Ben Ortiz",
                                   body: "Why Apple over Nvidia this week?", createdAt: iso.string(from: now.addingTimeInterval(-1800))),
                ProposalCommentDTO(id: "c-2", proposalId: "sample-0", parentId: "c-1", authorId: "ada", authorName: "Ada Park",
                                   body: "Smaller drawdown for our first buy. Nvidia can be next.", createdAt: iso.string(from: now.addingTimeInterval(-1200))),
            ],
        ]
    }

    func listProposals(groupId: String, tab: ProposalFeedTab) async throws -> [ProposalDTO] {
        records.map(\.proposal).filter { ($0.status == "open") == (tab == .open) }
    }

    func proposal(id: String) async throws -> ProposalDTO {
        guard let record = records.first(where: { $0.proposal.id == id }) else {
            throw MonacoCore.MonacoAPIError.httpStatus(404)
        }
        let votes = record.viewerVoted
            ? [ProposalVoteDTO(voterId: "viewer", displayName: viewerName, choice: "yes")]
            : []
        let p = record.proposal
        return ProposalDTO(
            id: p.id, symbol: p.symbol, status: p.status, kind: p.kind, usdcMicros: p.usdcMicros, tokenAmount: p.tokenAmount, agentDisplayName: p.agentDisplayName, allocationUsdcMicros: p.allocationUsdcMicros, canVote: p.canVote,
            proposerName: p.proposerName, createdAt: p.createdAt, expiresAt: p.expiresAt,
            votes: votes, voteSummary: p.voteSummary, execution: ProposalExecutionDTO(state: "not_applicable"),
            commentCount: p.commentCount
        )
    }

    func castVote(proposalId: String, choice: ProposalVoteChoice) async throws {
        guard let index = records.firstIndex(where: { $0.proposal.id == proposalId }) else {
            throw MonacoCore.MonacoAPIError.httpStatus(404)
        }
        var record = records[index]
        guard record.proposal.canVote == true else { throw MonacoCore.MonacoAPIError.httpStatus(403) }
        let p = record.proposal
        let summary = p.voteSummary ?? ProposalVoteSummaryDTO(yesCount: 0, noCount: 0, eligibleCount: 5, threshold: "majority")
        record.viewerVoted = true
        record.proposal = ProposalDTO(
            id: p.id, symbol: p.symbol, status: p.status, kind: p.kind, usdcMicros: p.usdcMicros, tokenAmount: p.tokenAmount, agentDisplayName: p.agentDisplayName, allocationUsdcMicros: p.allocationUsdcMicros, canVote: false,
            proposerName: p.proposerName, createdAt: p.createdAt, expiresAt: p.expiresAt,
            voteSummary: ProposalVoteSummaryDTO(
                yesCount: summary.yesCount + (choice == .yes ? 1 : 0),
                noCount: summary.noCount + (choice == .no ? 1 : 0),
                eligibleCount: summary.eligibleCount,
                threshold: summary.threshold
            ),
            commentCount: p.commentCount
        )
        records[index] = record
    }

    func comments(proposalId: String) async throws -> [ProposalCommentDTO] {
        comments[proposalId] ?? []
    }

    func postComment(proposalId: String, body: String, parentId: String?) async throws -> ProposalCommentDTO {
        guard case .ready(let trimmed) = ProposalCommentDraft(text: body) else {
            throw MonacoCore.MonacoAPIError.httpStatus(400)
        }
        let comment = ProposalCommentDTO(
            id: UUID().uuidString, proposalId: proposalId, parentId: parentId, authorId: "viewer",
            authorName: viewerName, body: trimmed, createdAt: iso.string(from: Date())
        )
        comments[proposalId, default: []].append(comment)
        if let index = records.firstIndex(where: { $0.proposal.id == proposalId }) {
            let p = records[index].proposal
            records[index].proposal = ProposalDTO(
                id: p.id, symbol: p.symbol, status: p.status, kind: p.kind, usdcMicros: p.usdcMicros, tokenAmount: p.tokenAmount, agentDisplayName: p.agentDisplayName, allocationUsdcMicros: p.allocationUsdcMicros, canVote: p.canVote,
                proposerName: p.proposerName, createdAt: p.createdAt, expiresAt: p.expiresAt,
                voteSummary: p.voteSummary, commentCount: (p.commentCount ?? 0) + 1
            )
        }
        return comment
    }
}

/// Root for the sample-data launch: a labelled feed so screenshots can't be mistaken for live data.
struct SampleProposalFeedRoot: View {
    @State private var service = SampleProposalFeedService()

    var body: some View {
        NavigationStack {
            ProposalFeedView(service: service, groupId: "sample", title: "Sample data")
        }
        .tint(MonacoTheme.accent)
    }
}
#endif
