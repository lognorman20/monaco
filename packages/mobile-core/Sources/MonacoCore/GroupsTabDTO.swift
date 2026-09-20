import Foundation

// Contracts for the Groups tab read APIs (#148):
//   GET /v1/groups/search?q=&limit=&cursor=
//   GET /v1/groups/leaderboard?limit=
//   GET /v1/groups/pnl-history?range=           (one series per joined cabal)
//   GET /v1/groups/{id}/pnl-history?range=
// Money fields are USD decimal strings; dollarPnl carries an explicit sign.

/// Who may enter a cabal without an admin.
public enum GroupJoinMode: String, Codable, Equatable, Sendable {
    /// Anyone can join immediately.
    case open
    /// Joining sends a request the cabal admin approves.
    case request

    /// Unknown future modes decode as `.request` so the app never offers a
    /// one-tap join the server would refuse.
    public init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = GroupJoinMode(rawValue: raw) ?? .request
    }
}

/// Public row for a cabal in search results or on the platform leaderboard.
public struct GroupDiscoveryRowDTO: Codable, Equatable, Sendable, Identifiable {
    public let groupID: String
    public let name: String
    public let memberCount: Int
    public let potValueUsd: String
    /// Nil until the cabal has money in (no fake 0% rows).
    public let percentReturn: String?
    public let dollarPnl: String
    public let isJoined: Bool
    public let joinMode: GroupJoinMode

    public var id: String { groupID }

    public init(
        groupID: String,
        name: String,
        memberCount: Int,
        potValueUsd: String,
        percentReturn: String?,
        dollarPnl: String,
        isJoined: Bool,
        joinMode: GroupJoinMode
    ) {
        self.groupID = groupID
        self.name = name
        self.memberCount = memberCount
        self.potValueUsd = potValueUsd
        self.percentReturn = percentReturn
        self.dollarPnl = dollarPnl
        self.isJoined = isJoined
        self.joinMode = joinMode
    }

    enum CodingKeys: String, CodingKey {
        case groupID = "groupId"
        case name
        case memberCount
        case potValueUsd
        case percentReturn
        case dollarPnl
        case isJoined
        case joinMode
    }
}

public struct GroupSearchResponseDTO: Codable, Equatable, Sendable {
    public let groups: [GroupDiscoveryRowDTO]
    /// Opaque; pass back as `cursor` for the next page. Nil on the last page.
    public let nextCursor: String?

    public init(groups: [GroupDiscoveryRowDTO], nextCursor: String?) {
        self.groups = groups
        self.nextCursor = nextCursor
    }
}

public struct GroupLeaderboardRowDTO: Codable, Equatable, Sendable, Identifiable {
    public let rank: Int
    public let groupID: String
    public let name: String
    public let memberCount: Int
    public let potValueUsd: String
    public let percentReturn: String?
    public let dollarPnl: String
    public let isJoined: Bool
    public let joinMode: GroupJoinMode

    public var id: String { groupID }

    public init(
        rank: Int,
        groupID: String,
        name: String,
        memberCount: Int,
        potValueUsd: String,
        percentReturn: String?,
        dollarPnl: String,
        isJoined: Bool,
        joinMode: GroupJoinMode
    ) {
        self.rank = rank
        self.groupID = groupID
        self.name = name
        self.memberCount = memberCount
        self.potValueUsd = potValueUsd
        self.percentReturn = percentReturn
        self.dollarPnl = dollarPnl
        self.isJoined = isJoined
        self.joinMode = joinMode
    }

    enum CodingKeys: String, CodingKey {
        case rank
        case groupID = "groupId"
        case name
        case memberCount
        case potValueUsd
        case percentReturn
        case dollarPnl
        case isJoined
        case joinMode
    }
}

public struct GroupLeaderboardResponseDTO: Codable, Equatable, Sendable {
    public let groups: [GroupLeaderboardRowDTO]

    public init(groups: [GroupLeaderboardRowDTO]) {
        self.groups = groups
    }
}

/// Lookback window for P&L history. The server caps every range at 90 days.
public enum GroupPnLRange: String, Codable, CaseIterable, Sendable {
    case oneDay = "1D"
    case oneWeek = "1W"
    case oneMonth = "1M"
    case threeMonths = "3M"

    public var label: String { rawValue }
}

/// One sample: P&L = pot value - net money members have put in.
public struct GroupPnLPointDTO: Codable, Equatable, Sendable, Identifiable {
    public let at: Date
    public let potValueUsd: String
    public let netInUsd: String
    public let dollarPnl: String

    public var id: TimeInterval { at.timeIntervalSince1970 }

    /// Dollar P&L as a number for charting; 0 when unparseable.
    public var chartValue: Double {
        Double(dollarPnl.replacingOccurrences(of: "+", with: "")) ?? 0
    }

    public init(at: Date, potValueUsd: String, netInUsd: String, dollarPnl: String) {
        self.at = at
        self.potValueUsd = potValueUsd
        self.netInUsd = netInUsd
        self.dollarPnl = dollarPnl
    }
}

public struct GroupPnLSeriesDTO: Codable, Equatable, Sendable, Identifiable {
    public let groupID: String
    public let name: String
    public let range: String
    /// Oldest first, UTC timestamps. The last point is the live valuation.
    public let points: [GroupPnLPointDTO]

    public var id: String { groupID }

    public init(groupID: String, name: String, range: String, points: [GroupPnLPointDTO]) {
        self.groupID = groupID
        self.name = name
        self.range = range
        self.points = points
    }

    enum CodingKeys: String, CodingKey {
        case groupID = "groupId"
        case name
        case range
        case points
    }
}

public struct MyGroupsPnLHistoryDTO: Codable, Equatable, Sendable {
    public let range: String
    public let series: [GroupPnLSeriesDTO]

    public init(range: String, series: [GroupPnLSeriesDTO]) {
        self.range = range
        self.series = series
    }
}

/// Decides what a tap on a discovery row does.
public enum GroupDiscoveryDestination: Equatable, Sendable {
    /// Viewer is a member: open the cabal.
    case detail
    /// Open cabal: join immediately.
    case join
    /// Approval cabal: send a request to the admin.
    case requestToJoin

    public init(isJoined: Bool, joinMode: GroupJoinMode) {
        if isJoined {
            self = .detail
        } else {
            switch joinMode {
            case .open: self = .join
            case .request: self = .requestToJoin
            }
        }
    }
}

/// Chart helpers for the multi-cabal P&L chart.
public enum GroupPnLChartModel {
    /// Series with at least two points can draw a line; others are "sparse".
    public static func drawable(_ series: [GroupPnLSeriesDTO]) -> [GroupPnLSeriesDTO] {
        series.filter { $0.points.count >= 2 }
    }

}

/// Client-side mirror of the server's search query rules (2...64 characters).
public enum GroupSearchQuery {
    public static let minimumLength = 2
    public static let maximumLength = 64

    /// The query to send, or nil when it is too short to search.
    public static func normalized(_ raw: String) -> String? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.count >= minimumLength else { return nil }
        return String(trimmed.prefix(maximumLength))
    }
}

/// Formats a signed server dollar string ("+48.20", "-3.10") as "+$48.20" / "-$3.10".
public enum SignedUsdFormatter {
    /// "+$48.20" gains, "−$7.60" losses (U+2212), "$0.00" with no sign when the amount rounds to zero
    /// ("+0.00", "-0.00", "-0.001"). Unparseable input renders "—".
    /// Always pass the raw server string, never an already formatted one.
    public static func format(_ raw: String) -> String {
        guard let value = parse(raw) else { return "—" }
        let magnitude = value < 0 ? -value : value
        let body = UsdAmountFormatter.format(decimal: magnitude)
        if body == "$0.00" { return body }
        return (value < 0 ? typographicMinus : "+") + body
    }

    /// True when the amount is below zero after rounding to cents (drives loss styling).
    public static func isLoss(_ raw: String) -> Bool {
        guard let value = parse(raw), !isZero(raw) else { return false }
        return value < 0
    }

    /// True when the amount rounds to $0.00, including negative zero and dust. False for unparseable input.
    public static func isZero(_ raw: String) -> Bool {
        guard let value = parse(raw) else { return false }
        let magnitude = value < 0 ? -value : value
        return UsdAmountFormatter.format(decimal: magnitude) == "$0.00"
    }

    /// Signed decimal from a server string ("+48.2", "-0.001", "−7.60", "$3"). Nil when unparseable.
    public static func parse(_ raw: String) -> Decimal? {
        var trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
            .replacingOccurrences(of: typographicMinus, with: "-")
            .replacingOccurrences(of: ",", with: "")
        var negative = false
        if trimmed.hasPrefix("+") {
            trimmed.removeFirst()
        } else if trimmed.hasPrefix("-") {
            negative = true
            trimmed.removeFirst()
        }
        if trimmed.hasPrefix("$") { trimmed.removeFirst() }
        guard !trimmed.isEmpty,
              trimmed.allSatisfy({ $0.isASCII && ($0.isNumber || $0 == ".") }),
              let magnitude = Decimal(string: trimmed, locale: Locale(identifier: "en_US_POSIX"))
        else { return nil }
        return negative ? -magnitude : magnitude
    }
}
