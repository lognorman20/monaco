import Foundation

/// One cabal chat message from `GET/POST /v1/groups/{id}/messages`.
public struct GroupMessageDTO: Codable, Equatable, Sendable, Identifiable {
    public let id: String
    public let groupId: String
    public let authorId: String
    public let authorName: String
    public let body: String
    /// Fixed-width RFC3339 UTC timestamp with microseconds, e.g. `2026-09-18T15:04:05.123456Z`.
    public let createdAt: String
    /// True when the signed-in viewer wrote this message.
    public let mine: Bool

    public init(
        id: String,
        groupId: String,
        authorId: String,
        authorName: String,
        body: String,
        createdAt: String,
        mine: Bool
    ) {
        self.id = id
        self.groupId = groupId
        self.authorId = authorId
        self.authorName = authorName
        self.body = body
        self.createdAt = createdAt
        self.mine = mine
    }

    public var createdAtDate: Date? {
        GroupChatDates.parse(createdAt)
    }
}

/// Newest-first page. `nextCursor` is present only when older messages exist.
public struct GroupMessagesPageDTO: Codable, Equatable, Sendable {
    public let messages: [GroupMessageDTO]
    public let nextCursor: String?

    public init(messages: [GroupMessageDTO], nextCursor: String? = nil) {
        self.messages = messages
        self.nextCursor = nextCursor
    }
}

enum GroupChatDates {
    static func parse(_ value: String) -> Date? {
        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]
        if let date = plain.date(from: value) { return date }
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fractional.date(from: truncatingFraction(value, toDigits: 3))
    }

    /// ISO8601DateFormatter only reliably reads millisecond fractions; drop digits past that.
    private static func truncatingFraction(_ value: String, toDigits digits: Int) -> String {
        guard let dot = value.firstIndex(of: ".") else { return value }
        let fractionStart = value.index(after: dot)
        let fractionEnd = value[fractionStart...].firstIndex { !$0.isNumber } ?? value.endIndex
        let fraction = value[fractionStart..<fractionEnd]
        guard fraction.count > digits else { return value }
        return String(value[..<fractionStart]) + fraction.prefix(digits) + value[fractionEnd...]
    }
}

/// Chat transport used by the chat screen. `MonacoAPIClient` is the live implementation.
public protocol GroupChatService: Sendable {
    func listGroupMessages(groupId: String, before: String?, limit: Int) async throws -> GroupMessagesPageDTO
    func postGroupMessage(groupId: String, body: String) async throws -> GroupMessageDTO
}

extension MonacoAPIClient: GroupChatService {}

/// Client-side draft rules mirroring the API (trimmed, 1...2000 characters).
public enum GroupChatDraft {
    public static let maxCharacters = 2000

    public enum Problem: Error, Equatable, Sendable {
        case empty
        case tooLong(count: Int)
    }

    /// Returns the trimmed body to send, or the reason it cannot be sent.
    public static func validate(_ text: String) -> Result<String, Problem> {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { return .failure(.empty) }
        let count = trimmed.unicodeScalars.count
        if count > maxCharacters { return .failure(.tooLong(count: count)) }
        return .success(trimmed)
    }
}

/// Chat history held oldest-to-newest for display, merged from newest-first API pages.
public struct GroupChatTimeline: Equatable, Sendable {
    public private(set) var messages: [GroupMessageDTO] = []
    /// Cursor for the next older page; nil once the oldest page has loaded.
    public private(set) var olderCursor: String?
    /// False until the first page arrives, so the UI can tell "loading" from "empty".
    public private(set) var hasLoadedNewest = false

    public init() {}

    public var hasOlder: Bool { olderCursor != nil }

    /// Applies a poll or first load of the newest page. Returns ids that were not already present.
    ///
    /// If a poll page shares no message with what is on screen and older messages exist, more
    /// arrived than one page holds. The older cursor then moves to that page so "Load earlier"
    /// walks through the gap; already-seen messages are de-duplicated by id on the way.
    @discardableResult
    public mutating func mergeNewest(_ page: GroupMessagesPageDTO) -> [String] {
        let isFirstLoad = !hasLoadedNewest
        hasLoadedNewest = true
        if isFirstLoad {
            olderCursor = page.nextCursor
        } else if page.nextCursor != nil, !messages.isEmpty {
            let known = Set(messages.map(\.id))
            if !page.messages.contains(where: { known.contains($0.id) }) {
                olderCursor = page.nextCursor
            }
        }
        return merge(page.messages)
    }

    /// Applies an older page fetched with `olderCursor`.
    public mutating func mergeOlder(_ page: GroupMessagesPageDTO) {
        olderCursor = page.nextCursor
        merge(page.messages)
    }

    /// Adds a message the viewer just sent (POST response).
    public mutating func appendSent(_ message: GroupMessageDTO) {
        merge([message])
    }

    @discardableResult
    private mutating func merge(_ incoming: [GroupMessageDTO]) -> [String] {
        var byID = Dictionary(uniqueKeysWithValues: messages.map { ($0.id, $0) })
        var added: [String] = []
        for message in incoming where byID[message.id] == nil {
            byID[message.id] = message
            added.append(message.id)
        }
        guard !added.isEmpty else { return [] }
        messages = byID.values.sorted(by: Self.chronological)
        return added
    }

    /// The API sends fixed-width microsecond UTC timestamps (`2026-09-18T15:04:05.123456Z`),
    /// so string order is time order and keeps sub-millisecond ordering that `Date` parsing drops.
    private static func chronological(_ lhs: GroupMessageDTO, _ rhs: GroupMessageDTO) -> Bool {
        if lhs.createdAt != rhs.createdAt { return lhs.createdAt < rhs.createdAt }
        return lhs.id < rhs.id
    }
}

/// User-facing chat copy shared by the app and copy audits.
public enum GroupChatCopy {
    public static let title = "Chat"
    public static let emptyState = "No messages yet. Say hi to your cabal or float a stock idea before someone proposes a buy."
    public static let composerPlaceholder = "Message your cabal"
    public static let loadEarlier = "Load earlier messages"

    public static func sendFailure(_ error: Error) -> String {
        switch error {
        case GroupChatDraft.Problem.empty:
            return "Type a message first."
        case GroupChatDraft.Problem.tooLong:
            return "Messages can be up to \(GroupChatDraft.maxCharacters) characters."
        case MonacoAPIError.httpStatus(let code):
            switch code {
            case 401: return "Your session expired. Sign in again to chat."
            case 403: return "Only members of this cabal can chat here."
            case 404: return "This cabal no longer exists."
            case 429: return "You're sending messages fast. Wait a moment and try again."
            case 400: return "That message couldn't be sent. Check the text and try again."
            default: return "Message not sent. Try again."
            }
        default:
            if let urlError = error as? URLError, urlError.code == .notConnectedToInternet {
                return "You're offline. Message not sent."
            }
            return "Message not sent. Check your connection and try again."
        }
    }

    public static func loadFailure(_ error: Error) -> String {
        if case MonacoAPIError.httpStatus(403) = error {
            return "Only members of this cabal can read the chat."
        }
        return "Couldn't load messages. Pull to try again."
    }
}
