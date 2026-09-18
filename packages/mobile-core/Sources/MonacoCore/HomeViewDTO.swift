import Foundation

public struct HomeViewDTO: Codable, Equatable, Sendable {
    public let groups: [HomeGroupBoardRowDTO]
    public let people: [HomePeopleBoardRowDTO]

    public init(groups: [HomeGroupBoardRowDTO], people: [HomePeopleBoardRowDTO]) {
        self.groups = groups
        self.people = people
    }
}

public struct HomeGroupBoardRowDTO: Codable, Equatable, Sendable {
    public let groupID: String
    public let name: String
    public let potValueUsd: String
    public let percentReturn: String?
    public let dollarPnl: String
    public let isJoined: Bool

    public init(
        groupID: String,
        name: String,
        potValueUsd: String,
        percentReturn: String?,
        dollarPnl: String,
        isJoined: Bool
    ) {
        self.groupID = groupID
        self.name = name
        self.potValueUsd = potValueUsd
        self.percentReturn = percentReturn
        self.dollarPnl = dollarPnl
        self.isJoined = isJoined
    }

    enum CodingKeys: String, CodingKey {
        case groupID = "groupId"
        case name
        case potValueUsd
        case percentReturn
        case dollarPnl
        case isJoined
    }
}

public struct HomePeopleBoardRowDTO: Codable, Equatable, Sendable {
    public let userID: String
    public let displayName: String
    public let percentReturn: String?
    public let dollarPnl: String

    public init(userID: String, displayName: String, percentReturn: String?, dollarPnl: String) {
        self.userID = userID
        self.displayName = displayName
        self.percentReturn = percentReturn
        self.dollarPnl = dollarPnl
    }

    enum CodingKeys: String, CodingKey {
        case userID = "userId"
        case displayName
        case percentReturn
        case dollarPnl
    }
}
