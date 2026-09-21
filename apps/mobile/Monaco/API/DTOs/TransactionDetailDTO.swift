import Foundation

struct TransactionDetailDTO: Codable, Equatable {
    let transactionId: String
    let groupId: String
    let action: String
    let status: String
    /// USDC micros: what a buy spent, or what a confirmed sell received. Zero for a sell with no proceeds yet.
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
    /// A sell's USDC proceeds; nil until the sell confirms with recorded proceeds.
    let proceedsUsdcMicros: Int64?
    /// A sell's quantity in token atomics (100_000_000 per share); nil for a buy.
    let tokenAmount: Int64?
}
