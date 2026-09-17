import Foundation

public struct PotRowDTO: Codable, Equatable, Sendable, Identifiable {
    public let symbol: String
    public let units: String
    public let markUsd: String
    public let valueUsd: String
    public let afterHours: Bool?

    public var id: String { symbol }

    public init(symbol: String, units: String, markUsd: String, valueUsd: String, afterHours: Bool?) {
        self.symbol = symbol
        self.units = units
        self.markUsd = markUsd
        self.valueUsd = valueUsd
        self.afterHours = afterHours
    }
}

public struct MemberSliceDTO: Codable, Equatable, Sendable {
    public let shareUnits: String
    public let equityUsd: String
    public let slicePercent: String
    public let dollarPnl: String
    public let percentReturn: String?

    public init(
        shareUnits: String,
        equityUsd: String,
        slicePercent: String,
        dollarPnl: String,
        percentReturn: String?
    ) {
        self.shareUnits = shareUnits
        self.equityUsd = equityUsd
        self.slicePercent = slicePercent
        self.dollarPnl = dollarPnl
        self.percentReturn = percentReturn
    }
}

public struct LeaderboardRowDTO: Codable, Equatable, Sendable, Identifiable {
    public let rank: Int
    public let userId: String
    public let displayName: String
    public let percentReturn: String?
    public let dollarPnl: String

    public var id: String { userId }

    public init(rank: Int, userId: String, displayName: String, percentReturn: String?, dollarPnl: String) {
        self.rank = rank
        self.userId = userId
        self.displayName = displayName
        self.percentReturn = percentReturn
        self.dollarPnl = dollarPnl
    }
}

public struct GroupViewDTO: Codable, Equatable, Sendable {
    public let id: String
    public let name: String
    public let treasuryAddress: String?
    public let pot: [PotRowDTO]
    public let you: MemberSliceDTO
    public let members: [LeaderboardRowDTO]
    public let proposals: [ProposalDTO]?

    public init(
        id: String,
        name: String,
        treasuryAddress: String? = nil,
        pot: [PotRowDTO],
        you: MemberSliceDTO,
        members: [LeaderboardRowDTO],
        proposals: [ProposalDTO]?
    ) {
        self.id = id
        self.name = name
        self.treasuryAddress = treasuryAddress
        self.pot = pot
        self.you = you
        self.members = members
        self.proposals = proposals
    }
}

public enum MemberBoardRenderer {
    public static func displayRows(from members: [LeaderboardRowDTO]) -> [LeaderboardRowDTO] {
        members
    }
}
