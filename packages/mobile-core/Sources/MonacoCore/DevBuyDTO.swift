import Foundation

public struct DevBuyRequestDTO: Encodable, Sendable {
    public let symbol: String
    public let usdc: Int64

    public init(symbol: String, usdc: Int64) {
        self.symbol = symbol
        self.usdc = usdc
    }
}

public struct DevBuyResponseDTO: Decodable, Sendable {
    public let transactionId: String
    public let groupId: String
    public let symbol: String
    public let status: String
    public let txHash: String?
    public let created: Bool
}
