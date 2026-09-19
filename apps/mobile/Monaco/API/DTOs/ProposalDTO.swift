import Foundation

struct ProposalDTO: Codable, Equatable, Identifiable {
    let id: String
    let symbol: String
    let kind: String?
    let usdcMicros: String?
    let tokenAmount: String?
    let status: String
    let canVote: Bool?
    let proposerId: String?
    let proposerName: String?
    let createdAt: String?
    let expiresAt: String?
    let groupId: String?
    let votes: [ProposalVoteDTO]?
    let voteSummary: ProposalVoteSummaryDTO?
    let execution: ProposalExecutionDTO?
    let agentDisplayName: String?
    let allocationUsdcMicros: String?
    let mintedAgentKey: String?

    var resolvedKind: String {
        let raw = kind?.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() ?? ""
        return raw.isEmpty ? "buy" : raw
    }
}

enum ProposalStatusChipStyle: String {
    case open
    case passed
    case failed
    case expired

    init?(status: String) {
        self.init(rawValue: status.lowercased())
    }

    var label: String {
        switch self {
        case .open: "Open"
        case .passed: "Passed"
        case .failed: "Failed"
        case .expired: "Expired"
        }
    }
}
