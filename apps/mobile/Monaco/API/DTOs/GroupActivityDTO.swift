import Foundation

struct GroupActivityItemDTO: Codable, Equatable, Identifiable {
    let id: String
    let kind: String
    let status: String
    let symbol: String?
    let amountMicros: Int64
    let createdAt: String
    let txSignature: String?
    let tokenAmount: String?
    let proceedsUsdcMicros: String?
    let initiatedBy: String?
    let agentDisplayName: String?
}

struct GroupActivityResponse: Codable, Equatable {
    let items: [GroupActivityItemDTO]
}

struct RetryTransactionResponse: Codable, Equatable {
    let transactionId: String
    let groupId: String
    let action: String
    let status: String
    let txSignature: String?
}
