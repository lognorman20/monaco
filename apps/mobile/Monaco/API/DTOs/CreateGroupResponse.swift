import Foundation

struct CreateGroupResponse: Codable, Equatable {
    let groupId: String
    let name: String
    let treasuryAddress: String
}
