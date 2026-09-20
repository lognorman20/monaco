import Foundation

struct TransactionDetailDTO: Codable, Equatable {
    let transactionId: String
    let groupId: String
    let action: String
    let status: String
    let amountMicros: Int64
    let inputToken: String?
    let outputToken: String?
    let inputSymbol: String?
    let outputSymbol: String?
    let txHash: String?
    let executeRequestId: String?
    let proposalId: String?
    let costBasisPrice: Int64?
    let costBasisAmount: Int64?
    let createdAt: String
    let confirmedAt: String?
    let failureReason: String?
    let proceedsUsdcMicros: Int64?
}
