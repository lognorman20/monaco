import Foundation

public struct CreatePlatformWithdrawalRequestDTO: Encodable, Sendable {
    public let amount: Int64
    public let toAddress: String

    public init(amount: Int64, toAddress: String) {
        self.amount = amount
        self.toAddress = toAddress
    }
}

public struct PlatformWithdrawalResponseDTO: Decodable, Equatable, Sendable {
    public let withdrawalId: String
    public let amount: Int64
    public let toAddress: String
    public let status: String
    public let txSignature: String?
    public let createdAt: String

    public init(
        withdrawalId: String,
        amount: Int64,
        toAddress: String,
        status: String,
        txSignature: String?,
        createdAt: String
    ) {
        self.withdrawalId = withdrawalId
        self.amount = amount
        self.toAddress = toAddress
        self.status = status
        self.txSignature = txSignature
        self.createdAt = createdAt
    }
}
