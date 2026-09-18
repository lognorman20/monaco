import Foundation

public struct GroupSearchResponseDTO: Codable, Equatable, Sendable {
    public let groups: [GroupDiscoveryRowDTO]

    public init(groups: [GroupDiscoveryRowDTO]) {
        self.groups = groups
    }
}

public struct GroupDiscoveryRowDTO: Codable, Equatable, Sendable, Identifiable {
    public let groupID: String
    public var groupId: String { groupID }
    public let name: String
    public let potValueUsd: String
    public let percentReturn: String?
    public let dollarPnl: String
    public let isJoined: Bool
    public let joinMode: String

    public var id: String { groupID }

    public init(
        groupID: String,
        name: String,
        potValueUsd: String,
        percentReturn: String?,
        dollarPnl: String,
        isJoined: Bool,
        joinMode: String
    ) {
        self.groupID = groupID
        self.name = name
        self.potValueUsd = potValueUsd
        self.percentReturn = percentReturn
        self.dollarPnl = dollarPnl
        self.isJoined = isJoined
        self.joinMode = joinMode
    }

    enum CodingKeys: String, CodingKey {
        case groupID = "groupId"
        case name
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

public struct GroupLeaderboardRowDTO: Codable, Equatable, Sendable, Identifiable {
    public let rank: Int
    public let groupID: String
    public var groupId: String { groupID }
    public let name: String
    public let potValueUsd: String
    public let percentReturn: String?
    public let dollarPnl: String
    public let isJoined: Bool

    public var id: String { groupID }

    public init(
        rank: Int,
        groupID: String,
        name: String,
        potValueUsd: String,
        percentReturn: String?,
        dollarPnl: String,
        isJoined: Bool
    ) {
        self.rank = rank
        self.groupID = groupID
        self.name = name
        self.potValueUsd = potValueUsd
        self.percentReturn = percentReturn
        self.dollarPnl = dollarPnl
        self.isJoined = isJoined
    }

    enum CodingKeys: String, CodingKey {
        case rank
        case groupID = "groupId"
        case name
        case potValueUsd
        case percentReturn
        case dollarPnl
        case isJoined
    }
}

public struct GroupPnLHistoryDTO: Codable, Equatable, Sendable {
    public let groupID: String
    public var groupId: String { groupID }
    public let name: String
    public let points: [GroupPnLHistoryPointDTO]

    public init(groupID: String, name: String, points: [GroupPnLHistoryPointDTO]) {
        self.groupID = groupID
        self.name = name
        self.points = points
    }

    enum CodingKeys: String, CodingKey {
        case groupID = "groupId"
        case name
        case points
    }
}

public struct GroupPnLHistoryPointDTO: Codable, Equatable, Sendable, Identifiable {
    public let at: String
    public let potValueUsd: String
    public let dollarPnl: String

    public var id: String { at }

    public init(at: String, potValueUsd: String, dollarPnl: String) {
        self.at = at
        self.potValueUsd = potValueUsd
        self.dollarPnl = dollarPnl
    }

    enum CodingKeys: String, CodingKey {
        case at
        case potValueUsd
        case dollarPnl
    }
}
