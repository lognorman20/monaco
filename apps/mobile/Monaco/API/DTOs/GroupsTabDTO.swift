import Foundation

struct GroupSearchResponse: Codable, Equatable {
    let groups: [GroupDiscoveryRow]
}

struct GroupDiscoveryRow: Codable, Equatable, Identifiable {
    let groupId: String
    let name: String
    let potValueUsd: String
    let percentReturn: String?
    let dollarPnl: String
    let isJoined: Bool
    let joinMode: String

    var id: String { groupId }
}

struct GroupLeaderboardResponse: Codable, Equatable {
    let groups: [GroupLeaderboardRow]
}

struct GroupLeaderboardRow: Codable, Equatable, Identifiable {
    let rank: Int
    let groupId: String
    let name: String
    let potValueUsd: String
    let percentReturn: String?
    let dollarPnl: String
    let isJoined: Bool

    var id: String { groupId }
}

struct GroupPnLHistoryResponse: Codable, Equatable {
    let groupId: String
    let name: String
    let points: [GroupPnLHistoryPoint]
}

struct GroupPnLHistoryPoint: Codable, Equatable, Identifiable {
    let at: String
    let potValueUsd: String
    let dollarPnl: String

    var id: String { at }
}

struct GroupPnLChartSeries: Identifiable, Equatable {
    let groupId: String
    let name: String
    let points: [GroupPnLChartPoint]

    var id: String { groupId }
}

struct GroupPnLChartPoint: Identifiable, Equatable {
    let date: Date
    let potValueUsd: Double
    let groupId: String

    var id: String { "\(groupId)-\(date.timeIntervalSince1970)" }
}
