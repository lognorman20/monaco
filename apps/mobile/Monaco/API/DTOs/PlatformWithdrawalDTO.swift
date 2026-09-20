import Foundation

struct CreatePlatformWithdrawalRequest: Encodable {
    let amount: Int64
    let toAddress: String
}

struct PlatformWithdrawalDTO: Decodable, Equatable {
    let withdrawalId: String
    let amount: Int64
    let toAddress: String
    let status: String
    let txHash: String?
    let createdAt: String
}
