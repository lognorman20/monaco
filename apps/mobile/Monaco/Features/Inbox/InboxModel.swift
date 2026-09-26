import Foundation
import MonacoCore
import Observation

/// The inbox reads and writes the model depends on, so it runs against the API or canned data.
@MainActor
protocol InboxSource: AnyObject {
    func page(cursor: String?, limit: Int) async throws -> NotificationsPageDTO
    func markRead(ids: [String]) async throws -> UnreadCountDTO
    func markAllRead() async throws -> UnreadCountDTO
}

/// The inbox over the API, authenticated with the Privy session token.
@MainActor
final class LiveInboxSource: InboxSource {
    private let client: MonacoCore.MonacoAPIClient

    init(auth: PrivyAuthService) {
        client = MonacoCore.MonacoAPIClient(
            baseURL: Config.apiBaseURL,
            accessTokenProvider: SessionTokenReader.provider(for: auth)
        )
    }

    func page(cursor: String?, limit: Int) async throws -> NotificationsPageDTO {
        try await client.listNotifications(cursor: cursor, limit: limit)
    }

    func markRead(ids: [String]) async throws -> UnreadCountDTO {
        try await client.markNotificationsRead(ids: ids)
    }

    func markAllRead() async throws -> UnreadCountDTO {
        try await client.markAllNotificationsRead()
    }
}

/// What the inbox shows and the bell counts. One instance per Home, shared by the bell and the
/// inbox screen, so the list opens on what the bell already knew and a row marked read drops
/// the badge at once.
///
/// Reads the member asked for (first load, Try again, pull to refresh) report failure; the
/// background poll never does. A read the member did not ask for must not put an error on screen.
@Observable
@MainActor
final class InboxModel {
    enum Phase: Equatable {
        case loading
        case loaded
        case failed
    }

    static let pageSize = 30

    private(set) var phase: Phase = .loading
    private(set) var items: [NotificationDTO] = []
    private(set) var unreadCount = 0
    /// The cursor for the next older page; nil once the oldest row is on screen.
    private(set) var nextCursor: String?
    private(set) var isLoadingOlder = false
    /// The last "load older" failed; the list offers a button instead of loading on scroll.
    private(set) var olderFailed = false

    private let source: InboxSource
    private let clock: () -> Date
    /// Whether pages past the first are on screen, so a refresh of the first page keeps their cursor.
    private var hasOlderPages = false

    init(source: InboxSource, clock: @escaping () -> Date = Date.init) {
        self.source = source
        self.clock = clock
    }

    var sections: [InboxSection] {
        InboxGrouping.sections(items, now: clock())
    }

    /// First load, and Try again. Shows the skeleton while nothing is on screen.
    func load() async {
        if items.isEmpty { phase = .loading }
        do {
            apply(firstPage: try await source.page(cursor: nil, limit: Self.pageSize))
            phase = .loaded
        } catch {
            if error.isRequestCancellation { return }
            if items.isEmpty { phase = .failed }
        }
    }

    /// Pull to refresh. False when it failed with rows already on screen, which the screen
    /// reports with a toast (with nothing on screen the failed state says it).
    @discardableResult
    func refresh() async -> Bool {
        do {
            apply(firstPage: try await source.page(cursor: nil, limit: Self.pageSize))
            phase = .loaded
            return true
        } catch {
            if error.isRequestCancellation { return true }
            if items.isEmpty { phase = .failed }
            return items.isEmpty
        }
    }

    /// The background tick: the bell's count and the newest rows. Throws so the poll backs off;
    /// never changes what is on screen when it fails.
    func poll() async throws {
        let page = try await source.page(cursor: nil, limit: Self.pageSize)
        apply(firstPage: page)
        if phase != .loaded { phase = .loaded }
    }

    /// The next older page, when the list reaches its end.
    func loadOlder() async {
        guard let cursor = nextCursor, !isLoadingOlder else { return }
        isLoadingOlder = true
        defer { isLoadingOlder = false }
        do {
            let page = try await source.page(cursor: cursor, limit: Self.pageSize)
            let known = Set(items.map(\.id))
            items += page.notifications.filter { !known.contains($0.id) }
            nextCursor = page.nextCursor
            unreadCount = page.unreadCount
            hasOlderPages = true
            olderFailed = false
        } catch {
            if error.isRequestCancellation { return }
            olderFailed = true
        }
    }

    /// The member opened a row: it reads as read at once, and the server hears about it. A write
    /// that fails is left to the next poll to put right; it is not worth an error.
    func open(_ notification: NotificationDTO) async {
        guard notification.isUnread, let index = items.firstIndex(where: { $0.id == notification.id }) else { return }
        items[index] = items[index].markedRead(at: clock())
        unreadCount = max(0, unreadCount - 1)
        if let answer = try? await source.markRead(ids: [notification.id]) {
            unreadCount = answer.unreadCount
        }
    }

    /// A push the member tapped: the row may not be on screen yet, so only the server is told.
    func markRead(id: String) async {
        if let index = items.firstIndex(where: { $0.id == id }), items[index].isUnread {
            items[index] = items[index].markedRead(at: clock())
            unreadCount = max(0, unreadCount - 1)
        }
        if let answer = try? await source.markRead(ids: [id]) {
            unreadCount = answer.unreadCount
        }
    }

    /// Mark all read. Everything clears at once and comes back if the server refuses, which the
    /// screen reports: the member asked for this.
    func markAllRead() async -> Bool {
        guard unreadCount > 0 || items.contains(where: \.isUnread) else { return true }
        let previousItems = items
        let previousCount = unreadCount
        let now = clock()
        items = items.map { $0.markedRead(at: now) }
        unreadCount = 0
        do {
            unreadCount = try await source.markAllRead().unreadCount
            return true
        } catch {
            items = previousItems
            unreadCount = previousCount
            return false
        }
    }

    private func apply(firstPage page: NotificationsPageDTO) {
        let merged = InboxGrouping.merge(fresh: page.notifications, over: items)
        QuietUpdate.apply(merged, over: items) { items = $0 }
        QuietUpdate.apply(page.unreadCount, over: unreadCount) { unreadCount = $0 }
        if !hasOlderPages {
            QuietUpdate.apply(page.nextCursor, over: nextCursor) { nextCursor = $0 }
        }
    }
}

/// Reads the signed-in member's access token for API calls made from `@Sendable` closures,
/// without keeping the auth service alive or capturing a mutable weak variable.
enum SessionTokenReader {
    static func provider(for auth: PrivyAuthService) -> AccessTokenProvider {
        let reference = WeakAuthReference(auth)
        return { await MainActor.run { reference.auth?.accessToken } }
    }
}

/// Only ever read on the main actor, inside `MainActor.run`.
private final class WeakAuthReference: @unchecked Sendable {
    weak var auth: PrivyAuthService?

    init(_ auth: PrivyAuthService) {
        self.auth = auth
    }
}
