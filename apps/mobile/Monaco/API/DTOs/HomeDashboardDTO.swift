import Foundation
import MonacoCore

struct HomeDashboardDTO: Codable, Equatable {
    let netWorthUsd: String
    let netWorthDollarPnl: String
    let netWorthPercentReturn: String?
    let myGroups: [HomeMyGroupRowDTO]
    let pnlSeries1H: [HomePnLSeriesPointDTO]
    let leaderboard: HomeLeaderboardSectionDTO
    let missedProposals: [HomeMissedProposalRowDTO]
}

struct HomeMyGroupRowDTO: Codable, Equatable, Identifiable {
    let groupId: String
    let name: String
    let equityUsd: String
    let slicePercent: String
    let dollarPnl: String
    let percentReturn: String?

    var id: String { groupId }
}

struct HomePnLSeriesPointDTO: Codable, Equatable, Identifiable {
    let ts: Date
    let equityUsd: String
    let dollarPnl: String

    var id: TimeInterval { ts.timeIntervalSince1970 }

    var chartValue: Double {
        let cleaned = dollarPnl.replacingOccurrences(of: "+", with: "")
        return Double(cleaned) ?? 0
    }
}

struct HomeLeaderboardSectionDTO: Codable, Equatable {
    let range: String
    let people: [HomePeopleBoardRowDTO]
}

struct HomeMissedProposalRowDTO: Codable, Equatable, Identifiable {
    let groupId: String
    let groupName: String
    let proposalId: String
    let symbol: String
    let status: String
    let createdAt: Date
    let expiresAt: Date

    var id: String { proposalId }
}

enum HomeLeaderboardRange: String, CaseIterable {
    case oneHour = "1H"
    case oneDay = "1D"
    case oneWeek = "1W"
    case oneMonth = "1M"
    case all = "ALL"

    var label: String {
        switch self {
        case .oneHour: "1H"
        case .oneDay: "1D"
        case .oneWeek: "1W"
        case .oneMonth: "1M"
        case .all: "All"
        }
    }
}

struct HomePnLSeriesDTO: Codable, Equatable {
    let points: [HomePnLSeriesPointDTO]
}

struct HomeMissedProposalsDTO: Codable, Equatable {
    let proposals: [HomeMissedProposalRowDTO]
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
