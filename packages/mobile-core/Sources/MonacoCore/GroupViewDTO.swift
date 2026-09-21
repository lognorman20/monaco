import Foundation

public struct PotRowDTO: Codable, Equatable, Sendable, Identifiable {
    public let symbol: String
    public let units: String
    public let markUsd: String
    public let valueUsd: String
    public let dollarPnl: String
    public let afterHours: Bool?
    public let tokenAmount: String?
    /// The underlying's day move, exactly as the market list rows report it: Pyth's
    /// latest price against the previous regular-session close. Distinct from
    /// `dollarPnl`, which is what this cabal has made on the position since it
    /// bought; the two answer different questions and the row shows both. Render it
    /// through `stockDayMove`, which carries the label.
    public let change24h: String?
    /// Which instrument `change24h` is about (`underlying`), and its symbol ("AAPL").
    public let change24hBasis: MarketPriceBasis?
    public let change24hBasisSymbol: String?
    /// The day's closes for the row's sparkline, in USDC micros, oldest first.
    /// Empty when the backend had no series; the row then draws none.
    public let sparkUsdcMicros: [Int64]
    /// Which instrument the line is about. A line is tinted by the day move only
    /// when both are the same instrument.
    public let sparkBasis: MarketPriceBasis?
    public let sparkBasisSymbol: String?
    /// Kept on the wire; the backend sends none for a B20 token.
    public let logoUrl: String?

    public var id: String { symbol }

    /// The day move with its label, or nil when the backend did not say whose move
    /// it is.
    public var stockDayMove: StockDayMove? {
        StockDayMove(ratio: change24h, basis: change24hBasis, basisSymbol: change24hBasisSymbol)
    }

    /// True when the drawn line and the reported day change are about different
    /// instruments.
    public var sparkAndChangeDisagreeOnInstrument: Bool {
        MarketRowBasis.disagree(spark: sparkBasis, change: change24hBasis)
    }

    /// The issuer's logo for this token, or nil for the ticker tile.
    public var logoURL: URL? { StockLogoURL.parse(logoUrl) }

    /// The price the day pill's dollar face is measured on: the underlying's latest
    /// close from this row's line. Never `markUsd`, which is the token's price and
    /// carries its multiplier.
    public var dayMoveReferencePriceUsdcMicros: Int64? {
        DayChangeFigures.referencePrice(sparkUsdcMicros: sparkUsdcMicros, sparkBasis: sparkBasis, dayMove: stockDayMove)
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
        change24hBasis: MarketPriceBasis? = nil,
        change24hBasisSymbol: String? = nil,
        sparkUsdcMicros: [Int64] = [],
        sparkBasis: MarketPriceBasis? = nil,
        sparkBasisSymbol: String? = nil,
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
        self.change24hBasis = change24hBasis
        self.change24hBasisSymbol = change24hBasisSymbol
        self.sparkUsdcMicros = sparkUsdcMicros
        self.sparkBasis = sparkBasis
        self.sparkBasisSymbol = sparkBasisSymbol
        self.logoUrl = logoUrl
    }

    private enum CodingKeys: String, CodingKey {
        case symbol, units, markUsd, valueUsd, dollarPnl, afterHours, tokenAmount
        case change24h, change24hBasis, change24hBasisSymbol
        case sparkUsdcMicros = "spark"
        case sparkBasis, sparkBasisSymbol
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
        change24hBasis = try container.decodeIfPresent(MarketPriceBasis.self, forKey: .change24hBasis)
        change24hBasisSymbol = try container.decodeIfPresent(String.self, forKey: .change24hBasisSymbol)
        sparkUsdcMicros = try container.decodeIfPresent([Int64].self, forKey: .sparkUsdcMicros) ?? []
        sparkBasis = try container.decodeIfPresent(MarketPriceBasis.self, forKey: .sparkBasis)
        sparkBasisSymbol = try container.decodeIfPresent(String.self, forKey: .sparkBasisSymbol)
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
