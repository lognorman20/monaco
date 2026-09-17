import Foundation

public struct ProposalDTO: Codable, Equatable, Sendable, Identifiable {
    public let id: String
    public let symbol: String
    public let usdcMicros: String
    public let status: String
    public let canVote: Bool?

    public init(id: String, symbol: String, usdcMicros: String, status: String, canVote: Bool? = nil) {
        self.id = id
        self.symbol = symbol
        self.usdcMicros = usdcMicros
        self.status = status
        self.canVote = canVote
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
