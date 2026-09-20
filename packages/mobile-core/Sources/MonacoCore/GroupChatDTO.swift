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
        if let date = SharedFormatters.iso8601WholeSeconds.date(from: value) { return date }
        return SharedFormatters.iso8601Fractional.date(from: truncatingFraction(value, toDigits: 3))
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

/// One rendered line of the thread: the message plus everything the view would otherwise
/// work out again on every body pass.
///
/// Parsing a chat timestamp is not cheap — the API's microsecond stamp misses the
/// whole-seconds formatter, gets truncated into a fresh string, then parsed a second time —
/// and the thread needed up to four of those per visible row, on a body that a single
/// composer keystroke invalidates. Rows are built once, when a page is merged.
public struct GroupChatRow: Identifiable, Equatable, Sendable {
    public let message: GroupMessageDTO
    /// When it was sent, parsed once. Nil for a stamp that will not parse.
    public let date: Date?
    /// True when a centred time label belongs above this bubble.
    public let showsTimeSeparator: Bool
    /// First bubble of a run by this author: the author's name goes above it.
    public let startsRun: Bool
    /// Last bubble of a run: the corner on the author's side is tightened.
    public let endsRun: Bool

    public var id: String { message.id }

    public init(
        message: GroupMessageDTO,
        date: Date?,
        showsTimeSeparator: Bool,
        startsRun: Bool,
        endsRun: Bool
    ) {
        self.message = message
        self.date = date
        self.showsTimeSeparator = showsTimeSeparator
        self.startsRun = startsRun
        self.endsRun = endsRun
    }

    /// "Today 12:40" for the rows that carry one. Formatted on demand rather than stored, so a
    /// thread left open past midnight stops calling yesterday's messages "Today".
    public func timeSeparatorLabel(now: Date = Date()) -> String? {
        guard showsTimeSeparator, let date else { return nil }
        return GroupChatCopy.timeSeparatorLabel(date, now: now)
    }
}

/// Chat history held oldest-to-newest for display, merged from newest-first API pages.
public struct GroupChatTimeline: Equatable, Sendable {
    public private(set) var messages: [GroupMessageDTO] = []
    /// The thread as the view draws it: dates parsed, runs and separators already decided.
    public private(set) var rows: [GroupChatRow] = []
    /// Cursor for the next older page; nil once the oldest page has loaded.
    public private(set) var olderCursor: String?
    /// False until the first page arrives, so the UI can tell "loading" from "empty".
    public private(set) var hasLoadedNewest = false

    public init() {}

    public var hasOlder: Bool { olderCursor != nil }

    /// Whether an arrival should pull the thread down to the newest message.
    ///
    /// Two things earn a scroll: a message the viewer just sent, and one landing while the
    /// thread was following the conversation. A member scrolled up in the history keeps their
    /// place and is told about the new messages instead of being thrown at them.
    ///
    /// `isFollowingThread` is the reader's standing intent — they have not dragged away from
    /// the end, or they have asked to go back to it — and deliberately not a measurement of
    /// the current offset. Appending a row grows the content before anything scrolls, so a
    /// thread that is faithfully following the conversation reads as "not at the bottom" for
    /// a frame or two after every message. Deciding on that would start counting arrivals as
    /// unread behind a reader who is watching them land.
    public static func shouldAutoScroll(added: [GroupMessageDTO], isFollowingThread: Bool) -> Bool {
        guard !added.isEmpty else { return false }
        if added.contains(where: \.mine) { return true }
        return isFollowingThread
    }

    /// Applies a poll or first load of the newest page. Returns the messages that were not
    /// already present, oldest first.
    ///
    /// If a poll page shares no message with what is on screen and older messages exist, more
    /// arrived than one page holds. The older cursor then moves to that page so "Load earlier"
    /// walks through the gap; already-seen messages are de-duplicated by id on the way.
    @discardableResult
    public mutating func mergeNewest(_ page: GroupMessagesPageDTO) -> [GroupMessageDTO] {
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
    private mutating func merge(_ incoming: [GroupMessageDTO]) -> [GroupMessageDTO] {
        var byID = Dictionary(uniqueKeysWithValues: messages.map { ($0.id, $0) })
        var addedIDs: Set<String> = []
        for message in incoming where byID[message.id] == nil {
            byID[message.id] = message
            addedIDs.insert(message.id)
        }
        // A quiet tick adds nothing: leave `messages` and `rows` untouched so the thread is
        // not re-sorted, not rebuilt, and — this is the point — not invalidated.
        guard !addedIDs.isEmpty else { return [] }
        messages = byID.values.sorted(by: Self.chronological)
        rows = Self.rows(for: messages, reusing: rows)
        return messages.filter { addedIDs.contains($0.id) }
    }

    /// Two passes because a bubble's tail depends on the row *after* it.
    ///
    /// Dates come from `reusing` wherever the message was already on screen. Parsing a chat
    /// timestamp costs two formatter passes and a fresh string, and a poll that brings one
    /// message used to re-parse the whole loaded thread on the main actor every four seconds —
    /// which on a busy cabal gave back most of what building rows once was meant to win. Only
    /// the arrivals are parsed now; the runs and separators are plain `Date` comparisons.
    private static func rows(
        for messages: [GroupMessageDTO],
        reusing previous: [GroupChatRow]
    ) -> [GroupChatRow] {
        var known = [String: Date?](minimumCapacity: previous.count)
        for row in previous { known[row.message.id] = row.date }
        // Two optionals deep on purpose: a miss re-parses, while a hit that parsed to nil
        // before stays nil rather than being parsed again to fail again.
        let dates = messages.map { known[$0.id] ?? $0.createdAtDate }
        var separators = [Bool](repeating: false, count: messages.count)
        for index in messages.indices {
            guard let date = dates[index] else { continue }
            let previous = index > 0 ? dates[index - 1] : nil
            separators[index] = GroupChatCopy.showsTimeSeparator(previous: previous, current: date)
        }
        return messages.indices.map { index in
            let message = messages[index]
            let previousAuthor = index > 0 ? messages[index - 1].authorId : nil
            let next = index + 1
            let endsRun = next >= messages.count
                || messages[next].authorId != message.authorId
                || separators[next]
            return GroupChatRow(
                message: message,
                date: dates[index],
                showsTimeSeparator: separators[index],
                startsRun: separators[index] || previousAuthor != message.authorId,
                endsRun: endsRun
            )
        }
    }

    /// The API sends fixed-width microsecond UTC timestamps (`2026-09-18T15:04:05.123456Z`),
    /// so string order is time order and keeps sub-millisecond ordering that `Date` parsing drops.
    private static func chronological(_ lhs: GroupMessageDTO, _ rhs: GroupMessageDTO) -> Bool {
        if lhs.createdAt != rhs.createdAt { return lhs.createdAt < rhs.createdAt }
        return lhs.id < rhs.id
    }
}

/// Whether the thread is currently shut to the viewer, and how sure the app is about it.
///
/// Closing is heavy-handed: it parks the poll, takes the composer away and tells a paying
/// member they are out of their cabal. It used to be one-way and a single background tick
/// could trigger it, so one 403 or 404 — a membership read served by a lagging replica, one of
/// the messages route's non-cabal 404s — ended the screen for the life of the view, with
/// nothing on it to tap.
///
/// Two rules put that right. Being thrown out of a cabal is a durable fact: the server says so
/// every time it is asked, so a background poll has to say it `pollsBeforeClosing` times in a
/// row before it is believed. And a load the member asked for is answered at once and in both
/// directions — they are watching, and a server that just handed over the thread settles the
/// question whatever the app believed a moment ago.
public struct GroupChatClosureTracker: Equatable, Sendable {
    /// Consecutive closed answers a background poll must give before the thread closes. At the
    /// 4-second tick that is a server holding the same story for over ten seconds, which no
    /// replica blip does and a real removal always will.
    public static let pollsBeforeClosing = 3

    /// Why the thread is shut, or nil while it is open.
    public private(set) var message: String?
    private var consecutiveClosedPolls = 0

    public init() {}

    public var isClosed: Bool { message != nil }

    /// The server answered with a page. Whatever the app believed about being shut out is out
    /// of date, so this reopens the thread as well as resetting the run of closed polls.
    public mutating func succeeded() {
        message = nil
        consecutiveClosedPolls = 0
    }

    /// A load the member asked for — opening the screen, Try again, pull to refresh, Load
    /// earlier, sending. They are waiting on this one, so it is believed immediately.
    public mutating func memberLoadFailed(_ error: Error) {
        guard let closed = GroupChatCopy.chatClosed(error) else { return }
        message = closed
        consecutiveClosedPolls = 0
    }

    /// A background tick. One closed answer is not evidence of anything.
    public mutating func pollFailed(_ error: Error) {
        guard let closed = GroupChatCopy.chatClosed(error) else {
            consecutiveClosedPolls = 0
            return
        }
        consecutiveClosedPolls += 1
        guard consecutiveClosedPolls >= Self.pollsBeforeClosing else { return }
        message = closed
    }
}

/// User-facing chat copy shared by the app and copy audits.
public enum GroupChatCopy {
    /// Fallback navigation title when the cabal's name isn't known yet.
    public static let title = "Cabal chat"
    public static let emptyState = "No messages yet. Say hi to your cabal or float a stock idea before someone proposes a buy."
    public static let composerPlaceholder = "Message your cabal"
    public static let loadEarlier = "Load earlier messages"

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
        // The API answered with something we can't read, which is no evidence it didn't store
        // the message first.
        case MonacoAPIError.invalidResponse:
            return sendUnconfirmed
        case MonacoAPIError.httpStatus(let code, _):
            switch code {
            case 401: return "Your session expired. Sign in again to chat."
            case 403: return "Only members of this cabal can chat here."
            case 404: return "This cabal no longer exists."
            case 429: return "You're sending messages fast. Wait a moment and try again."
            case 400: return "That message couldn't be sent. Check the text and try again."
            // The request reached the API and it broke on its own side of the line. That says
            // nothing about whether it wrote the message down before it did.
            case 500...599: return sendUnconfirmed
            default: return "Message not sent. Try again."
            }
        default:
            // Not a URL error at all: it got as far as a reply we couldn't read. A 201 whose
            // body fails to decode is still a message the API stored.
            guard let urlError = error as? URLError else { return sendUnconfirmed }
            if urlError.code == .notConnectedToInternet { return "You're offline. Message not sent." }
            return FlowErrorInput.neverSentURLErrorCodes.contains(urlError.code)
                ? "Message not sent. Check your connection and try again."
                : sendUnconfirmed
        }
    }

    /// Sending posts a bare body — no idempotency key, unlike the money routes — so a second
    /// attempt at a message that did land posts it twice, and chat has no delete. When the
    /// failure could only have happened after the request went out, say we don't know instead
    /// of promising it didn't arrive; the next poll answers the question.
    ///
    /// Same judgement the money flows make in `MoneyFlowCopy.unconfirmed`, and the same list of
    /// URL errors raised before any byte leaves the device.
    public static let sendUnconfirmed = "We couldn't confirm that went through. Check above before sending it again."

    /// True when a failed send leaves it unknown whether the API stored the message.
    ///
    /// The same judgement `sendFailure` already makes, asked as a question rather than
    /// restated, so the two cannot drift apart. The caller needs it because the two answers
    /// differ in what to do with the member's text: a send that definitely failed should hand
    /// it back, while one that may have landed must not — chat has no delete, and a composer
    /// refilled with a message that is already in the thread is one tap from the duplicate
    /// this sentence exists to prevent.
    public static func isSendUnconfirmed(_ error: Error) -> Bool {
        sendFailure(error) == sendUnconfirmed
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

    /// Shown in place of the thread when the first page never arrived. A Try again button
    /// sits right under it, so this must not send the member looking for a gesture: there is
    /// nothing to pull on an empty screen.
    public static func loadFailure(_ error: Error) -> String {
        if let closed = chatClosed(error) { return closed }
        return "Couldn't load messages."
    }

    /// Toast for a refresh of a thread already on screen, where pulling down does work.
    public static func refreshFailure(_ error: Error) -> String {
        if let closed = chatClosed(error) { return closed }
        return "Couldn't refresh messages. Pull down to try again."
    }

    /// Toast for the "Load earlier messages" button.
    public static func earlierFailure(_ error: Error) -> String {
        if let closed = chatClosed(error) { return closed }
        return "Couldn't load earlier messages. Try again."
    }

    /// Why this thread is no longer readable, or nil for a failure worth retrying. Chat stops
    /// polling on one of these instead of asking a cabal it was thrown out of every 4 seconds.
    public static func chatClosed(_ error: Error) -> String? {
        switch error {
        case MonacoAPIError.httpStatus(let code, _):
            return chatClosed(status: code, serverMessage: nil)
        case MonacoAPIError.rejected(let code, let message, _):
            return chatClosed(status: code, serverMessage: message)
        default:
            return nil
        }
    }

    /// 403 is the API making a decision about this member, and it means what it says.
    ///
    /// 404 does not. The messages route answers 404 for a missing user and a missing path id
    /// as well as a missing cabal, so reading every 404 as "your cabal is gone" tells a member
    /// their cabal was deleted because a membership read landed on a replica that had not
    /// caught up yet. The body is the only thing that tells them apart — these branches log a
    /// machine-readable reason but do not put it in the response — so match the one that is
    /// about something other than the group and treat it as worth retrying.
    private static func chatClosed(status: Int, serverMessage: String?) -> String? {
        switch status {
        case 403:
            return "You're no longer in this cabal, so its chat is closed to you."
        case 404:
            guard !isAboutTheUser(serverMessage) else { return nil }
            return "This cabal no longer exists."
        default:
            return nil
        }
    }

    private static func isAboutTheUser(_ serverMessage: String?) -> Bool {
        guard let serverMessage else { return false }
        return serverMessage.localizedCaseInsensitiveContains("user not found")
    }

    /// The pill offered to a member reading history when messages land below them.
    public static func newMessagesPill(count: Int) -> String {
        count == 1 ? "1 new message" : "\(count) new messages"
    }
}
