import Foundation

public struct PotRowDTO: Codable, Equatable, Sendable, Identifiable {
    public let symbol: String
    public let units: String
    public let markUsd: String
    public let valueUsd: String
    public let dollarPnl: String
    public let afterHours: Bool?
    public let tokenAmount: String?
    /// The stock's day change, as the market list routes report it. Distinct from
    /// `dollarPnl`, which is what this cabal has made on the position since it
    /// bought — the two answer different questions and the row shows both.
    public let change24h: String?
    /// The day's closes for the row's sparkline, in USDC micros. Empty when the
    /// backend had no series; the row then draws none.
    public let sparkUsdcMicros: [Int64]
    /// Which instrument each half of the row is about. `change24h` is the xStock
    /// token's move; the series is Pyth's underlying equity. They diverge, so when
    /// the backend says they are different instruments the row tints its line from
    /// its own line rather than from a number measured on something else.
    public let sparkBasis: MarketPriceBasis?
    public let sparkBasisSymbol: String?
    public let changeBasis: MarketPriceBasis?
    public let logoUrl: String?

    public var id: String { symbol }

    /// True when the drawn line and the reported day change are about different
    /// instruments.
    public var sparkAndChangeDisagreeOnInstrument: Bool {
        guard let sparkBasis, let changeBasis else { return false }
        return sparkBasis != changeBasis
    }

    public var logoURL: URL? {
        guard let logoUrl, !logoUrl.isEmpty else { return nil }
        return URL(string: logoUrl)
    }

    /// The mark in micros, for the figures that work in micros — the day-change
    /// pill's dollar face, for one. Derived from `markUsd` rather than carried as
    /// a second field, so the two cannot disagree.
    public var markUsdcMicros: Int64? {
        guard let decimal = SignedUsdFormatter.parse(markUsd), decimal > 0 else { return nil }
        var rounded = Decimal()
        var scaled = decimal * 1_000_000
        NSDecimalRound(&rounded, &scaled, 0, .plain)
        return (rounded as NSDecimalNumber).int64Value
    }

    public init(
        symbol: String,
        units: String,
        markUsd: String,
        valueUsd: String,
        dollarPnl: String,
        afterHours: Bool?,
        tokenAmount: String? = nil,
        change24h: String? = nil,
        sparkUsdcMicros: [Int64] = [],
        sparkBasis: MarketPriceBasis? = nil,
        sparkBasisSymbol: String? = nil,
        changeBasis: MarketPriceBasis? = nil,
        logoUrl: String? = nil
    ) {
        self.symbol = symbol
        self.units = units
        self.markUsd = markUsd
        self.valueUsd = valueUsd
        self.dollarPnl = dollarPnl
        self.afterHours = afterHours
        self.tokenAmount = tokenAmount
        self.change24h = change24h
        self.sparkUsdcMicros = sparkUsdcMicros
        self.sparkBasis = sparkBasis
        self.sparkBasisSymbol = sparkBasisSymbol
        self.changeBasis = changeBasis
        self.logoUrl = logoUrl
    }

    private enum CodingKeys: String, CodingKey {
        case symbol, units, markUsd, valueUsd, dollarPnl, afterHours, tokenAmount, change24h
        case sparkUsdcMicros = "spark"
        case sparkBasis, sparkBasisSymbol, changeBasis
        case logoUrl
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        symbol = try container.decode(String.self, forKey: .symbol)
        units = try container.decode(String.self, forKey: .units)
        markUsd = try container.decode(String.self, forKey: .markUsd)
        valueUsd = try container.decode(String.self, forKey: .valueUsd)
        dollarPnl = try container.decode(String.self, forKey: .dollarPnl)
        afterHours = try container.decodeIfPresent(Bool.self, forKey: .afterHours)
        tokenAmount = try container.decodeIfPresent(String.self, forKey: .tokenAmount)
        change24h = try container.decodeIfPresent(String.self, forKey: .change24h)
        sparkUsdcMicros = try container.decodeIfPresent([Int64].self, forKey: .sparkUsdcMicros) ?? []
        sparkBasis = try container.decodeIfPresent(MarketPriceBasis.self, forKey: .sparkBasis)
        sparkBasisSymbol = try container.decodeIfPresent(String.self, forKey: .sparkBasisSymbol)
        changeBasis = try container.decodeIfPresent(MarketPriceBasis.self, forKey: .changeBasis)
        logoUrl = try container.decodeIfPresent(String.self, forKey: .logoUrl)
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
    public let profilePhotoUrl: String?
    public let percentReturn: String?
    public let dollarPnl: String

    public var id: String { userId }

    public init(
        rank: Int,
        userId: String,
        displayName: String,
        profilePhotoUrl: String? = nil,
        percentReturn: String?,
        dollarPnl: String
    ) {
        self.rank = rank
        self.userId = userId
        self.displayName = displayName
        self.profilePhotoUrl = profilePhotoUrl
        self.percentReturn = percentReturn
        self.dollarPnl = dollarPnl
    }
}

public struct GroupAgentDTO: Codable, Equatable, Sendable {
    public let id: String
    public let status: String
    public let agentDisplayName: String
    public let allocationUsdcMicros: String
    /// Plaintext bot key for cabal members; omitted for spectators and revoked agents.
    public let apiKey: String?

    public init(
        id: String,
        status: String,
        agentDisplayName: String,
        allocationUsdcMicros: String,
        apiKey: String? = nil
    ) {
        self.id = id
        self.status = status
        self.agentDisplayName = agentDisplayName
        self.allocationUsdcMicros = allocationUsdcMicros
        self.apiKey = apiKey
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
