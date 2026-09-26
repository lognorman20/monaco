import Foundation

/// One cabal on the season table (`GET /v1/matchups/table`).
public struct MatchupTableRowDTO: Codable, Equatable, Sendable, Identifiable {
    public let rank: Int
    public let groupID: String
    public let name: String
    public let pictureUrl: String?
    public let wins: Int
    public let losses: Int
    public let ties: Int
    /// The sum of the cabal's weekly returns as a ratio string; the tiebreak after wins.
    public let points: String
    /// The current run: "W3", "L1", "T2". Nil before a first result.
    public let streak: String?
    /// One of the viewer's cabals.
    public let isMine: Bool

    public var id: String { groupID }

    public var record: MatchupRecordDTO { MatchupRecordDTO(wins: wins, losses: losses, ties: ties) }

    public init(
        rank: Int,
        groupID: String,
        name: String,
        pictureUrl: String? = nil,
        wins: Int,
        losses: Int,
        ties: Int,
        points: String,
        streak: String?,
        isMine: Bool
    ) {
        self.rank = rank
        self.groupID = groupID
        self.name = name
        self.pictureUrl = pictureUrl
        self.wins = wins
        self.losses = losses
        self.ties = ties
        self.points = points
        self.streak = streak
        self.isMine = isMine
    }

    enum CodingKeys: String, CodingKey {
        case rank
        case groupID = "groupId"
        case name
        case pictureUrl
        case wins
        case losses
        case ties
        case points
        case streak
        case isMine
    }
}

/// `GET /v1/matchups/table`.
public struct MatchupTableDTO: Codable, Equatable, Sendable {
    /// The last finished week the table counts; nil before the first week has finished.
    public let throughWeek: Date?
    /// Ranked rows. The viewer's cabals below the limit follow with their real rank.
    public let cabals: [MatchupTableRowDTO]

    public init(throughWeek: Date?, cabals: [MatchupTableRowDTO]) {
        self.throughWeek = throughWeek
        self.cabals = cabals
    }
}
