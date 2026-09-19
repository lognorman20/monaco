import Foundation

public struct PotRowDTO: Codable, Equatable, Sendable, Identifiable {
    public let symbol: String
    public let units: String
    public let markUsd: String
    public let valueUsd: String
    public let dollarPnl: String
    public let afterHours: Bool?
    public let tokenAmount: String?

    public var id: String { symbol }

    public init(
        symbol: String,
        units: String,
        markUsd: String,
        valueUsd: String,
        dollarPnl: String,
        afterHours: Bool?,
        tokenAmount: String? = nil
    ) {
        self.symbol = symbol
        self.units = units
        self.markUsd = markUsd
        self.valueUsd = valueUsd
        self.dollarPnl = dollarPnl
        self.afterHours = afterHours
        self.tokenAmount = tokenAmount
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

public struct GroupAgentDTO: Codable, Equatable, Sendable {
    public let id: String
    public let status: String
    public let agentDisplayName: String
    public let allocationUsdcMicros: String

    public init(
        id: String,
        status: String,
        agentDisplayName: String,
        allocationUsdcMicros: String
    ) {
        self.id = id
        self.status = status
        self.agentDisplayName = agentDisplayName
        self.allocationUsdcMicros = allocationUsdcMicros
    }
}

public struct GroupViewDTO: Codable, Equatable, Sendable {
    public let id: String
    public let name: String
    public let treasuryAddress: String?
    public let potTotalUsd: String?
    public let pot: [PotRowDTO]
    public let you: MemberSliceDTO
    public let members: [LeaderboardRowDTO]
    public let proposals: [ProposalDTO]?
    public let agent: GroupAgentDTO?

    public init(
        id: String,
        name: String,
        treasuryAddress: String? = nil,
        potTotalUsd: String? = nil,
        pot: [PotRowDTO],
        you: MemberSliceDTO,
        members: [LeaderboardRowDTO],
        proposals: [ProposalDTO]?,
        agent: GroupAgentDTO? = nil
    ) {
        self.id = id
        self.name = name
        self.treasuryAddress = treasuryAddress
        self.potTotalUsd = potTotalUsd
        self.pot = pot
        self.you = you
        self.members = members
        self.proposals = proposals
        self.agent = agent
    }

    /// Marked pot NAV; falls back to summing row values when the server omits potTotalUsd.
    public var resolvedPotTotalUsd: String {
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

public enum MemberBoardRenderer {
    public static func displayRows(from members: [LeaderboardRowDTO]) -> [LeaderboardRowDTO] {
        members
    }
}
