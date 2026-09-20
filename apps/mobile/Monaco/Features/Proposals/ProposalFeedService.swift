import Foundation
import MonacoCore

enum ProposalVoteChoice: String {
    case yes
    case no
}

/// Backend calls the proposal feed, detail, and comment thread need.
/// Views depend on this protocol so the feed can run against the API or in-memory sample data.
@MainActor
protocol ProposalFeedService: AnyObject {
    /// The signed-in member's id, to find their ballot on a detail payload. Nil when unknown.
    var viewerId: String? { get }
    func listProposals(groupId: String, tab: ProposalFeedTab) async throws -> [ProposalDTO]
    func proposal(id: String) async throws -> ProposalDTO
    func castVote(proposalId: String, choice: ProposalVoteChoice) async throws
    func comments(proposalId: String) async throws -> [ProposalCommentDTO]
    func postComment(proposalId: String, body: String, parentId: String?) async throws -> ProposalCommentDTO
}

/// Live service over the MonacoCore API client, authenticated with the Privy session token.
@MainActor
final class LiveProposalFeedService: ProposalFeedService {
    private let client: MonacoCore.MonacoAPIClient

    init(auth: PrivyAuthService, baseURL: URL = Config.apiBaseURL) {
        client = MonacoCore.MonacoAPIClient(
            baseURL: baseURL,
            accessTokenProvider: { [weak auth] in
                await MainActor.run { auth?.accessToken }
            }
        )
    }

    var viewerId: String? {
        MonacoSessionStore().storedUserId
    }

    func listProposals(groupId: String, tab: ProposalFeedTab) async throws -> [ProposalDTO] {
        try await client.listGroupProposals(groupId: groupId, tab: tab).proposals
    }

    func proposal(id: String) async throws -> ProposalDTO {
        try await client.getProposalDetail(proposalId: id)
    }

    func castVote(proposalId: String, choice: ProposalVoteChoice) async throws {
        try await client.castVote(proposalId: proposalId, choice: choice.rawValue)
    }

    func comments(proposalId: String) async throws -> [ProposalCommentDTO] {
        try await client.listProposalComments(proposalId: proposalId).comments
    }

    func postComment(proposalId: String, body: String, parentId: String?) async throws -> ProposalCommentDTO {
        try await client.postProposalComment(proposalId: proposalId, body: body, parentId: parentId)
    }
}

/// One vote path for feed cards and detail: POST the ballot, then report a toast.
/// Callers reload the proposal afterwards so counts come from the server, not a local guess.
enum ProposalVoting {
    @MainActor
    static func cast(_ choice: ProposalVoteChoice, proposalId: String, service: ProposalFeedService) async -> (succeeded: Bool, toast: MonacoToast) {
        do {
            try await service.castVote(proposalId: proposalId, choice: choice)
            return (true, MonacoToast(message: ProposalFeedCopy.voteRecorded, isSuccess: true))
        } catch {
            return (false, MonacoToast(message: ProposalFeedErrorCopy.vote(error)))
        }
    }
}

/// Maps service errors to toast copy. Shared by feed cards and detail so both vote the same way.
enum ProposalFeedErrorCopy {
    static func vote(_ error: Error) -> String {
        switch httpStatus(error) {
        case 409: ProposalFeedCopy.voteClosed
        case 403: ProposalFeedCopy.voteNotEligible
        default: ProposalFeedCopy.voteFailed
        }
    }

    static func comment(_ error: Error) -> String {
        switch httpStatus(error) {
        case 400: ProposalFeedCopy.commentRejected
        case 404: ProposalFeedCopy.commentUnavailable
        case 429: ProposalFeedCopy.commentRateLimited
        default: ProposalFeedCopy.commentFailed
        }
    }

    private static func httpStatus(_ error: Error) -> Int? {
        if case MonacoCore.MonacoAPIError.httpStatus(let code) = error {
            return code
        }
        return nil
    }
}
