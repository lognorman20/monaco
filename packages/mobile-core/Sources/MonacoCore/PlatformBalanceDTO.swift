import Foundation

public struct PlatformBalanceDTO: Codable, Equatable, Sendable {
    public let availableUsdcMicros: Int64
    public let memberWalletAddress: String
    public let pendingAllocationMicros: Int64

    public init(
        availableUsdcMicros: Int64,
        memberWalletAddress: String,
        pendingAllocationMicros: Int64
    ) {
        self.availableUsdcMicros = availableUsdcMicros
        self.memberWalletAddress = memberWalletAddress
        self.pendingAllocationMicros = pendingAllocationMicros
    }
}

public struct FundGroupRequestDTO: Encodable, Sendable {
    public let amount: Int64

    public init(amount: Int64) {
        self.amount = amount
    }
}

public struct FundGroupResponseDTO: Decodable, Equatable, Sendable {
    public let depositId: String
    public let groupId: String
    public let amount: Int64
    public let status: String
    public let fromAddress: String

    public init(depositId: String, groupId: String, amount: Int64, status: String, fromAddress: String) {
        self.depositId = depositId
        self.groupId = groupId
        self.amount = amount
        self.status = status
        self.fromAddress = fromAddress
    }
}
