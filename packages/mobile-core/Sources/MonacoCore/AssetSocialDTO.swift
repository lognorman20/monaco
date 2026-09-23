import Foundation

/// One cabal's position in a single stock, as `GET /v1/assets/{symbol}/social`
/// reports it.
///
/// Money is a decimal string in the same shape every other Monaco screen uses, so
/// the app has one number format and one P&L component rather than a second set for
/// this screen.
public struct AssetHoldingDTO: Codable, Equatable, Sendable, Identifiable {
    public let groupId: String
    public let name: String
    /// Whole tokens ("12.5").
    public let units: String
    /// The same figure in atomic units, for anything that needs to do arithmetic.
    public let tokenAmount: String
    public let markUsd: String
    public let valueUsd: String
    /// What the cabal paid for the units it still holds.
    public let costBasisUsd: String
    public let dollarPnl: String
    /// Nil when there is no cost basis to measure a return against — a migrated or
    /// airdropped position has a value but no percentage, and showing one would
    /// invent a 100% gain.
    public let percentReturn: String?
    /// The viewer's own share of `valueUsd`. This is the figure that makes the card
    /// personal rather than a company report.
    public let mySliceUsd: String
    public let mySlicePercent: String
    /// True when this holding was marked with a frozen equity price.
    public let afterHours: Bool

    public var id: String { groupId }

    public init(
        groupId: String,
        name: String,
        units: String,
        tokenAmount: String = "0",
        markUsd: String = "0.00",
        valueUsd: String,
        costBasisUsd: String,
        dollarPnl: String,
        percentReturn: String? = nil,
        mySliceUsd: String = "0.00",
        mySlicePercent: String = "0",
        afterHours: Bool = false
    ) {
        self.groupId = groupId
        self.name = name
        self.units = units
        self.tokenAmount = tokenAmount
        self.markUsd = markUsd
        self.valueUsd = valueUsd
        self.costBasisUsd = costBasisUsd
        self.dollarPnl = dollarPnl
        self.percentReturn = percentReturn
        self.mySliceUsd = mySliceUsd
        self.mySlicePercent = mySlicePercent
        self.afterHours = afterHours
    }

    private enum CodingKeys: String, CodingKey {
        case groupId, name, units, tokenAmount, markUsd, valueUsd, costBasisUsd
        case dollarPnl, percentReturn, mySliceUsd, mySlicePercent, afterHours
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        groupId = try container.decode(String.self, forKey: .groupId)
        name = try container.decode(String.self, forKey: .name)
        units = try container.decodeIfPresent(String.self, forKey: .units) ?? "0"
        tokenAmount = try container.decodeIfPresent(String.self, forKey: .tokenAmount) ?? "0"
        markUsd = try container.decodeIfPresent(String.self, forKey: .markUsd) ?? "0.00"
        valueUsd = try container.decodeIfPresent(String.self, forKey: .valueUsd) ?? "0.00"
        costBasisUsd = try container.decodeIfPresent(String.self, forKey: .costBasisUsd) ?? "0.00"
        dollarPnl = try container.decodeIfPresent(String.self, forKey: .dollarPnl) ?? "0.00"
        percentReturn = try container.decodeIfPresent(String.self, forKey: .percentReturn)
        mySliceUsd = try container.decodeIfPresent(String.self, forKey: .mySliceUsd) ?? "0.00"
        mySlicePercent = try container.decodeIfPresent(String.self, forKey: .mySlicePercent) ?? "0"
        afterHours = try container.decodeIfPresent(Bool.self, forKey: .afterHours) ?? false
    }
}

/// Which way a member voted. `unknown` keeps a choice this build does not know from
/// failing the whole response.
public enum AssetVoteChoice: String, Codable, Sendable {
    case yes
    case no
    case unknown

    public init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = AssetVoteChoice(rawValue: raw) ?? .unknown
    }
}

/// One ballot, with enough identity to draw the voter's avatar.
public struct AssetVoterDTO: Codable, Equatable, Sendable, Identifiable {
    public let userId: String
    public let displayName: String
    public let profilePhotoUrl: String?
    public let choice: AssetVoteChoice

    public var id: String { userId }

    public init(userId: String, displayName: String, profilePhotoUrl: String? = nil, choice: AssetVoteChoice) {
        self.userId = userId
        self.displayName = displayName
        self.profilePhotoUrl = profilePhotoUrl
        self.choice = choice
    }

    private enum CodingKeys: String, CodingKey {
        case userId, displayName, profilePhotoUrl, choice
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        userId = try container.decode(String.self, forKey: .userId)
        displayName = try container.decodeIfPresent(String.self, forKey: .displayName) ?? ""
        // An empty string and an absent key are the same absence of a photo.
        let photo = try container.decodeIfPresent(String.self, forKey: .profilePhotoUrl)
        profilePhotoUrl = (photo?.isEmpty ?? true) ? nil : photo
        choice = try container.decodeIfPresent(AssetVoteChoice.self, forKey: .choice) ?? .unknown
    }
}

public enum AssetProposalKind: String, Codable, Sendable {
    case buy
    case sell
    case unknown

    public init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = AssetProposalKind(rawValue: raw) ?? .unknown
    }
}

/// An open vote about this stock in one of the viewer's cabals.
public struct AssetProposalDTO: Codable, Equatable, Sendable, Identifiable {
    public let id: String
    public let groupId: String
    public let groupName: String
    public let kind: AssetProposalKind
    public let usdcMicros: Int64
    public let tokenAmount: Int64
    public let thesis: String?
    public let yes: Int
    public let no: Int
    /// How many members could vote, so the card can say "2 of 5".
    public let memberCount: Int
    /// Nil when the viewer has not voted — the difference between "2 open votes" and
    /// "2 open votes waiting on you".
    public let myVote: AssetVoteChoice?
    public let voters: [AssetVoterDTO]
    public let expiresAt: Date?
    public let createdAt: Date?

    public init(
        id: String,
        groupId: String,
        groupName: String,
        kind: AssetProposalKind,
        usdcMicros: Int64 = 0,
        tokenAmount: Int64 = 0,
        thesis: String? = nil,
        yes: Int = 0,
        no: Int = 0,
        memberCount: Int = 0,
        myVote: AssetVoteChoice? = nil,
        voters: [AssetVoterDTO] = [],
        expiresAt: Date? = nil,
        createdAt: Date? = nil
    ) {
        self.id = id
        self.groupId = groupId
        self.groupName = groupName
        self.kind = kind
        self.usdcMicros = usdcMicros
        self.tokenAmount = tokenAmount
        self.thesis = thesis
        self.yes = yes
        self.no = no
        self.memberCount = memberCount
        self.myVote = myVote
        self.voters = voters
        self.expiresAt = expiresAt
        self.createdAt = createdAt
    }

    /// The members who said yes, in the order they voted.
    public var yesVoters: [AssetVoterDTO] { voters.filter { $0.choice == .yes } }

    private enum CodingKeys: String, CodingKey {
        case id, groupId, groupName, kind, usdcMicros, tokenAmount, thesis
        case yes, no, memberCount, myVote, voters, expiresAt, createdAt
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        groupId = try container.decode(String.self, forKey: .groupId)
        groupName = try container.decodeIfPresent(String.self, forKey: .groupName) ?? ""
        kind = try container.decodeIfPresent(AssetProposalKind.self, forKey: .kind) ?? .unknown
        usdcMicros = try container.decodeIfPresent(Int64.self, forKey: .usdcMicros) ?? 0
        tokenAmount = try container.decodeIfPresent(Int64.self, forKey: .tokenAmount) ?? 0
        let rawThesis = try container.decodeIfPresent(String.self, forKey: .thesis)
        thesis = (rawThesis?.isEmpty ?? true) ? nil : rawThesis
        yes = try container.decodeIfPresent(Int.self, forKey: .yes) ?? 0
        no = try container.decodeIfPresent(Int.self, forKey: .no) ?? 0
        memberCount = try container.decodeIfPresent(Int.self, forKey: .memberCount) ?? 0
        // The backend omits the key entirely when the viewer has not voted.
        myVote = try container.decodeIfPresent(AssetVoteChoice.self, forKey: .myVote)
        voters = try container.decodeIfPresent([AssetVoterDTO].self, forKey: .voters) ?? []
        expiresAt = try container.decodeIfPresent(MonacoTimestamp.self, forKey: .expiresAt)?.date
        createdAt = try container.decodeIfPresent(MonacoTimestamp.self, forKey: .createdAt)?.date
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(groupId, forKey: .groupId)
        try container.encode(groupName, forKey: .groupName)
        try container.encode(kind, forKey: .kind)
        try container.encode(usdcMicros, forKey: .usdcMicros)
        try container.encode(tokenAmount, forKey: .tokenAmount)
        try container.encodeIfPresent(thesis, forKey: .thesis)
        try container.encode(yes, forKey: .yes)
        try container.encode(no, forKey: .no)
        try container.encode(memberCount, forKey: .memberCount)
        try container.encodeIfPresent(myVote, forKey: .myVote)
        try container.encode(voters, forKey: .voters)
        try container.encodeIfPresent(expiresAt.map(MonacoTimestamp.init(date:)), forKey: .expiresAt)
        try container.encodeIfPresent(createdAt.map(MonacoTimestamp.init(date:)), forKey: .createdAt)
    }
}

/// What happened. `unknown` exists so a kind added later cannot fail the response.
public enum AssetActivityKind: String, Codable, Sendable {
    case proposed
    case passed
    case failed
    case expired
    case filled
    case unknown

    public init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = AssetActivityKind(rawValue: raw) ?? .unknown
    }
}

/// One thing that happened to this stock in one of the viewer's cabals.
public struct AssetActivityDTO: Codable, Equatable, Sendable, Identifiable {
    public let id: String
    public let groupId: String
    public let groupName: String
    public let kind: AssetActivityKind
    /// "buy" or "sell" — what the proposal or swap was about.
    public let action: AssetProposalKind
    public let usdcMicros: Int64
    public let tokenAmount: Int64
    public let actorName: String?
    public let txHash: String?
    public let createdAt: Date?

    public init(
        id: String,
        groupId: String,
        groupName: String,
        kind: AssetActivityKind,
        action: AssetProposalKind = .unknown,
        usdcMicros: Int64 = 0,
        tokenAmount: Int64 = 0,
        actorName: String? = nil,
        txHash: String? = nil,
        createdAt: Date? = nil
    ) {
        self.id = id
        self.groupId = groupId
        self.groupName = groupName
        self.kind = kind
        self.action = action
        self.usdcMicros = usdcMicros
        self.tokenAmount = tokenAmount
        self.actorName = actorName
        self.txHash = txHash
        self.createdAt = createdAt
    }

    private enum CodingKeys: String, CodingKey {
        case id, groupId, groupName, kind, action, usdcMicros, tokenAmount
        case actorName, txHash, createdAt
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        groupId = try container.decode(String.self, forKey: .groupId)
        groupName = try container.decodeIfPresent(String.self, forKey: .groupName) ?? ""
        kind = try container.decodeIfPresent(AssetActivityKind.self, forKey: .kind) ?? .unknown
        action = try container.decodeIfPresent(AssetProposalKind.self, forKey: .action) ?? .unknown
        usdcMicros = try container.decodeIfPresent(Int64.self, forKey: .usdcMicros) ?? 0
        tokenAmount = try container.decodeIfPresent(Int64.self, forKey: .tokenAmount) ?? 0
        let actor = try container.decodeIfPresent(String.self, forKey: .actorName)
        actorName = (actor?.isEmpty ?? true) ? nil : actor
        let hash = try container.decodeIfPresent(String.self, forKey: .txHash)
        txHash = (hash?.isEmpty ?? true) ? nil : hash
        createdAt = try container.decodeIfPresent(MonacoTimestamp.self, forKey: .createdAt)?.date
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(groupId, forKey: .groupId)
        try container.encode(groupName, forKey: .groupName)
        try container.encode(kind, forKey: .kind)
        try container.encode(action, forKey: .action)
        try container.encode(usdcMicros, forKey: .usdcMicros)
        try container.encode(tokenAmount, forKey: .tokenAmount)
        try container.encodeIfPresent(actorName, forKey: .actorName)
        try container.encodeIfPresent(txHash, forKey: .txHash)
        try container.encodeIfPresent(createdAt.map(MonacoTimestamp.init(date:)), forKey: .createdAt)
    }
}

/// `GET /v1/assets/{symbol}/social`: what the viewer's own cabals are doing with one
/// stock.
public struct AssetSocialDTO: Codable, Equatable, Sendable {
    public let symbol: String
    public let holdings: [AssetHoldingDTO]
    public let openProposals: [AssetProposalDTO]
    public let activity: [AssetActivityDTO]
    public let holderCount: Int
    /// How many of the viewer's cabals could not be priced on this pass. The card
    /// says so rather than implying those cabals hold nothing.
    public let unvaluedGroups: Int

    public init(
        symbol: String,
        holdings: [AssetHoldingDTO] = [],
        openProposals: [AssetProposalDTO] = [],
        activity: [AssetActivityDTO] = [],
        holderCount: Int = 0,
        unvaluedGroups: Int = 0
    ) {
        self.symbol = symbol
        self.holdings = holdings
        self.openProposals = openProposals
        self.activity = activity
        self.holderCount = holderCount
        self.unvaluedGroups = unvaluedGroups
    }

    /// True when there is nothing at all to show, so the position card hides itself
    /// rather than drawing an empty frame under the chart.
    public var isEmpty: Bool {
        holdings.isEmpty && openProposals.isEmpty && unvaluedGroups == 0
    }

    private enum CodingKeys: String, CodingKey {
        case symbol, holdings, openProposals, activity, holderCount, unvaluedGroups
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        symbol = try container.decodeIfPresent(String.self, forKey: .symbol) ?? ""
        holdings = try container.decodeIfPresent([AssetHoldingDTO].self, forKey: .holdings) ?? []
        openProposals = try container.decodeIfPresent([AssetProposalDTO].self, forKey: .openProposals) ?? []
        activity = try container.decodeIfPresent([AssetActivityDTO].self, forKey: .activity) ?? []
        holderCount = try container.decodeIfPresent(Int.self, forKey: .holderCount) ?? holdings.count
        unvaluedGroups = try container.decodeIfPresent(Int.self, forKey: .unvaluedGroups) ?? 0
    }
}
