#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: the inbox on canned rows, no backend and no sign-in. Launch with
/// `-MonacoInboxSample <inbox|empty|loading|permission>`. Time is pinned to 2pm today, so the
/// "Today" and "Earlier" split and every age read the same in every screenshot.
enum InboxSampleScenario: String, CaseIterable {
    case inbox
    case empty
    case loading
    case permission

    static var requested: InboxSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: "-MonacoInboxSample"),
              arguments.indices.contains(flag + 1)
        else { return nil }
        return InboxSampleScenario(rawValue: arguments[flag + 1])
    }
}

struct InboxSampleHarness: View {
    let scenario: InboxSampleScenario
    @ObservedObject var auth: PrivyAuthService

    @State private var model: InboxModel
    @State private var showsPrePrompt: Bool

    private static let now: Date = {
        Calendar.current.date(bySettingHour: 14, minute: 0, second: 0, of: Date()) ?? Date()
    }()

    init(scenario: InboxSampleScenario, auth: PrivyAuthService) {
        self.scenario = scenario
        self.auth = auth
        let source = SampleInboxSource(scenario: scenario, now: Self.now)
        _model = State(initialValue: InboxModel(source: source, clock: { Self.now }))
        _showsPrePrompt = State(initialValue: scenario == .permission)
    }

    var body: some View {
        NavigationStack {
            InboxView(
                auth: auth,
                model: model,
                permission: FixedPushPermission(fixed: scenario == .permission ? .notDetermined : .authorized),
                onOpenCabals: {},
                clock: { Self.now }
            )
        }
        .sheet(isPresented: $showsPrePrompt) {
            PushPrePromptSheet(onTurnOn: { showsPrePrompt = false }, onNotNow: { showsPrePrompt = false })
        }
    }

    /// The sample cabals, as the other harnesses name them.
    static func sampleRows(now: Date) -> [NotificationDTO] {
        func ago(_ minutes: Double) -> Date { now.addingTimeInterval(-minutes * 60) }
        return [
            NotificationDTO(
                id: "n1", kind: NotificationKind.proposalCreated, category: "proposals",
                title: "Jordan proposed $250 of Alphabet in Weekend investors", body: "Voting closes in 1 day.",
                groupId: "g1", groupName: "Weekend investors", proposalId: "p1", symbol: "GOOGLx",
                createdAt: ago(4)
            ),
            NotificationDTO(
                id: "n2", kind: NotificationKind.proposalNudge, category: "proposals",
                title: "Priya is waiting on your vote", body: "$40 of Nvidia in Semis or bust. Voting closes in 3 hours.",
                groupId: "g2", groupName: "Semis or bust", proposalId: "p2", symbol: "NVDAx",
                createdAt: ago(38)
            ),
            NotificationDTO(
                id: "n3", kind: NotificationKind.tradeBought, category: "results",
                title: "Weekend investors bought Tesla", body: "$120 from the pot, as voted.",
                groupId: "g1", groupName: "Weekend investors", proposalId: "p3", transactionId: "t3", symbol: "TSLAx",
                createdAt: ago(130)
            ),
            NotificationDTO(
                id: "n4", kind: NotificationKind.chatMessage, category: "chat",
                title: "Sam in Semis or bust", body: "Anyone else think Nvidia runs into earnings?",
                groupId: "g2", groupName: "Semis or bust",
                readAt: ago(150), createdAt: ago(185)
            ),
            NotificationDTO(
                id: "n5", kind: NotificationKind.fundsArrived, category: "money",
                title: "$500 arrived in your balance", body: "Put it into a cabal when you're ready.",
                readAt: ago(200), createdAt: ago(300)
            ),
            NotificationDTO(
                id: "n6", kind: NotificationKind.botTrade, category: "results",
                title: "Scout bought $40 of Nvidia", body: "For Semis or bust, inside the budget the cabal voted.",
                groupId: "g2", groupName: "Semis or bust", transactionId: "t6", symbol: "NVDAx",
                readAt: ago(1_000), createdAt: ago(1_500)
            ),
            NotificationDTO(
                id: "n7", kind: NotificationKind.proposalFailed, category: "results",
                title: "Index huggers voted down selling Apple", body: "Proposed by Ada.",
                groupId: "g3", groupName: "Index huggers", proposalId: "p7", symbol: "AAPLx",
                readAt: ago(2_800), createdAt: ago(2_900)
            ),
            NotificationDTO(
                id: "n8", kind: NotificationKind.memberJoined, category: "results",
                title: "Priya joined Weekend investors", body: "4 members now.",
                groupId: "g1", groupName: "Weekend investors",
                readAt: ago(4_000), createdAt: ago(4_300)
            ),
            NotificationDTO(
                id: "n9", kind: NotificationKind.cashOutSettled, category: "money",
                title: "$120.50 from Index huggers is in your balance", body: "Your cash out went through.",
                groupId: "g3", groupName: "Index huggers",
                readAt: ago(7_000), createdAt: ago(7_200)
            ),
        ]
    }
}

/// Canned inbox answers: rows, nothing, or a read that never finishes.
@MainActor
private final class SampleInboxSource: InboxSource {
    private let scenario: InboxSampleScenario
    private var rows: [NotificationDTO]

    init(scenario: InboxSampleScenario, now: Date) {
        self.scenario = scenario
        rows = scenario == .empty ? [] : InboxSampleHarness.sampleRows(now: now)
    }

    func page(cursor: String?, limit: Int) async throws -> NotificationsPageDTO {
        if scenario == .loading {
            try await Task.sleep(for: .seconds(3_600))
        }
        return NotificationsPageDTO(notifications: rows, unreadCount: rows.filter(\.isUnread).count)
    }

    func markRead(ids: [String]) async throws -> UnreadCountDTO {
        rows = rows.map { ids.contains($0.id) ? $0.markedRead(at: Date()) : $0 }
        return UnreadCountDTO(unreadCount: rows.filter(\.isUnread).count)
    }

    func markAllRead() async throws -> UnreadCountDTO {
        rows = rows.map { $0.markedRead(at: Date()) }
        return UnreadCountDTO(unreadCount: 0)
    }
}
#endif
