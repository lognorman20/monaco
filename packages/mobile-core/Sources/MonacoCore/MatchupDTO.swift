import Foundation

// Contracts for weekly matchups:
//   GET  /v1/groups/{id}/matchup
//   GET  /v1/home/matchups
//   POST /v1/groups/{id}/matchups/challenge
//   POST /v1/groups/{id}/matchups/challenges/{challengeId}/accept
// Scores are the week's return as a ratio string ("0.0123" is +1.23%), the same shape as
// `percentReturn` everywhere else. Side `a` is always the cabal the request was about.

/// A cabal's public face on a matchup surface.
public struct MatchupFaceDTO: Codable, Equatable, Sendable, Identifiable {
    public let groupID: String
    public let name: String
    /// Nil when the cabal has no picture; its mark falls back to tinted initials.
    public let pictureUrl: String?
    public let memberCount: Int

    public var id: String { groupID }

    public init(groupID: String, name: String, pictureUrl: String? = nil, memberCount: Int) {
        self.groupID = groupID
        self.name = name
        self.pictureUrl = pictureUrl
        self.memberCount = memberCount
    }

    enum CodingKeys: String, CodingKey {
        case groupID = "groupId"
        case name
        case pictureUrl
        case memberCount
    }
}

/// One cabal in a live matchup, with its return for the week so far.
public struct MatchupSideDTO: Codable, Equatable, Sendable, Identifiable {
    public let groupID: String
    public let name: String
    public let pictureUrl: String?
    public let memberCount: Int
    /// Nil on a bye.
    public let score: String?

    public var id: String { groupID }

    public init(groupID: String, name: String, pictureUrl: String? = nil, memberCount: Int, score: String?) {
        self.groupID = groupID
        self.name = name
        self.pictureUrl = pictureUrl
        self.memberCount = memberCount
        self.score = score
    }

    enum CodingKeys: String, CodingKey {
        case groupID = "groupId"
        case name
        case pictureUrl
        case memberCount
        case score
    }
}

/// Who is ahead, from side a's point of view.
public enum MatchupLeader: String, Codable, Equatable, Sendable {
    case a
    case b
    case tie
}

/// This week's matchup for one cabal.
public struct MatchupDTO: Codable, Equatable, Sendable, Identifiable {
    public let id: String
    public let weekStart: Date
    /// When the week ends and the result freezes: the next Monday 00:00 UTC.
    public let weekEnd: Date
    public let daysLeft: Int
    public let a: MatchupSideDTO
    /// Nil on a bye.
    public let b: MatchupSideDTO?
    /// Nil on a bye, and for a value this build does not know.
    public let leading: MatchupLeader?
    /// The pair came from a friendly challenge rather than the draw.
    public let fromChallenge: Bool

    public var isBye: Bool { b == nil }

    public init(
        id: String,
        weekStart: Date,
        weekEnd: Date,
        daysLeft: Int,
        a: MatchupSideDTO,
        b: MatchupSideDTO?,
        leading: MatchupLeader?,
        fromChallenge: Bool = false
    ) {
        self.id = id
        self.weekStart = weekStart
        self.weekEnd = weekEnd
        self.daysLeft = daysLeft
        self.a = a
        self.b = b
        self.leading = leading
        self.fromChallenge = fromChallenge
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        weekStart = try container.decode(Date.self, forKey: .weekStart)
        weekEnd = try container.decode(Date.self, forKey: .weekEnd)
        daysLeft = try container.decode(Int.self, forKey: .daysLeft)
        a = try container.decode(MatchupSideDTO.self, forKey: .a)
        b = try container.decodeIfPresent(MatchupSideDTO.self, forKey: .b)
        leading = (try? container.decodeIfPresent(String.self, forKey: .leading)).flatMap { $0.flatMap(MatchupLeader.init(rawValue:)) }
        fromChallenge = try container.decodeIfPresent(Bool.self, forKey: .fromChallenge) ?? false
    }
}

/// A cabal's season record over frozen weeks.
public struct MatchupRecordDTO: Codable, Equatable, Sendable {
    public let wins: Int
    public let losses: Int
    public let ties: Int

    public init(wins: Int, losses: Int, ties: Int) {
        self.wins = wins
        self.losses = losses
        self.ties = ties
    }

    public static let empty = MatchupRecordDTO(wins: 0, losses: 0, ties: 0)

    public var hasPlayed: Bool { wins + losses + ties > 0 }
}

/// How a finished week went for one cabal.
public enum MatchupOutcome: String, Codable, Equatable, Sendable {
    case win
    case loss
    case tie
    case bye
}

/// One finished week from a cabal's side.
public struct MatchupResultDTO: Codable, Equatable, Sendable, Identifiable {
    public let id: String
    public let weekStart: Date
    /// Nil for a value this build does not know.
    public let result: MatchupOutcome?
    /// Nil on a bye.
    public let opponent: MatchupFaceDTO?
    public let score: String?
    public let opponentScore: String?
    public let winner: String?

    public init(
        id: String,
        weekStart: Date,
        result: MatchupOutcome?,
        opponent: MatchupFaceDTO?,
        score: String?,
        opponentScore: String?,
        winner: String? = nil
    ) {
        self.id = id
        self.weekStart = weekStart
        self.result = result
        self.opponent = opponent
        self.score = score
        self.opponentScore = opponentScore
        self.winner = winner
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        weekStart = try container.decode(Date.self, forKey: .weekStart)
        result = (try? container.decodeIfPresent(String.self, forKey: .result)).flatMap { $0.flatMap(MatchupOutcome.init(rawValue:)) }
        opponent = try container.decodeIfPresent(MatchupFaceDTO.self, forKey: .opponent)
        score = try container.decodeIfPresent(String.self, forKey: .score)
        opponentScore = try container.decodeIfPresent(String.self, forKey: .opponentScore)
        winner = try container.decodeIfPresent(String.self, forKey: .winner)
    }
}

/// A friendly challenge for next week, from one cabal's side.
public struct MatchupChallengeDTO: Codable, Equatable, Sendable, Identifiable {
    public enum Direction: String, Codable, Sendable {
        /// Sent to this cabal; a member can accept it.
        case incoming
        /// Sent by this cabal; waiting on the other one.
        case outgoing
    }

    public enum Status: String, Codable, Sendable {
        case pending
        case accepted
        case scheduled
        case void
        case expired
    }

    public let id: String
    public let weekStart: Date
    public let direction: Direction
    public let status: Status
    public let opponent: MatchupFaceDTO
    public let createdAt: Date
    public let acceptedAt: Date?

    public init(
        id: String,
        weekStart: Date,
        direction: Direction,
        status: Status,
        opponent: MatchupFaceDTO,
        createdAt: Date,
        acceptedAt: Date? = nil
    ) {
        self.id = id
        self.weekStart = weekStart
        self.direction = direction
        self.status = status
        self.opponent = opponent
        self.createdAt = createdAt
        self.acceptedAt = acceptedAt
    }

    public var canAccept: Bool { direction == .incoming && status == .pending }

    /// A status or direction this build does not know reads as closed and outgoing, so the app
    /// never offers to accept something it does not understand.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        weekStart = try container.decode(Date.self, forKey: .weekStart)
        direction = Direction(rawValue: try container.decode(String.self, forKey: .direction)) ?? .outgoing
        status = Status(rawValue: try container.decode(String.self, forKey: .status)) ?? .expired
        opponent = try container.decode(MatchupFaceDTO.self, forKey: .opponent)
        createdAt = try container.decode(Date.self, forKey: .createdAt)
        acceptedAt = try container.decodeIfPresent(Date.self, forKey: .acceptedAt)
    }
}

/// `GET /v1/groups/{id}/matchup`.
public struct GroupMatchupDTO: Codable, Equatable, Sendable {
    /// The next Monday 00:00 UTC: when this week ends and the next is drawn.
    public let nextDrawAt: Date
    /// Nil when the cabal is not in this week's draw.
    public let current: MatchupDTO?
    public let record: MatchupRecordDTO
    public let recent: [MatchupResultDTO]
    public let challenges: [MatchupChallengeDTO]
    /// Members can send and accept challenges.
    public let canChallenge: Bool

    public init(
        nextDrawAt: Date,
        current: MatchupDTO?,
        record: MatchupRecordDTO,
        recent: [MatchupResultDTO],
        challenges: [MatchupChallengeDTO],
        canChallenge: Bool
    ) {
        self.nextDrawAt = nextDrawAt
        self.current = current
        self.record = record
        self.recent = recent
        self.challenges = challenges
        self.canChallenge = canChallenge
    }

    /// The accepted challenge that fixes next week's opponent, if there is one.
    public var nextOpponent: MatchupChallengeDTO? {
        challenges.first { $0.status == .accepted }
    }
}

/// `GET /v1/home/matchups`.
public struct HomeMatchupsDTO: Codable, Equatable, Sendable {
    public let weekStart: Date
    public let weekEnd: Date
    public let daysLeft: Int
    public let nextDrawAt: Date
    /// False in the minutes between Monday 00:00 UTC and the week's draw landing.
    public let drawn: Bool
    public let hasCabals: Bool
    public let matchups: [MatchupDTO]

    public init(
        weekStart: Date,
        weekEnd: Date,
        daysLeft: Int,
        nextDrawAt: Date,
        drawn: Bool,
        hasCabals: Bool,
        matchups: [MatchupDTO]
    ) {
        self.weekStart = weekStart
        self.weekEnd = weekEnd
        self.daysLeft = daysLeft
        self.nextDrawAt = nextDrawAt
        self.drawn = drawn
        self.hasCabals = hasCabals
        self.matchups = matchups
    }
}
