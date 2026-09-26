import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// Answers the inbox model from memory, and fails on request.
@MainActor
private final class StubInboxSource: InboxSource {
    var pages: [String?: NotificationsPageDTO] = [:]
    var failReads = false
    var failWrites = false
    private(set) var markedRead: [[String]] = []
    private(set) var markedAll = 0

    struct Failure: Error {}

    func page(cursor: String?, limit: Int) async throws -> NotificationsPageDTO {
        if failReads { throw Failure() }
        return pages[cursor] ?? NotificationsPageDTO(notifications: [], unreadCount: 0)
    }

    func markRead(ids: [String]) async throws -> UnreadCountDTO {
        if failWrites { throw Failure() }
        markedRead.append(ids)
        let unread = (pages[nil]?.unreadCount ?? 1) - 1
        return UnreadCountDTO(unreadCount: max(0, unread))
    }

    func markAllRead() async throws -> UnreadCountDTO {
        if failWrites { throw Failure() }
        markedAll += 1
        return UnreadCountDTO(unreadCount: 0)
    }
}

private let now = Date(timeIntervalSince1970: 1_790_000_000)

private func row(_ id: String, minutesAgo: Double, unread: Bool = true) -> NotificationDTO {
    NotificationDTO(
        id: id, kind: NotificationKind.chatMessage, title: "t\(id)", groupId: "g",
        readAt: unread ? nil : now, createdAt: now.addingTimeInterval(-minutesAgo * 60)
    )
}

@MainActor
struct InboxModelTests {
    @Test func firstLoadShowsRowsAndTheUnreadCount() async {
        let source = StubInboxSource()
        source.pages[nil] = NotificationsPageDTO(notifications: [row("a", minutesAgo: 1), row("b", minutesAgo: 2, unread: false)], unreadCount: 1, nextCursor: "c1")
        let model = InboxModel(source: source, clock: { now })

        await model.load()

        #expect(model.phase == .loaded)
        #expect(model.items.map(\.id) == ["a", "b"])
        #expect(model.unreadCount == 1)
        #expect(model.nextCursor == "c1")
    }

    @Test func aFailedFirstLoadIsTheFailedState() async {
        let source = StubInboxSource()
        source.failReads = true
        let model = InboxModel(source: source, clock: { now })

        await model.load()

        #expect(model.phase == .failed)
        #expect(model.items.isEmpty)
    }

    @Test func aFailedPollLeavesTheScreenAlone() async throws {
        let source = StubInboxSource()
        source.pages[nil] = NotificationsPageDTO(notifications: [row("a", minutesAgo: 1)], unreadCount: 1)
        let model = InboxModel(source: source, clock: { now })
        await model.load()
        source.failReads = true

        await #expect(throws: StubInboxSource.Failure.self) { try await model.poll() }

        #expect(model.phase == .loaded)
        #expect(model.items.map(\.id) == ["a"])
        #expect(model.unreadCount == 1)
    }

    @Test func aFailedRefreshWithRowsOnScreenAsksForAToast() async {
        let source = StubInboxSource()
        source.pages[nil] = NotificationsPageDTO(notifications: [row("a", minutesAgo: 1)], unreadCount: 1)
        let model = InboxModel(source: source, clock: { now })
        await model.load()
        source.failReads = true

        let succeeded = await model.refresh()

        #expect(succeeded == false)
        #expect(model.items.count == 1)
    }

    @Test func olderPagesAppendAndSurviveARefreshOfTheFirst() async throws {
        let source = StubInboxSource()
        source.pages[nil] = NotificationsPageDTO(notifications: [row("a", minutesAgo: 1), row("b", minutesAgo: 2)], unreadCount: 2, nextCursor: "c1")
        source.pages["c1"] = NotificationsPageDTO(notifications: [row("c", minutesAgo: 60), row("b", minutesAgo: 2)], unreadCount: 2, nextCursor: nil)
        let model = InboxModel(source: source, clock: { now })
        await model.load()

        await model.loadOlder()
        source.pages[nil] = NotificationsPageDTO(notifications: [row("new", minutesAgo: 0), row("a", minutesAgo: 1), row("b", minutesAgo: 2)], unreadCount: 3, nextCursor: "c-new")
        try await model.poll()

        #expect(model.items.map(\.id) == ["new", "a", "b", "c"])
        #expect(model.nextCursor == nil, "the oldest page was reached; a refresh of the first must not bring its cursor back")
        #expect(model.unreadCount == 3)
    }

    @Test func aFailedOlderPageOffersAButton() async {
        let source = StubInboxSource()
        source.pages[nil] = NotificationsPageDTO(notifications: [row("a", minutesAgo: 1)], unreadCount: 0, nextCursor: "c1")
        let model = InboxModel(source: source, clock: { now })
        await model.load()
        source.failReads = true

        await model.loadOlder()

        #expect(model.olderFailed)
        #expect(model.nextCursor == "c1")
    }

    @Test func openingARowMarksItReadAtOnce() async {
        let source = StubInboxSource()
        source.pages[nil] = NotificationsPageDTO(notifications: [row("a", minutesAgo: 1), row("b", minutesAgo: 2)], unreadCount: 2)
        let model = InboxModel(source: source, clock: { now })
        await model.load()

        await model.open(model.items[0])

        #expect(model.items[0].isUnread == false)
        #expect(model.unreadCount == 1)
        #expect(source.markedRead == [["a"]])
    }

    @Test func openingARowThatFailsToSaveStaysRead() async {
        let source = StubInboxSource()
        source.pages[nil] = NotificationsPageDTO(notifications: [row("a", minutesAgo: 1)], unreadCount: 1)
        let model = InboxModel(source: source, clock: { now })
        await model.load()
        source.failWrites = true

        await model.open(model.items[0])

        #expect(model.items[0].isUnread == false)
        #expect(model.unreadCount == 0)
    }

    @Test func markAllReadClearsAndComesBackWhenRefused() async {
        let source = StubInboxSource()
        source.pages[nil] = NotificationsPageDTO(notifications: [row("a", minutesAgo: 1), row("b", minutesAgo: 2)], unreadCount: 2)
        let model = InboxModel(source: source, clock: { now })
        await model.load()
        source.failWrites = true

        let refused = await model.markAllRead()

        #expect(refused == false)
        #expect(model.unreadCount == 2)
        let allUnread = model.items.allSatisfy { $0.isUnread }
        #expect(allUnread)

        source.failWrites = false
        let accepted = await model.markAllRead()

        #expect(accepted)
        #expect(model.unreadCount == 0)
        let noneUnread = model.items.allSatisfy { !$0.isUnread }
        #expect(noneUnread)
        #expect(source.markedAll == 1)
    }
}

struct PushPromptGateTests {
    @Test(arguments: [
        (Int?.none, Int?.some(2), PushPermissionStatus.notDetermined, false, false),  // first read at launch
        (Int?.some(0), Int?.some(1), PushPermissionStatus.notDetermined, false, true), // first cabal
        (Int?.some(2), Int?.some(3), PushPermissionStatus.notDetermined, false, true), // another cabal, never asked
        (Int?.some(1), Int?.some(1), PushPermissionStatus.notDetermined, false, false), // nothing joined
        (Int?.some(2), Int?.some(1), PushPermissionStatus.notDetermined, false, false), // left one
        (Int?.some(0), Int?.some(1), PushPermissionStatus.notDetermined, true, false),  // already asked
        (Int?.some(0), Int?.some(1), PushPermissionStatus.authorized, false, false),    // already on
        (Int?.some(0), Int?.some(1), PushPermissionStatus.denied, false, false),        // said no in Settings
    ])
    func asksOnlyAfterAJoinWhenNothingHasBeenAsked(previous: Int?, joined: Int?, status: PushPermissionStatus, asked: Bool, expected: Bool) {
        #expect(PushPromptGate.shouldAsk(previousJoined: previous, joined: joined, status: status, alreadyAsked: asked) == expected)
    }
}
