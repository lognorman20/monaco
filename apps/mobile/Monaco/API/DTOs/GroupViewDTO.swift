import Foundation

struct PotRowDTO: Codable, Equatable, Identifiable {
    let symbol: String
    let units: String
    let markUsd: String
    let valueUsd: String
    let dollarPnl: String
    let afterHours: Bool?
    let tokenAmount: String?

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

struct GroupAgentDTO: Codable, Equatable {
    let id: String
    let status: String
    let agentDisplayName: String
    let allocationUsdcMicros: String
}

struct GroupViewDTO: Codable, Equatable {
    let id: String
    let name: String
    let treasuryAddress: String?
    let potTotalUsd: String?
    let pot: [PotRowDTO]
    let you: MemberSliceDTO
    let members: [LeaderboardRowDTO]
    let proposals: [ProposalDTO]?
    let agent: GroupAgentDTO?

    var resolvedPotTotalUsd: String {
        if let potTotalUsd, !potTotalUsd.isEmpty {
            return potTotalUsd
        }
        let sum = pot.reduce(Decimal.zero) { partial, row in
            partial + (Decimal(string: row.valueUsd) ?? .zero)
        }
        var rounded = sum
        var result = Decimal()
        NSDecimalRound(&result, &rounded, 2, .plain)
        return NSDecimalNumber(decimal: result).stringValue
    }
}
