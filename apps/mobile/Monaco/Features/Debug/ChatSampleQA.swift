#if DEBUG
import SwiftUI
import MonacoCore

/// Debug-only QA harness: opens cabal chat against in-memory sample data, no sign-in or backend.
///
/// Launch arguments (Debug builds only):
/// - `-MonacoChatSampleQA` — show chat with a short sample thread
/// - `-MonacoChatSampleEmpty` — with the above, start with no messages
/// - `-MonacoChatSampleOffline` — with the above, every send fails as if offline
enum ChatSampleQA {
    static var isEnabled: Bool { arguments.contains("-MonacoChatSampleQA") }

    private static var arguments: [String] { ProcessInfo.processInfo.arguments }

    static func rootView() -> some View {
        let service = SampleGroupChatService(
            startEmpty: arguments.contains("-MonacoChatSampleEmpty"),
            failSends: arguments.contains("-MonacoChatSampleOffline")
        )
        return NavigationStack {
            GroupChatView(groupId: SampleGroupChatService.groupId, groupName: "Sample data · Tech Bros") { service }
        }
    }
}

private actor SampleGroupChatService: GroupChatService {
    static let groupId = "00000000-0000-4000-8000-000000000166"

    private var messages: [GroupMessageDTO]
    private let failSends: Bool

    init(startEmpty: Bool, failSends: Bool) {
        self.failSends = failSends
        guard !startEmpty else {
            messages = []
            return
        }
        let now = Date()
        func at(_ minutesAgo: Double) -> String { Self.stamp(now.addingTimeInterval(-minutesAgo * 60)) }
        messages = [
            .init(id: "s1", groupId: Self.groupId, authorId: "u-ana", authorName: "Ana", body: "Apple reports Thursday. Anyone want in before?", createdAt: at(42), mine: false),
            .init(id: "s2", groupId: Self.groupId, authorId: "u-ana", authorName: "Ana", body: "Thinking $50 from the pot.", createdAt: at(41), mine: false),
            .init(id: "s3", groupId: Self.groupId, authorId: "u-me", authorName: "You", body: "I'm in. Propose it and I'll vote yes.", createdAt: at(30), mine: true),
            .init(id: "s4", groupId: Self.groupId, authorId: "u-leo", authorName: "Leo", body: "Tesla instead? Or split it.", createdAt: at(12), mine: false),
        ]
    }

    func listGroupMessages(groupId: String, before: String?, limit: Int) async throws -> GroupMessagesPageDTO {
        GroupMessagesPageDTO(messages: messages.reversed())
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
