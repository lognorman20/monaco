import Foundation

/// One inbox row from `GET /v1/me/notifications`. The title and body are the server's words,
/// the same ones the push carried; the ids say where a tap goes.
public struct NotificationDTO: Codable, Equatable, Sendable, Identifiable {
    public let id: String
    public let kind: String
    /// `proposals`, `results`, `chat` or `money`: the preference that switches this kind off.
    public let category: String?
    public let title: String
    public let body: String
    public let groupId: String?
    public let groupName: String?
    public let groupPictureUrl: String?
    public let proposalId: String?
    public let transactionId: String?
    /// The stock a buy or sell was in.
    public let symbol: String?
    public let readAt: Date?
    public let createdAt: Date

    public init(
        id: String,
        kind: String,
        category: String? = nil,
        title: String,
        body: String = "",
        groupId: String? = nil,
        groupName: String? = nil,
        groupPictureUrl: String? = nil,
        proposalId: String? = nil,
        transactionId: String? = nil,
        symbol: String? = nil,
        readAt: Date? = nil,
        createdAt: Date
    ) {
        self.id = id
        self.kind = kind
        self.category = category
        self.title = title
        self.body = body
        self.groupId = groupId
        self.groupName = groupName
        self.groupPictureUrl = groupPictureUrl
        self.proposalId = proposalId
        self.transactionId = transactionId
        self.symbol = symbol
        self.readAt = readAt
        self.createdAt = createdAt
    }

    public var isUnread: Bool { readAt == nil }

    /// This row as it reads once the member has seen it.
    public func markedRead(at date: Date) -> NotificationDTO {
        guard isUnread else { return self }
        return NotificationDTO(
            id: id, kind: kind, category: category, title: title, body: body,
            groupId: groupId, groupName: groupName, groupPictureUrl: groupPictureUrl,
            proposalId: proposalId, transactionId: transactionId, symbol: symbol,
            readAt: date, createdAt: createdAt
        )
    }
}

/// One newest-first page of the inbox with the member's unread count.
public struct NotificationsPageDTO: Codable, Equatable, Sendable {
    public let notifications: [NotificationDTO]
    public let unreadCount: Int
    /// Pass back as `cursor` for the next, older page. Nil on the last page.
    public let nextCursor: String?

    public init(notifications: [NotificationDTO], unreadCount: Int, nextCursor: String? = nil) {
        self.notifications = notifications
        self.unreadCount = unreadCount
        self.nextCursor = nextCursor
    }
}

/// `POST /v1/me/notifications/read` answers with what is left unread.
public struct UnreadCountDTO: Codable, Equatable, Sendable {
    public let unreadCount: Int

    public init(unreadCount: Int) {
        self.unreadCount = unreadCount
    }
}

/// `POST /v1/proposals/{id}/nudge`: how many members the reminder reached, and how many
/// voters (the sender aside) still have not voted.
public struct NudgeResultDTO: Codable, Equatable, Sendable {
    public let reminded: Int
    public let waitingOn: Int

    public init(reminded: Int, waitingOn: Int) {
        self.reminded = reminded
        self.waitingOn = waitingOn
    }
}

/// Which build registered a device, so the server pushes through the matching APNs host.
public enum DeviceAppEnv: String, Codable, Sendable {
    case debug
    case production
}

/// The kinds the server sends. Unknown kinds still render: they fall back to the cabal's mark
/// or a bell, and a tap goes wherever the ids say.
public enum NotificationKind {
    public static let proposalCreated = "proposal_created"
    public static let proposalExpiring = "proposal_expiring"
    public static let proposalNudge = "proposal_nudge"
    public static let joinRequest = "join_request"
    public static let proposalPassed = "proposal_passed"
    public static let proposalFailed = "proposal_failed"
    public static let proposalExpired = "proposal_expired"
    public static let tradeBought = "trade_bought"
    public static let tradeSold = "trade_sold"
    public static let botTrade = "bot_trade"
    public static let memberJoined = "member_joined"
    public static let joinApproved = "join_approved"
    public static let chatMessage = "chat_message"
    public static let fundsArrived = "funds_arrived"
    public static let fundCredited = "fund_credited"
    public static let cashOutSettled = "cash_out_settled"

    static let trades: Set<String> = [tradeBought, tradeSold, botTrade]
    static let money: Set<String> = [fundsArrived, fundCredited, cashOutSettled]
}

/// Where tapping a notification goes: the proposal when there is one (a buy's whole story is
/// there), else the trade's receipt, else the cabal. A notification about the member's own
/// balance points nowhere; the tap only marks it read.
public enum NotificationDestination: Hashable, Sendable {
    case proposal(id: String)
    case transaction(id: String, isSell: Bool)
    case cabal(id: String, name: String?)
    case none

    public static func of(_ notification: NotificationDTO) -> NotificationDestination {
        if let proposalId = notification.proposalId.nonBlank {
            return .proposal(id: proposalId)
        }
        if let transactionId = notification.transactionId.nonBlank {
            return .transaction(id: transactionId, isSell: notification.kind == NotificationKind.tradeSold)
        }
        if let groupId = notification.groupId.nonBlank {
            return .cabal(id: groupId, name: notification.groupName.nonBlank)
        }
        return .none
    }

    /// Push payloads carry only `groupId` and `proposalId`.
    public static func fromPush(groupId: String?, proposalId: String?) -> NotificationDestination {
        if let proposalId = proposalId.nonBlank { return .proposal(id: proposalId) }
        if let groupId = groupId.nonBlank { return .cabal(id: groupId, name: nil) }
        return .none
    }
}

/// The mark on the left of an inbox row: the stock's coin for a buy or sell, the dollar coin for
/// the member's own money, the cabal's mark for anything else in a cabal.
public enum NotificationMark: Equatable, Sendable {
    case stock(symbol: String)
    case money
    case cabal(groupId: String, name: String, pictureUrl: String?)
    case bell

    public static func of(_ notification: NotificationDTO) -> NotificationMark {
        if NotificationKind.trades.contains(notification.kind), let symbol = notification.symbol.nonBlank {
            return .stock(symbol: symbol)
        }
        if NotificationKind.money.contains(notification.kind) || notification.category == "money" {
            return .money
        }
        if let groupId = notification.groupId.nonBlank {
            return .cabal(groupId: groupId, name: notification.groupName ?? "", pictureUrl: notification.groupPictureUrl.nonBlank)
        }
        return .bell
    }
}

/// "Today" and "Earlier", newest first inside each. An empty group is left out.
public struct InboxSection: Equatable, Identifiable, Sendable {
    public let title: String
    public let items: [NotificationDTO]
    public var id: String { title }
}

public enum InboxGrouping {
    public static func sections(_ items: [NotificationDTO], now: Date, calendar: Calendar = .current) -> [InboxSection] {
        let sorted = items.sorted { lhs, rhs in
            lhs.createdAt != rhs.createdAt ? lhs.createdAt > rhs.createdAt : lhs.id > rhs.id
        }
        let today = sorted.filter { calendar.isDate($0.createdAt, inSameDayAs: now) }
        let earlier = sorted.filter { !calendar.isDate($0.createdAt, inSameDayAs: now) }
        var sections: [InboxSection] = []
        if !today.isEmpty { sections.append(InboxSection(title: InboxCopy.today, items: today)) }
        if !earlier.isEmpty { sections.append(InboxSection(title: InboxCopy.earlier, items: earlier)) }
        return sections
    }

    /// Merges a fresh first page over what is on screen: fresh rows win, older pages stay.
    public static func merge(fresh: [NotificationDTO], over current: [NotificationDTO]) -> [NotificationDTO] {
        let freshIDs = Set(fresh.map(\.id))
        guard let oldestFresh = fresh.map(\.createdAt).min() else { return current }
        let olderKept = current.filter { !freshIDs.contains($0.id) && $0.createdAt < oldestFresh }
        return fresh + olderKept
    }
}

/// How long ago, in the stamp's few characters: "now", "12m", "3h", "2d", then the date.
public enum NotificationAge {
    public static func label(for date: Date, now: Date, calendar: Calendar = .current) -> String {
        let seconds = max(0, now.timeIntervalSince(date))
        switch seconds {
        case ..<60:
            return "now"
        case ..<3_600:
            return "\(Int(seconds / 60))m"
        case ..<86_400:
            return "\(Int(seconds / 3_600))h"
        case ..<(7 * 86_400):
            return "\(Int(seconds / 86_400))d"
        default:
            let sameYear = calendar.component(.year, from: date) == calendar.component(.year, from: now)
            return date.formatted(sameYear
                ? .dateTime.month(.abbreviated).day()
                : .dateTime.month(.abbreviated).day().year())
        }
    }

    /// The same, spoken: "12 minutes ago".
    public static func accessibilityLabel(for date: Date, now: Date) -> String {
        let seconds = max(0, now.timeIntervalSince(date))
        switch seconds {
        case ..<60:
            return "Just now"
        case ..<3_600:
            return spoken(Int(seconds / 60), "minute")
        case ..<86_400:
            return spoken(Int(seconds / 3_600), "hour")
        case ..<(7 * 86_400):
            return spoken(Int(seconds / 86_400), "day")
        default:
            return date.formatted(date: .abbreviated, time: .omitted)
        }
    }

    private static func spoken(_ n: Int, _ unit: String) -> String {
        n == 1 ? "1 \(unit) ago" : "\(n) \(unit)s ago"
    }
}

/// The unread badge on the bell: mono digits, "99+" past two of them.
public enum InboxBadge {
    public static func text(unread: Int) -> String? {
        guard unread > 0 else { return nil }
        return unread > 99 ? "99+" : "\(unread)"
    }
}

/// APNs hands the device token over as bytes; the API wants lowercase hex.
public enum DeviceTokenFormatter {
    public static func hex(_ token: Data) -> String {
        token.map { String(format: "%02x", $0) }.joined()
    }
}

/// When the proposal screen offers "Remind them": the vote is open, the viewer proposed it or
/// already voted, and someone else still owes a ballot. Mirrors the server's rule, so the
/// button never offers a reminder the server would refuse.
public enum ProposalNudgeRule {
    public static func showsRemind(proposal: ProposalDTO, viewerId: String?, viewerChoice: String?, now: Date = Date()) -> Bool {
        guard proposal.isOpen else { return false }
        if let raw = proposal.expiresAt, let expires = SharedFormatters.iso8601Date(from: raw), expires <= now {
            return false
        }
        guard let summary = proposal.voteSummary else { return false }
        let viewerVoted = viewerChoice != nil
            || (viewerId.map { id in proposal.votes?.contains { $0.voterId == id } ?? false } ?? false)
        let viewerProposed = viewerId != nil && viewerId == proposal.proposerId
        guard viewerVoted || viewerProposed else { return false }
        let cast = summary.yesCount + summary.noCount
        let othersWaiting = summary.eligibleCount - cast - (viewerVoted ? 0 : 1)
        return othersWaiting > 0
    }
}

/// Every word the inbox, the bell, the permission ask and the reminder button say.
public enum InboxCopy {
    public static let title = "Inbox"
    public static let markAllRead = "Mark all read"
    public static let today = "Today"
    public static let earlier = "Earlier"
    public static let emptyTitle = "Nothing yet"
    public static let emptyMessage = "When friends propose or vote, it lands here."
    public static let loadFailedTitle = "Couldn't load your inbox"
    public static let loadFailedMessage = "Check your connection and try again."
    public static let tryAgain = "Try again"
    public static let refreshFailed = "Couldn't refresh just now"
    public static let markReadFailed = "Couldn't mark them read"
    public static let loadMore = "Show older"
    public static let unreadDot = "Unread"
    public static let openCabals = "See your cabals"

    public static let bellLabel = "Inbox"
    public static func bellValue(unread: Int) -> String {
        unread == 0 ? "Nothing new" : (unread == 1 ? "1 unread" : "\(unread) unread")
    }

    public static let permissionTitle = "Friends will wait on your vote"
    public static let permissionMessage = "Hear when a friend proposes, when a vote is an hour from closing, and when your cabal buys."
    public static let permissionTurnOn = "Turn on"
    public static let permissionNotNow = "Not now"
    public static let permissionDeniedTitle = "Notifications are off"
    public static let permissionDeniedMessage = "Turn them on in Settings to hear when friends propose or vote."
    public static let permissionOpenSettings = "Open Settings"

    public static let remindThem = "Remind them"
    public static let remindSent = "Reminder sent"
    public static func reminded(_ result: NudgeResultDTO) -> String {
        if result.waitingOn == 0 { return "Everyone has voted" }
        if result.reminded == 0 { return "They've switched reminders off" }
        return result.reminded == 1 ? "Reminded 1 member" : "Reminded \(result.reminded) members"
    }
    public static let remindTooSoon = "They were reminded less than an hour ago"
    public static let remindVoteFirst = "Vote first to remind the others"
    public static let remindClosed = "Voting has closed"
    public static let remindFailed = "Couldn't send the reminder"

    public static func remindError(status: Int?) -> String {
        switch status {
        case 429: remindTooSoon
        case 403: remindVoteFirst
        case 409: remindClosed
        default: remindFailed
        }
    }

    /// For the copy audit.
    public static let all: [String] = [
        title, markAllRead, today, earlier, emptyTitle, emptyMessage, loadFailedTitle, loadFailedMessage,
        tryAgain, refreshFailed, markReadFailed, loadMore, unreadDot, openCabals, remindSent, bellLabel, bellValue(unread: 0), bellValue(unread: 3),
        permissionTitle, permissionMessage, permissionTurnOn, permissionNotNow, permissionDeniedTitle,
        permissionDeniedMessage, permissionOpenSettings, remindThem,
        reminded(NudgeResultDTO(reminded: 2, waitingOn: 3)), reminded(NudgeResultDTO(reminded: 1, waitingOn: 1)),
        reminded(NudgeResultDTO(reminded: 0, waitingOn: 0)), reminded(NudgeResultDTO(reminded: 0, waitingOn: 2)),
        remindTooSoon, remindVoteFirst, remindClosed, remindFailed,
    ]
}

private extension Optional where Wrapped == String {
    /// The string trimmed, or nil when it is missing or blank.
    var nonBlank: String? {
        guard let trimmed = self?.trimmingCharacters(in: .whitespacesAndNewlines), !trimmed.isEmpty else { return nil }
        return trimmed
    }
}
