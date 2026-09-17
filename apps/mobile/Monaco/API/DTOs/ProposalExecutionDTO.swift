import Foundation

struct ProposalVoteSummaryDTO: Codable, Equatable {
    let yesCount: Int
    let noCount: Int
    let eligibleCount: Int
    let threshold: String
}

struct ProposalExecutionDTO: Codable, Equatable {
    let state: String
    let txSignature: String?
    let transactionId: String?
    let executeRequestId: String?
    let executedAt: String?
    let failureReason: String?
}
