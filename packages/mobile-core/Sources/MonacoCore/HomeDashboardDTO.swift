import Foundation

public struct HomeDashboardDTO: Codable, Equatable, Sendable {
    public let netWorthUsd: String
    public let netWorthDollarPnl: String
    public let netWorthPercentReturn: String?
    public let myGroups: [HomeMyGroupRowDTO]
    public let pnlSeries1H: [HomePnLSeriesPointDTO]
    public let leaderboard: HomeLeaderboardSectionDTO
    public let missedProposals: [HomeMissedProposalRowDTO]

    public init(
        netWorthUsd: String,
        netWorthDollarPnl: String,
        netWorthPercentReturn: String?,
        myGroups: [HomeMyGroupRowDTO],
        pnlSeries1H: [HomePnLSeriesPointDTO],
        leaderboard: HomeLeaderboardSectionDTO,
        missedProposals: [HomeMissedProposalRowDTO]
    ) {
        self.netWorthUsd = netWorthUsd
        self.netWorthDollarPnl = netWorthDollarPnl
        self.netWorthPercentReturn = netWorthPercentReturn
        self.myGroups = myGroups
        self.pnlSeries1H = pnlSeries1H
        self.leaderboard = leaderboard
        self.missedProposals = missedProposals
    }
}

public struct HomeMyGroupRowDTO: Codable, Equatable, Sendable, Identifiable {
    public let groupID: String
    public let name: String
    public let equityUsd: String
    public let slicePercent: String
    public let dollarPnl: String
    public let percentReturn: String?

    public var id: String { groupID }

    public init(
        groupID: String,
        name: String,
        equityUsd: String,
        slicePercent: String,
        dollarPnl: String,
        percentReturn: String?
    ) {
        self.groupID = groupID
        self.name = name
        self.equityUsd = equityUsd
        self.slicePercent = slicePercent
        self.dollarPnl = dollarPnl
        self.percentReturn = percentReturn
    }

    enum CodingKeys: String, CodingKey {
        case groupID = "groupId"
        case name
        case equityUsd
        case slicePercent
        case dollarPnl
        case percentReturn
    }
}

public struct HomePnLSeriesPointDTO: Codable, Equatable, Sendable, Identifiable {
    public let ts: Date
    public let equityUsd: String
    public let dollarPnl: String

    public var id: TimeInterval { ts.timeIntervalSince1970 }

    public var chartValue: Double {
        let cleaned = dollarPnl.replacingOccurrences(of: "+", with: "")
        return Double(cleaned) ?? 0
    }

    public init(ts: Date, equityUsd: String, dollarPnl: String) {
        self.ts = ts
        self.equityUsd = equityUsd
        self.dollarPnl = dollarPnl
    }
}

public struct HomeLeaderboardSectionDTO: Codable, Equatable, Sendable {
    public let range: String
    public let people: [HomePeopleBoardRowDTO]

    public init(range: String, people: [HomePeopleBoardRowDTO]) {
        self.range = range
        self.people = people
    }
}

public struct HomeMissedProposalRowDTO: Codable, Equatable, Sendable, Identifiable {
    public let groupID: String
    public let groupName: String
    public let proposalID: String
    public let symbol: String
    public let status: String
    public let createdAt: Date
    public let expiresAt: Date

    public var id: String { proposalID }

    public init(
        groupID: String,
        groupName: String,
        proposalID: String,
        symbol: String,
        status: String,
        createdAt: Date,
        expiresAt: Date
    ) {
        self.groupID = groupID
        self.groupName = groupName
        self.proposalID = proposalID
        self.symbol = symbol
        self.status = status
        self.createdAt = createdAt
        self.expiresAt = expiresAt
    }

    enum CodingKeys: String, CodingKey {
        case groupID = "groupId"
        case groupName
        case proposalID = "proposalId"
        case symbol
        case status
        case createdAt
        case expiresAt
    }
}

public enum HomeLeaderboardRange: String, CaseIterable, Sendable {
    case oneHour = "1H"
    case oneDay = "1D"
    case oneWeek = "1W"
    case oneMonth = "1M"
    case all = "ALL"

    public var label: String {
        switch self {
        case .oneHour: "1H"
        case .oneDay: "1D"
        case .oneWeek: "1W"
        case .oneMonth: "1M"
        case .all: "All"
        }
    }
}

public struct HomePnLSeriesDTO: Codable, Equatable, Sendable {
    public let points: [HomePnLSeriesPointDTO]

    public init(points: [HomePnLSeriesPointDTO]) {
        self.points = points
    }
}

public struct HomeMissedProposalsDTO: Codable, Equatable, Sendable {
    public let proposals: [HomeMissedProposalRowDTO]

    public init(proposals: [HomeMissedProposalRowDTO]) {
        self.proposals = proposals
    }
}

func monacoISO8601JSONDecoder() -> JSONDecoder {
    let decoder = JSONDecoder()
    decoder.dateDecodingStrategy = .custom { decoder in
        let container = try decoder.singleValueContainer()
        let raw = try container.decode(String.self)
        if let date = SharedFormatters.iso8601Date(from: raw) {
            return date
        }
        throw DecodingError.dataCorruptedError(
            in: container,
            debugDescription: "Expected ISO8601 date, got \(raw)"
        )
    }
    return decoder
}
