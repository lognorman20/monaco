import Foundation

/// One cabal's position in a symbol, as the Stocks tab shows it.
public struct HeldAssetCabalDTO: Codable, Equatable, Sendable, Identifiable {
    public let groupId: String
    public let name: String
    public let units: String
    public let valueUsd: String
    public let dollarPnl: String
    /// The viewer's own share of this position, in dollars. It is the figure that
    /// makes the section personal: the cabal owns the shares, the member owns a
    /// slice of them.
    public let mySliceUsd: String

    public var id: String { groupId }

    public init(
        groupId: String,
        name: String,
        units: String,
        valueUsd: String,
        dollarPnl: String,
        mySliceUsd: String
    ) {
        self.groupId = groupId
        self.name = name
        self.units = units
        self.valueUsd = valueUsd
        self.dollarPnl = dollarPnl
        self.mySliceUsd = mySliceUsd
    }
}

/// A stock at least one of the viewer's cabals holds.
public struct HeldAssetDTO: Codable, Equatable, Sendable, Identifiable {
    /// The market row, in the same shape the list routes serve, so it renders
    /// through the same row view — logo, sparkline and day-change pill included.
    public let asset: MarketAssetDTO
    public let cabals: [HeldAssetCabalDTO]
    public let totalValueUsd: String
    public let totalDollarPnl: String
    /// Every cabal's slice of this symbol added up: what the member's own stake in
    /// it is worth.
    public let mySliceUsd: String

    public var id: String { asset.symbol }

    public init(
        asset: MarketAssetDTO,
        cabals: [HeldAssetCabalDTO],
        totalValueUsd: String,
        totalDollarPnl: String,
        mySliceUsd: String
    ) {
        self.asset = asset
        self.cabals = cabals
        self.totalValueUsd = totalValueUsd
        self.totalDollarPnl = totalDollarPnl
        self.mySliceUsd = mySliceUsd
    }

    private enum CodingKeys: String, CodingKey {
        case asset, cabals, totalValueUsd, totalDollarPnl, mySliceUsd
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        asset = try container.decode(MarketAssetDTO.self, forKey: .asset)
        cabals = try container.decodeIfPresent([HeldAssetCabalDTO].self, forKey: .cabals) ?? []
        totalValueUsd = try container.decodeIfPresent(String.self, forKey: .totalValueUsd) ?? "0"
        totalDollarPnl = try container.decodeIfPresent(String.self, forKey: .totalDollarPnl) ?? "0"
        mySliceUsd = try container.decodeIfPresent(String.self, forKey: .mySliceUsd) ?? "0"
    }
}

/// A stock with an open proposal in one of the viewer's cabals.
public struct VotableAssetDTO: Codable, Equatable, Sendable, Identifiable {
    public let asset: MarketAssetDTO
    public let openProposals: Int
    /// The cabals a vote is open in, in the order the backend listed them.
    public let cabalNames: [String]
    /// When the first of those votes closes, in UTC.
    public let soonestExpiresAt: Date?

    public var id: String { asset.symbol }

    public init(
        asset: MarketAssetDTO,
        openProposals: Int,
        cabalNames: [String] = [],
        soonestExpiresAt: Date? = nil
    ) {
        self.asset = asset
        self.openProposals = openProposals
        self.cabalNames = cabalNames
        self.soonestExpiresAt = soonestExpiresAt
    }

    private enum CodingKeys: String, CodingKey {
        case asset, openProposals, cabalNames, soonestExpiresAt
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        asset = try container.decode(MarketAssetDTO.self, forKey: .asset)
        openProposals = try container.decodeIfPresent(Int.self, forKey: .openProposals) ?? 0
        cabalNames = try container.decodeIfPresent([String].self, forKey: .cabalNames) ?? []
        soonestExpiresAt = try container.decodeIfPresent(MonacoTimestamp.self, forKey: .soonestExpiresAt)?.date
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(asset, forKey: .asset)
        try container.encode(openProposals, forKey: .openProposals)
        try container.encode(cabalNames, forKey: .cabalNames)
        try container.encodeIfPresent(soonestExpiresAt.map(MonacoTimestamp.init(date:)), forKey: .soonestExpiresAt)
    }
}

/// `GET /v1/assets/held`: what the viewer's cabals own and what they are voting on.
///
/// One route for both sections because they answer the same question from the same
/// scan of the same cabals, and the Stocks tab wants them together or not at all.
public struct HeldAssetsResponseDTO: Codable, Equatable, Sendable {
    public let held: [HeldAssetDTO]
    public let upForVote: [VotableAssetDTO]
    public let market: MarketStatusDTO?

    public init(
        held: [HeldAssetDTO],
        upForVote: [VotableAssetDTO],
        market: MarketStatusDTO? = nil
    ) {
        self.held = held
        self.upForVote = upForVote
        self.market = market
    }

    private enum CodingKeys: String, CodingKey {
        case held, upForVote, market
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        held = try container.decodeIfPresent([HeldAssetDTO].self, forKey: .held) ?? []
        upForVote = try container.decodeIfPresent([VotableAssetDTO].self, forKey: .upForVote) ?? []
        // Same rule the market list and the detail follow: the session chip is
        // decoration carrying two timestamps that throw on any spelling the shared
        // ISO8601 parser rejects. This route feeds two of the Stocks tab's four
        // sections, and losing both because a chip would not parse is not a trade
        // anyone would make.
        market = (try? container.decodeIfPresent(MarketStatusDTO.self, forKey: .market)) ?? nil
    }
}

/// The one line under a "In your cabals" or "Up for vote" row.
public enum CabalStockCopy {
    /// "Weekend investors · your slice $48.20" for one cabal, "2 cabals · your
    /// slice $48.20" for more. The slice is dropped when it rounds to nothing,
    /// because "your slice $0.00" reads as a bug rather than as a small position.
    public static func heldLine(cabals: [HeldAssetCabalDTO], mySliceUsd: String) -> String {
        var parts: [String] = []
        switch cabals.count {
        case 0: break
        case 1: parts.append(cabals[0].name)
        default: parts.append("\(cabals.count) cabals")
        }
        if !SignedUsdFormatter.isZero(mySliceUsd), SignedUsdFormatter.parse(mySliceUsd) != nil {
            parts.append("your slice \(UsdAmountFormatter.format(decimalString: mySliceUsd))")
        }
        return parts.joined(separator: " · ")
    }

    /// "1 open vote · Weekend investors" / "3 open votes". The cabal is named only
    /// when there is exactly one, since a list of three names does not fit a row.
    public static func voteLine(openProposals: Int, cabalNames: [String]) -> String {
        let count = max(openProposals, 0)
        var parts = ["\(count) open \(count == 1 ? "vote" : "votes")"]
        if cabalNames.count == 1, let name = cabalNames.first, !name.isEmpty {
            parts.append(name)
        }
        return parts.joined(separator: " · ")
    }

    /// VoiceOver reads the whole row, so the vote row needs its deadline spoken as
    /// well as shown: "closes in 4 hours". Nil when there is no deadline to speak.
    public static func voteDeadline(_ expiresAt: Date?, now: Date) -> String? {
        guard let expiresAt else { return nil }
        let seconds = expiresAt.timeIntervalSince(now)
        if seconds <= 0 { return "closing now" }
        if seconds < 3600 {
            let minutes = max(Int((seconds / 60).rounded(.down)), 1)
            return "closes in \(minutes) \(minutes == 1 ? "minute" : "minutes")"
        }
        if seconds < 86_400 {
            let hours = max(Int((seconds / 3600).rounded(.down)), 1)
            return "closes in \(hours) \(hours == 1 ? "hour" : "hours")"
        }
        let days = max(Int((seconds / 86_400).rounded(.down)), 1)
        return "closes in \(days) \(days == 1 ? "day" : "days")"
    }
}
