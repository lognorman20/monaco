import Foundation

// Contracts for the invite routes (docs/api.md, "Invites"):
//   GET  /v1/groups/{id}/invites          → InviteDTO (members only)
//   POST /v1/groups/{id}/invites          → InviteDTO, 201 (the old code stops working)
//   POST /v1/groups/{id}/invites/revoke   → 204
//   GET  /v1/invites/{code}               → InvitePreviewDTO (public)
//   POST /v1/groups/join-by-code          → 204 joined, 202 { status, groupId } pending

/// A cabal's live invite: the code and the link that carries it.
public struct InviteDTO: Codable, Equatable, Sendable {
    public let code: String
    public let url: String

    public init(code: String, url: String) {
        self.code = code
        self.url = url
    }

    /// The link as a URL. Falls back to the canonical link for the code if the server's
    /// string does not parse, so Share and Copy link always have something to send.
    public var link: URL {
        URL(string: url) ?? InviteLink.webURL(for: code)
    }
}

/// What anyone holding a live code may see about the cabal behind it.
public struct InvitePreviewDTO: Codable, Equatable, Sendable {
    public let code: String
    public let groupId: String
    public let name: String
    public let memberCount: Int
    /// The cabal's identity tint by name (`pine`, `ochre`, `plum`, `indigo`, `moss`). The app
    /// derives the same tint from `groupId`, so this is for clients that cannot.
    public let tint: String
    /// Nil when the cabal has no picture; the mark falls back to its tinted initials.
    public let pictureUrl: String?
    public let joinPolicy: GroupJoinMode
    /// The pot as a USD decimal string, like the Groups tab's `potValueUsd`.
    public let potValueUsd: String

    public init(
        code: String,
        groupId: String,
        name: String,
        memberCount: Int,
        tint: String,
        pictureUrl: String?,
        joinPolicy: GroupJoinMode,
        potValueUsd: String
    ) {
        self.code = code
        self.groupId = groupId
        self.name = name
        self.memberCount = memberCount
        self.tint = tint
        self.pictureUrl = pictureUrl
        self.joinPolicy = joinPolicy
        self.potValueUsd = potValueUsd
    }
}

/// How a join through a code ended.
public enum InviteJoinStatus: String, Codable, Equatable, Sendable {
    /// The member is in (or already was: the server answers both with 204).
    case joined
    /// The cabal's admin approves members; the request is waiting.
    case pending
}

public struct InviteJoinResult: Equatable, Sendable {
    public let status: InviteJoinStatus
    /// The cabal the code named, from the 202 body or the `Location` header. Nil only if the
    /// server sent neither.
    public let groupId: String?

    public init(status: InviteJoinStatus, groupId: String?) {
        self.status = status
        self.groupId = groupId
    }
}
