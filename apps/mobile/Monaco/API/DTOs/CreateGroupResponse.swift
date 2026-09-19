import Foundation

struct CreateGroupResponse: Codable, Equatable, Hashable {
    let groupId: String
    let name: String
    let treasuryAddress: String
}
