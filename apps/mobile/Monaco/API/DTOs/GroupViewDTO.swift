import Foundation

struct PotRowDTO: Codable, Equatable, Identifiable {
    let symbol: String
    let units: String
    let markUsd: String
    let valueUsd: String
    let afterHours: Bool?

    var id: String { symbol }
}

struct MemberSliceDTO: Codable, Equatable {
    let shareUnits: String
    let equityUsd: String
    let slicePercent: String
    let dollarPnl: String
    let percentReturn: String?
}

struct LeaderboardRowDTO: Codable, Equatable, Identifiable {
    let rank: Int
    let userId: String
    let displayName: String
    let percentReturn: String?
    let dollarPnl: String

    var id: String { userId }
}

struct GroupViewDTO: Codable, Equatable {
    let id: String
    let name: String
    let treasuryAddress: String?
    let pot: [PotRowDTO]
    let you: MemberSliceDTO
    let members: [LeaderboardRowDTO]
    let proposals: [ProposalDTO]?
}
