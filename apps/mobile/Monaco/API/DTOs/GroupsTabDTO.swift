import Foundation
import MonacoCore

// The shared package owns the wire schema; aliases preserve native call sites.
typealias GroupSearchResponse = MonacoCore.GroupSearchResponseDTO
typealias GroupDiscoveryRow = MonacoCore.GroupDiscoveryRowDTO
typealias GroupLeaderboardResponse = MonacoCore.GroupLeaderboardResponseDTO
typealias GroupLeaderboardRow = MonacoCore.GroupLeaderboardRowDTO
typealias GroupPnLHistoryResponse = MonacoCore.GroupPnLHistoryDTO
typealias GroupPnLHistoryPoint = MonacoCore.GroupPnLHistoryPointDTO

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
