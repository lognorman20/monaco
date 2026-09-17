import Foundation

struct CreateDepositResponse: Decodable {
    let depositId: String
    let groupId: String
    let amount: Int64
    let status: String
    let fromAddress: String
}

struct GetDepositResponse: Decodable {
    let depositId: String
    let groupId: String
    let amount: Int64
    let status: String
    let fromAddress: String?
    let txSignature: String?
    let shareUnits: Int64
    let createdAt: String
}
