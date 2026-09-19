import Foundation

struct PlatformBalanceDTO: Codable, Equatable {
    let availableUsdcMicros: Int64
    let memberWalletAddress: String
    let pendingAllocationMicros: Int64
}

struct FundGroupRequest: Encodable {
    let amount: Int64
}

struct FundGroupResponse: Decodable, Equatable {
    let depositId: String
    let groupId: String
    let amount: Int64
    let status: String
    let fromAddress: String
}
