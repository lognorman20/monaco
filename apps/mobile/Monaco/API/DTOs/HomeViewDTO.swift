import Foundation

struct HomeViewDTO: Codable, Equatable {
    let groups: [HomeGroupBoardRowDTO]
    let people: [HomePeopleBoardRowDTO]
}

struct HomeGroupBoardRowDTO: Codable, Equatable, Identifiable {
    let groupId: String
    let name: String
    let potValueUsd: String
    let percentReturn: String?
    let dollarPnl: String
    let isJoined: Bool
    /// The cabal's picture; nil falls back to the tinted initials.
    var pictureUrl: String? = nil

    var id: String { groupId }
}

struct HomePeopleBoardRowDTO: Codable, Equatable, Identifiable {
    let userId: String
    let displayName: String
    var profilePhotoUrl: String? = nil
    let percentReturn: String?
    let dollarPnl: String

    var id: String { userId }
}

struct UserSharedGroupsResponse: Codable, Equatable {
    let groups: [HomeGroupBoardRowDTO]
}
