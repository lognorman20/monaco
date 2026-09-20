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
enum ChatSampleQA {
    static var isEnabled: Bool { arguments.contains("-MonacoChatSampleQA") }

    private static var arguments: [String] { ProcessInfo.processInfo.arguments }

    static func rootView() -> some View {
        let service = SampleGroupChatService(
            startEmpty: arguments.contains("-MonacoChatSampleEmpty"),
            failSends: arguments.contains("-MonacoChatSampleOffline"),
            busy: arguments.contains("-MonacoChatSampleBusy")
        )
        return NavigationStack {
            GroupChatView(groupId: SampleGroupChatService.groupId, groupName: "Weekend investors") { service }
        }
    }
}

private actor SampleGroupChatService: GroupChatService {
    static let groupId = "00000000-0000-4000-8000-000000000166"

    private var messages: [GroupMessageDTO]
    private let failSends: Bool
    /// Another member posting while the viewer reads. Off unless `-MonacoChatSampleBusy`.
    private let busy: Bool
    private var listCalls = 0

    init(startEmpty: Bool, failSends: Bool, busy: Bool = false) {
        self.failSends = failSends
        self.busy = busy
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
        if before == nil {
            listCalls += 1
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
        return GroupMessagesPageDTO(messages: messages.reversed())
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
