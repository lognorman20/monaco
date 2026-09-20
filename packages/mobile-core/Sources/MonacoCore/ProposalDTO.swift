import Foundation

/// Proposal row from `GET /v1/groups/{id}/proposals` (feed card) or `GET /v1/proposals/{id}` (detail).
/// Detail-only fields (`votes`, `execution`, `groupId`) are nil on list items.
public struct ProposalDTO: Codable, Equatable, Sendable, Identifiable {
    public let id: String
    public let symbol: String
    public let kind: String?
    public let usdcMicros: String?
    public let tokenAmount: String?
    /// Agent governance proposals (`add_agent`, `pause_agent`, ...) name the agent and its budget.
    public let agentDisplayName: String?
    public let allocationUsdcMicros: String?
    /// Plaintext API key on a passed `add_agent` proposal detail for cabal members; never on list items.
    public let mintedAgentKey: String?
    public let status: String
    public let canVote: Bool?
    public let thesis: String?
    public let proposerId: String?
    public let proposerName: String?
    public let createdAt: String?
    public let expiresAt: String?
    public let groupId: String?
    public let votes: [ProposalVoteDTO]?
    public let voteSummary: ProposalVoteSummaryDTO?
    public let execution: ProposalExecutionDTO?
    public let commentCount: Int?

    public init(
        id: String,
        symbol: String,
        status: String,
        kind: String? = nil,
        usdcMicros: String? = nil,
        tokenAmount: String? = nil,
        agentDisplayName: String? = nil,
        allocationUsdcMicros: String? = nil,
        mintedAgentKey: String? = nil,
        canVote: Bool? = nil,
        thesis: String? = nil,
        proposerId: String? = nil,
        proposerName: String? = nil,
        createdAt: String? = nil,
        expiresAt: String? = nil,
        groupId: String? = nil,
        votes: [ProposalVoteDTO]? = nil,
        voteSummary: ProposalVoteSummaryDTO? = nil,
        execution: ProposalExecutionDTO? = nil,
        commentCount: Int? = nil
    ) {
        self.id = id
        self.symbol = symbol
        self.kind = kind
        self.usdcMicros = usdcMicros
        self.tokenAmount = tokenAmount
        self.agentDisplayName = agentDisplayName
        self.allocationUsdcMicros = allocationUsdcMicros
        self.mintedAgentKey = mintedAgentKey
        self.status = status
        self.canVote = canVote
        self.thesis = thesis
        self.proposerId = proposerId
        self.proposerName = proposerName
        self.createdAt = createdAt
        self.expiresAt = expiresAt
        self.groupId = groupId
        self.votes = votes
        self.voteSummary = voteSummary
        self.execution = execution
        self.commentCount = commentCount
    }

    public var resolvedKind: String {
        let raw = kind?.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() ?? ""
        return raw.isEmpty ? "buy" : raw
    }

    public var isSell: Bool {
        resolvedKind == "sell"
    }

    /// Buy or sell of a stock; agent governance kinds have no trade execution.
    public var isTrade: Bool {
        resolvedKind == "buy" || resolvedKind == "sell"
    }

    public var isOpen: Bool {
        status.lowercased() == ProposalStatusDisplay.open.rawValue
    }

    /// Inline yes/no buttons show only while the server says this viewer may still vote.
    public var showsVoteActions: Bool {
        isOpen && canVote == true
    }
}

public struct ProposalVoteDTO: Codable, Equatable, Sendable, Identifiable {
    public let voterId: String
    public let displayName: String
    public let choice: String
    public let castAt: String?

    public var id: String { voterId }

    public init(voterId: String, displayName: String, choice: String, castAt: String? = nil) {
        self.voterId = voterId
        self.displayName = displayName
        self.choice = choice
        self.castAt = castAt
    }
}

public struct ProposalVoteSummaryDTO: Codable, Equatable, Sendable {
    public let yesCount: Int
    public let noCount: Int
    public let eligibleCount: Int
    public let threshold: String

    public init(yesCount: Int, noCount: Int, eligibleCount: Int, threshold: String) {
        self.yesCount = yesCount
        self.noCount = noCount
        self.eligibleCount = eligibleCount
        self.threshold = threshold
    }
}

public struct ProposalExecutionDTO: Codable, Equatable, Sendable {
    public let state: String
    public let txSignature: String?
    public let transactionId: String?
    public let executeRequestId: String?
    public let executedAt: String?
    public let failureReason: String?

    public init(
        state: String,
        txSignature: String? = nil,
        transactionId: String? = nil,
        executeRequestId: String? = nil,
        executedAt: String? = nil,
        failureReason: String? = nil
    ) {
        self.state = state
        self.txSignature = txSignature
        self.transactionId = transactionId
        self.executeRequestId = executeRequestId
        self.executedAt = executedAt
        self.failureReason = failureReason
    }
}

public struct ProposalListResponseDTO: Codable, Equatable, Sendable {
    public let proposals: [ProposalDTO]

    public init(proposals: [ProposalDTO]) {
        self.proposals = proposals
    }
}

/// `tab` query value for `GET /v1/groups/{id}/proposals`.
public enum ProposalFeedTab: String, CaseIterable, Identifiable, Sendable {
    case open
    case closed

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .open: "Open"
        case .closed: "Closed"
        }
    }
}

public enum ProposalStatusDisplay: String, CaseIterable {
    case open
    case passed
    case failed
    case expired

    public var label: String {
        switch self {
        case .open: "Open"
        case .passed: "Passed"
        case .failed: "Failed"
        case .expired: "Expired"
        }
    }

    public static func from(status: String) -> ProposalStatusDisplay? {
        ProposalStatusDisplay(rawValue: status.lowercased())
    }
}

public struct CreateProposalResponseDTO: Codable, Equatable, Sendable {
    public let proposalId: String

    public init(proposalId: String) {
        self.proposalId = proposalId
    }
}
