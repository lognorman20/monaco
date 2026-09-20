import Foundation

enum JoinGroupOutcome: String, Codable, Equatable {
    case joined
    case pending
    case alreadyMember = "already_member"
}

struct JoinGroupStatusResponse: Codable, Equatable {
    let status: JoinGroupOutcome
}

struct JoinRequestDTO: Codable, Equatable, Identifiable {
    let id: String
    let userId: String
    let displayName: String
    var profilePhotoUrl: String? = nil
    let requestedAt: String
}

struct JoinRequestsListResponse: Codable, Equatable {
    let items: [JoinRequestDTO]
}
