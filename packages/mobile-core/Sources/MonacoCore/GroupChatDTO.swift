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
    /// `createdAt` parsed once, when the message is decoded or built. Rendering reads this on
    /// every pass, so it must never go back through a date formatter. Nil when unparseable.
    public let createdAtDate: Date?

    private enum CodingKeys: String, CodingKey {
        case id, groupId, authorId, authorName, body, createdAt, mine
    }

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
        self.createdAtDate = GroupChatDates.parse(createdAt)
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try self.init(
            id: container.decode(String.self, forKey: .id),
            groupId: container.decode(String.self, forKey: .groupId),
            authorId: container.decode(String.self, forKey: .authorId),
            authorName: container.decode(String.self, forKey: .authorName),
            body: container.decode(String.self, forKey: .body),
            createdAt: container.decode(String.self, forKey: .createdAt),
            mine: container.decode(Bool.self, forKey: .mine)
        )
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
    /// The API always sends a fraction, so that format goes first; whole seconds is the fallback.
    static func parse(_ value: String) -> Date? {
        if let date = SharedFormatters.iso8601Fractional.date(from: truncatingFraction(value, toDigits: 3)) {
            return date
        }
        return SharedFormatters.iso8601WholeSeconds.date(from: value)
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

/// A message the viewer sent that the server has not confirmed yet, or refused.
public struct GroupChatPendingMessage: Equatable, Sendable, Identifiable {
    public enum State: Equatable, Sendable {
        case sending
        case failed
    }

    /// Generated on the device; the row keeps this identity after the server confirms it.
    public let clientId: String
    public let body: String
    /// Device clock. Only places and labels the row until the server's own time arrives.
    public let createdAt: Date
    public fileprivate(set) var state: State
    /// `createdAt` of the newest server message on screen when this send began. Only a server
    /// message after it can be this one coming back in a poll.
    fileprivate let newestKnownCreatedAt: String?
    /// False when the send began before the first page loaded: with no baseline, an old
    /// message with the same text would look like this one.
    fileprivate let canMatchPolledMessage: Bool
    /// The server row a poll delivered before the POST answered.
    fileprivate var matchedServerId: String?

    public var id: String { clientId }
}

/// One bubble in the thread with everything the view needs already worked out, so a render
/// pass does no date parsing and no neighbour lookups.
public struct GroupChatRow: Equatable, Sendable, Identifiable {
    public enum Delivery: Equatable, Sendable {
        case delivered
        case sending
        case failed
    }

    /// Stable for the life of the bubble: the client id for messages sent from this screen
    /// (before and after the server confirms them), the server id for everything else.
    public let id: String
    /// The server's id once there is one.
    public let serverId: String?
    public let authorName: String
    public let body: String
    public let mine: Bool
    public let date: Date?
    public let delivery: Delivery
    /// Set when a centred time separator goes above this bubble.
    public let separatorDate: Date?
    public let startsRun: Bool
    public let endsRun: Bool
}

/// Chat history held oldest-to-newest for display, merged from newest-first API pages, plus the
/// viewer's own messages that are still on their way to the server.
public struct GroupChatTimeline: Equatable, Sendable {
    public private(set) var messages: [GroupMessageDTO] = []
    /// Messages sent from this screen that the server has not confirmed, oldest first.
    public private(set) var pending: [GroupChatPendingMessage] = []
    /// `messages` then `pending`, ready to render. Rebuilt only when either changes.
    public private(set) var rows: [GroupChatRow] = []
    /// Cursor for the next older page; nil once the oldest page has loaded.
    public private(set) var olderCursor: String?
    /// False until the first page arrives, so the UI can tell "loading" from "empty".
    public private(set) var hasLoadedNewest = false
    /// Server id → client id for messages sent from this screen, so their rows keep one identity.
    private var clientIdByServerId: [String: String] = [:]

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
        return merge(page.messages, matchingPending: true)
    }

    /// Applies an older page fetched with `olderCursor`.
    public mutating func mergeOlder(_ page: GroupMessagesPageDTO) {
        olderCursor = page.nextCursor
        merge(page.messages, matchingPending: false)
    }

    // MARK: Optimistic sends

    /// Shows `body` in the thread straight away, as sending. `clientId` identifies it until
    /// `confirmSent` or `failSend` settles it.
    public mutating func beginSend(clientId: String, body: String, at now: Date = Date()) {
        guard !pending.contains(where: { $0.clientId == clientId }) else { return }
        pending.append(GroupChatPendingMessage(
            clientId: clientId,
            body: body,
            createdAt: now,
            state: .sending,
            newestKnownCreatedAt: messages.last?.createdAt,
            canMatchPolledMessage: hasLoadedNewest,
            matchedServerId: nil
        ))
        rebuildRows()
    }

    /// The POST answered: the server's row replaces the pending one. If a poll already
    /// delivered that row nothing is added twice, because messages are keyed by server id.
    public mutating func confirmSent(clientId: String, message: GroupMessageDTO) {
        if let index = pending.firstIndex(where: { $0.clientId == clientId }) {
            // A poll row taken for this message turned out to be another one: give it back its own identity.
            if let matched = pending[index].matchedServerId, matched != message.id {
                clientIdByServerId[matched] = nil
            }
            pending.remove(at: index)
            clientIdByServerId[message.id] = clientId
        }
        if merge([message], matchingPending: false).isEmpty { rebuildRows() }
    }

    public enum SendFailureOutcome: Equatable, Sendable {
        /// The row now shows as failed and can be retried.
        case markedFailed
        /// A poll had already delivered the message — the POST's answer was lost, not the
        /// message — so there is nothing to retry and nothing to tell the viewer.
        case alreadyDelivered
        /// No such pending message: it was already settled.
        case unknown
    }

    /// The POST failed.
    @discardableResult
    public mutating func failSend(clientId: String) -> SendFailureOutcome {
        guard let index = pending.firstIndex(where: { $0.clientId == clientId }) else { return .unknown }
        if pending[index].matchedServerId != nil {
            pending.remove(at: index)
            rebuildRows()
            return .alreadyDelivered
        }
        pending[index].state = .failed
        rebuildRows()
        return .markedFailed
    }

    /// Puts a failed message back to sending. Returns its body, or nil when it is not failed.
    public mutating func retrySend(clientId: String) -> String? {
        guard let index = pending.firstIndex(where: { $0.clientId == clientId && $0.state == .failed }) else {
            return nil
        }
        pending[index].state = .sending
        rebuildRows()
        return pending[index].body
    }

    /// Drops a failed message the viewer no longer wants to send.
    public mutating func discardFailed(clientId: String) {
        let countBefore = pending.count
        pending.removeAll { $0.clientId == clientId && $0.state == .failed }
        if pending.count != countBefore { rebuildRows() }
    }

    // MARK: Merging

    @discardableResult
    private mutating func merge(_ incoming: [GroupMessageDTO], matchingPending: Bool) -> [String] {
        var byID = Dictionary(uniqueKeysWithValues: messages.map { ($0.id, $0) })
        var added: [GroupMessageDTO] = []
        for message in incoming where byID[message.id] == nil {
            byID[message.id] = message
            added.append(message)
        }
        guard !added.isEmpty else { return [] }
        messages = byID.values.sorted(by: Self.chronological)
        if matchingPending { matchPending(against: added) }
        rebuildRows()
        return added.map(\.id)
    }

    /// The API takes no client id, so a poll that beats the POST response brings the viewer's
    /// own message back as a stranger. Recognise it — mine, same text, newer than anything on
    /// screen when the send began — and stop showing the pending copy, so it never appears
    /// twice. The POST response still has the last word: `confirmSent` keys on the server id.
    private mutating func matchPending(against added: [GroupMessageDTO]) {
        guard !pending.isEmpty else { return }
        for message in added.sorted(by: Self.chronological) where message.mine {
            guard let index = pending.firstIndex(where: { candidate in
                candidate.matchedServerId == nil
                    && candidate.canMatchPolledMessage
                    && candidate.body == message.body
                    && (candidate.newestKnownCreatedAt.map { message.createdAt > $0 } ?? true)
            }) else { continue }
            // The bubble keeps the identity it was given when the viewer hit send.
            clientIdByServerId[message.id] = pending[index].clientId
            if pending[index].state == .failed {
                // The send was reported failed but landed after all: nothing left to retry.
                pending.remove(at: index)
            } else {
                pending[index].matchedServerId = message.id
            }
        }
    }

    /// The API sends fixed-width microsecond UTC timestamps (`2026-09-18T15:04:05.123456Z`),
    /// so string order is time order and keeps sub-millisecond ordering that `Date` parsing drops.
    private static func chronological(_ lhs: GroupMessageDTO, _ rhs: GroupMessageDTO) -> Bool {
        if lhs.createdAt != rhs.createdAt { return lhs.createdAt < rhs.createdAt }
        return lhs.id < rhs.id
    }

    // MARK: Rows

    private struct Bubble {
        let id: String
        let serverId: String?
        let authorId: String?
        let authorName: String
        let body: String
        let mine: Bool
        let date: Date?
        let delivery: GroupChatRow.Delivery
    }

    private mutating func rebuildRows() {
        var bubbles = messages.map { message in
            Bubble(
                id: clientIdByServerId[message.id] ?? message.id,
                serverId: message.id,
                authorId: message.authorId,
                authorName: message.authorName,
                body: message.body,
                mine: message.mine,
                date: message.createdAtDate,
                delivery: .delivered
            )
        }
        // A pending message a poll already delivered is on screen as that server row.
        for message in pending where message.matchedServerId == nil {
            bubbles.append(Bubble(
                id: message.clientId,
                serverId: nil,
                authorId: nil,
                authorName: "",
                body: message.body,
                mine: true,
                date: message.createdAt,
                delivery: message.state == .failed ? .failed : .sending
            ))
        }

        var separators: [Date?] = []
        separators.reserveCapacity(bubbles.count)
        var previousDate: Date?
        for bubble in bubbles {
            if let date = bubble.date, GroupChatCopy.showsTimeSeparator(previous: previousDate, current: date) {
                separators.append(date)
            } else {
                separators.append(nil)
            }
            previousDate = bubble.date
        }

        rows = bubbles.indices.map { index in
            let bubble = bubbles[index]
            let previous = index > 0 ? bubbles[index - 1] : nil
            let next = index + 1 < bubbles.count ? bubbles[index + 1] : nil
            let nextHasSeparator = next != nil && separators[index + 1] != nil
            return GroupChatRow(
                id: bubble.id,
                serverId: bubble.serverId,
                authorName: bubble.authorName,
                body: bubble.body,
                mine: bubble.mine,
                date: bubble.date,
                delivery: bubble.delivery,
                separatorDate: separators[index],
                startsRun: separators[index] != nil || !Self.sameAuthor(previous, bubble),
                endsRun: nextHasSeparator || !Self.sameAuthor(next, bubble)
            )
        }
    }

    /// Every `mine` bubble is the viewer's, whether or not the server has named its author yet.
    private static func sameAuthor(_ other: Bubble?, _ bubble: Bubble) -> Bool {
        guard let other else { return false }
        if other.mine || bubble.mine { return other.mine && bubble.mine }
        return other.authorId == bubble.authorId
    }
}

/// User-facing chat copy shared by the app and copy audits.
public enum GroupChatCopy {
    /// Fallback navigation title when the cabal's name isn't known yet.
    public static let title = "Cabal chat"
    public static let emptyState = "No messages yet. Say hi to your cabal or float a stock idea before someone proposes a buy."
    public static let composerPlaceholder = "Message your cabal"
    public static let loadEarlier = "Load earlier messages"
    public static let sending = "Sending…"
    public static let notSent = "Not sent. Tap to try again."
    public static let tryAgain = "Try again"
    public static let deleteUnsent = "Delete message"

    public static func sendFailure(_ error: Error) -> String {
        switch error {
        case GroupChatDraft.Problem.empty:
            return "Type a message first."
        case GroupChatDraft.Problem.tooLong:
            return "Messages can be up to \(GroupChatDraft.maxCharacters) characters."
        // 429 arrives as its own case with the server's Retry-After, never as httpStatus.
        case MonacoAPIError.rateLimited(let retryAfterSeconds, _):
            guard let seconds = retryAfterSeconds, seconds > 0 else {
                return "You're sending messages fast. Wait a moment and try again."
            }
            return "You're sending messages fast. Try again in \(seconds) second\(seconds == 1 ? "" : "s")."
        // 4xx bodies carry the API's own reason; show it when it was written for members.
        case MonacoAPIError.rejected(let status, let message, _):
            if let memberFacing = MoneyFlowCopy.memberFacingMessage(message) { return memberFacing }
            return sendFailure(MonacoAPIError.httpStatus(status))
        case MonacoAPIError.httpStatus(let code, _):
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

    /// The chat screen is titled with the cabal's own name; "Cabal chat" only when it's missing.
    public static func title(groupName: String?) -> String {
        let trimmed = groupName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return trimmed.isEmpty ? title : trimmed
    }

    /// Gap after which the thread shows a centred time separator instead of stamping every bubble.
    public static let timeSeparatorGap: TimeInterval = 10 * 60

    /// True for the first message and whenever more than ten minutes passed since the previous one.
    public static func showsTimeSeparator(previous: Date?, current: Date) -> Bool {
        guard let previous else { return true }
        return current.timeIntervalSince(previous) > timeSeparatorGap
    }

    /// "Today 12:40", "Yesterday 9:02 AM", "Sep 14, 9:02 AM" in the viewer's local time (the API stores UTC).
    public static func timeSeparatorLabel(
        _ date: Date,
        now: Date = Date(),
        calendar: Calendar = .current,
        locale: Locale = .current
    ) -> String {
        let clock = SharedFormatters.string(from: date, pattern: .template("jmm"), locale: locale, calendar: calendar)
        if calendar.isDate(date, inSameDayAs: now) {
            return "Today \(clock)"
        }
        if let yesterday = calendar.date(byAdding: .day, value: -1, to: now),
           calendar.isDate(date, inSameDayAs: yesterday) {
            return "Yesterday \(clock)"
        }
        let sameYear = calendar.component(.year, from: date) == calendar.component(.year, from: now)
        let day = SharedFormatters.string(
            from: date,
            pattern: .template(sameYear ? "MMMd" : "yMMMd"),
            locale: locale,
            calendar: calendar
        )
        return "\(day), \(clock)"
    }

    public static func loadFailure(_ error: Error) -> String {
        if case MonacoAPIError.httpStatus(403, _) = error {
            return "Only members of this cabal can read the chat."
        }
        return "Couldn't load messages. Pull to try again."
    }
}
