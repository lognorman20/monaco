#if DEBUG
import SwiftUI
import MonacoCore

/// Debug-only QA harness: opens cabal chat against in-memory sample data, no sign-in or backend.
///
/// Launch arguments (Debug builds only):
/// - `-MonacoChatSampleQA` — show chat with a short sample thread
/// - `-MonacoChatSampleEmpty` — with the above, start with no messages
/// - `-MonacoChatSampleOffline` — with the above, every send fails as if offline
/// - `-MonacoChatSampleBusy` — with the above, a long backlog where another member keeps
///   posting, so a viewer reading history can be tested against the arriving messages
/// - `-MonacoChatSampleClosedBlip` — with the above, two polls in a row answer 403 and then
///   the cabal is readable again: the blip a lagging membership read produces, which must not
///   tell a member they were thrown out of their cabal
/// - `-MonacoChatSampleClosedFirstLoad` — with the above, the *first* load answers 403 and
///   every call after it succeeds: the thread opens closed, and the member has to be left
///   something to tap that gets them back in
/// - `-MonacoChatSampleLoading` — with the above, the first page never arrives, so the thread's
///   loading state stays on screen
enum ChatSampleQA {
    static var isEnabled: Bool { arguments.contains("-MonacoChatSampleQA") }

    private static var arguments: [String] { ProcessInfo.processInfo.arguments }

    static func rootView() -> some View {
        let service = SampleGroupChatService(
            startEmpty: arguments.contains("-MonacoChatSampleEmpty"),
            failSends: arguments.contains("-MonacoChatSampleOffline"),
            busy: arguments.contains("-MonacoChatSampleBusy"),
            closedListCalls: closedListCalls,
            closedFirstLoad: arguments.contains("-MonacoChatSampleClosedFirstLoad"),
            neverAnswers: arguments.contains("-MonacoChatSampleLoading")
        )
        return NavigationStack {
            OpensOnceActive {
                GroupChatView(groupId: SampleGroupChatService.groupId, groupName: "Weekend investors") { service }
            }
        }
    }

    /// How many list calls after the first answer 403. One short of the run the screen needs
    /// before it believes a thread is closed, or one exactly equal to it.
    private static var closedListCalls: Int {
        if arguments.contains("-MonacoChatSampleClosedBlip") {
            return GroupChatClosureTracker.pollsBeforeClosing - 1
        }
        return 0
    }
}

/// Puts the chat on screen once the app is in the foreground, which is how the product always
/// reaches it: pushed from the cabal screen of an app that is already running.
///
/// Launched straight into the chat, the app's own activation lands after the first load and
/// reads to the screen as the member coming back to the app — which it answers, on a closed
/// thread, by asking the server again. A first load refused on purpose was then retried before
/// anyone could see it, and the closed-on-first-load scenario opened on a readable thread.
private struct OpensOnceActive<Content: View>: View {
    @ViewBuilder let content: () -> Content

    @Environment(\.scenePhase) private var scenePhase
    @State private var isOpen = false

    var body: some View {
        Group {
            if isOpen {
                content()
            } else {
                MonacoTheme.canvas.ignoresSafeArea()
            }
        }
        .onChange(of: scenePhase, initial: true) { _, phase in
            if phase == .active { isOpen = true }
        }
    }
}

private actor SampleGroupChatService: GroupChatService {
    static let groupId = "00000000-0000-4000-8000-000000000166"

    /// The 403 the chat routes answer for a member who is out of the cabal. Spelled with the
    /// module on purpose: the app has a `MonacoAPIError` of its own, and a bare name picks that
    /// one, which `GroupChatCopy.chatClosed` does not recognise. The closed scenarios then never
    /// closed anything — the first load read as a generic failure and the blip rode out because
    /// no answer counted as closed at all, not because the tracker waited for a run of them.
    static let closed = MonacoCore.MonacoAPIError.httpStatus(403)

    private var messages: [GroupMessageDTO]
    private let failSends: Bool
    /// Another member posting while the viewer reads. Off unless `-MonacoChatSampleBusy`.
    private let busy: Bool
    private var listCalls = 0
    /// List calls after the first that answer 403 before the cabal becomes readable again.
    private let closedListCalls: Int
    private var closedAnswersGiven = 0
    /// The first load answers 403, so the screen opens in its closed state.
    private let closedFirstLoad: Bool
    /// No page ever arrives: the loading state, held.
    private let neverAnswers: Bool

    init(
        startEmpty: Bool,
        failSends: Bool,
        busy: Bool = false,
        closedListCalls: Int = 0,
        closedFirstLoad: Bool = false,
        neverAnswers: Bool = false
    ) {
        self.failSends = failSends
        self.busy = busy
        self.closedListCalls = closedListCalls
        self.closedFirstLoad = closedFirstLoad
        self.neverAnswers = neverAnswers
        guard !startEmpty else {
            messages = []
            return
        }
        let now = Date()
        func at(_ minutesAgo: Double) -> String { Self.stamp(now.addingTimeInterval(-minutesAgo * 60)) }
        func msg(_ id: String, _ who: String, _ name: String, _ body: String, _ minutesAgo: Double) -> GroupMessageDTO {
            .init(id: id, groupId: Self.groupId, authorId: who, authorName: name, body: body, createdAt: at(minutesAgo), mine: who == "u-me")
        }
        messages = [
            msg("s1", "u-ana", "Ana", "Apple reports Thursday. Anyone want in before?", 1_210),
            msg("s2", "u-ana", "Ana", "Thinking $50 from the pot.", 1_209),
            msg("s3", "u-leo", "Leo", "Tesla instead? Or split it.", 1_195),
            msg("s4", "u-me", "You", "I'd rather do Apple first. Smaller swings for our first buy.", 1_190),
            msg("s5", "u-mia", "Mia", "Agree. Nvidia can be next.", 95),
            msg("s6", "u-ana", "Ana", "Proposing Apple now. $50.", 12),
            msg("s7", "u-me", "You", "Voted yes.", 9),
            msg("s8", "u-me", "You", "Leo, you're the last vote.", 9),
        ]
        guard busy else { return }
        // A thread tall enough that the viewer can scroll away from the bottom, which is
        // the whole point: a message arriving must not drag them back down.
        let filler = (1...40).map { index in
            msg("b\(index)", "u-leo", "Leo", "Backlog line \(index) about the Apple buy.", 1_180 - Double(index))
        }
        messages.insert(contentsOf: filler, at: 4)
        messages.sort { $0.createdAt < $1.createdAt }
    }

    func listGroupMessages(groupId: String, before: String?, limit: Int) async throws -> GroupMessagesPageDTO {
        if neverAnswers {
            try await Task.sleep(for: .seconds(3600))
        }
        if before == nil {
            listCalls += 1
            // Only the first load, so the member's retry finds the cabal readable again.
            if closedFirstLoad, listCalls == 1 {
                throw Self.closed
            }
            // Otherwise the first load lands, so the thread is on screen and the closed state
            // shows as the banner over it rather than as a whole-screen error.
            if listCalls > 1, closedAnswersGiven < closedListCalls {
                closedAnswersGiven += 1
                throw Self.closed
            }
            // Not on the first load: the test needs to get itself scrolled up first.
            if busy, listCalls > 1 {
                messages.append(
                    GroupMessageDTO(
                        id: "incoming-\(listCalls)",
                        groupId: Self.groupId,
                        authorId: "u-ana",
                        authorName: "Ana",
                        body: "Still thinking about Thursday (\(listCalls)).",
                        createdAt: Self.stamp(Date()),
                        mine: false
                    )
                )
            }
        }
        // Paged the way the API pages: newest first, `before` an exclusive cursor on the
        // timestamp, and `nextCursor` present only while older messages remain. Returning the
        // whole thread with no cursor meant `hasOlder` was never true, so "Load earlier" —
        // and the place-keeping behind it — could not be reached from the harness at all.
        let newestFirst = messages.reversed().filter { message in
            guard let before else { return true }
            return message.createdAt < before
        }
        let page = Array(newestFirst.prefix(limit))
        let hasOlder = newestFirst.count > page.count
        return GroupMessagesPageDTO(messages: page, nextCursor: hasOlder ? page.last?.createdAt : nil)
    }

    func postGroupMessage(groupId: String, body: String) async throws -> GroupMessageDTO {
        try await Task.sleep(for: .milliseconds(300))
        if failSends { throw URLError(.notConnectedToInternet) }
        let message = GroupMessageDTO(
            id: "sent-\(messages.count + 1)",
            groupId: groupId,
            authorId: "u-me",
            authorName: "You",
            body: body,
            createdAt: Self.stamp(Date()),
            mine: true
        )
        messages.append(message)
        return message
    }

    /// Same fixed-width microsecond UTC shape the API returns.
    private static func stamp(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(identifier: "UTC")
        formatter.dateFormat = "yyyy-MM-dd'T'HH:mm:ss.SSS'000Z'"
        return formatter.string(from: date)
    }
}
#endif
