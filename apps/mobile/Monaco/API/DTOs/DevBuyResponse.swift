import Foundation

struct DevBuyRequest: Encodable {
    let symbol: String
    let usdc: Int64
}

struct DevBuyResponse: Decodable {
    let transactionId: String
    let groupId: String
    let symbol: String
    let status: String
    let txHash: String?
    let created: Bool
}
