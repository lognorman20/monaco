import MonacoCore
import Observation
import SwiftUI
import UserNotifications

/// Where Home's stack is sent by the bell or by a tapped push.
enum InboxRoute: Hashable {
    case inbox
    case destination(NotificationDestination)
}

/// Home's route, shared with the bell through the environment so the bell can sit in Home's own
/// toolbar next to the profile button.
@Observable
@MainActor
final class InboxNavigator {
    var route: InboxRoute?
}

extension View {
    /// Home's door to the inbox: the model the bell counts, the pushes onto Home's stack, and
    /// the tapped push that should open something. Applied once, on `HomeView`.
    func inboxEntry(auth: PrivyAuthService, selectedTab: Binding<MainTab>) -> some View {
        modifier(InboxHomeEntry(auth: auth, selectedTab: selectedTab))
    }
}

private struct InboxHomeEntry: ViewModifier {
    @ObservedObject var auth: PrivyAuthService
    @Binding var selectedTab: MainTab

    @State private var model: InboxModel
    @State private var navigator = InboxNavigator()
    private let router = PushRouter.shared

    init(auth: PrivyAuthService, selectedTab: Binding<MainTab>) {
        self.auth = auth
        _selectedTab = selectedTab
        _model = State(initialValue: InboxModel(source: LiveInboxSource(auth: auth)))
    }

    func body(content: Content) -> some View {
        @Bindable var navigator = navigator
        return content
            .environment(model)
            .environment(navigator)
            .navigationDestination(item: $navigator.route) { route in
                switch route {
                case .inbox:
                    InboxView(auth: auth, model: model, onOpenCabals: { selectedTab = .cabals })
                case .destination(let destination):
                    InboxDestinationView(auth: auth, destination: destination)
                }
            }
            // The bell's count: quiet, on Home's resting cadence, never an error on screen.
            .task(id: auth.sessionIdentity) {
                guard auth.sessionIdentity != nil else { return }
                try? await model.poll()
            }
            .pollWhileVisible(every: LiveRefreshCadence.resting) {
                try await model.poll()
            }
            .onChange(of: router.arrivals) { _, _ in
                Task { try? await model.poll() }
            }
            .onChange(of: model.unreadCount) { _, unread in
                // The app icon says what the bell says.
                Task { try? await UNUserNotificationCenter.current().setBadgeCount(unread) }
            }
            .task { openTappedPush() }
            .onChange(of: router.pending) { _, _ in openTappedPush() }
    }

    /// A push the member tapped: bring Home forward and open what it is about.
    private func openTappedPush() {
        guard let tapped = router.take() else { return }
        selectedTab = .home
        if tapped.destination != .none {
            navigator.route = .destination(tapped.destination)
        } else {
            navigator.route = .inbox
        }
        if let id = tapped.notificationId {
            Task { await model.markRead(id: id) }
        }
    }
}

/// The bell in Home's navigation bar, with the unread count on an ink capsule in mono digits.
struct InboxBellButton: View {
    @Environment(InboxModel.self) private var model
    @Environment(InboxNavigator.self) private var navigator

    var body: some View {
        Button {
            Haptics.selection()
            navigator.route = .inbox
        } label: {
            Image(systemName: "bell")
                .font(MonacoTheme.Typo.bodyStrong)
                .imageScale(.large)
                .foregroundStyle(MonacoTheme.ink)
                .frame(width: 44, height: 44)
                .overlay(alignment: .topTrailing) {
                    if let badge = InboxBadge.text(unread: model.unreadCount) {
                        InboxUnreadBadge(text: badge)
                            .offset(x: -2, y: 4)
                    }
                }
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(InboxCopy.bellLabel)
        .accessibilityValue(InboxCopy.bellValue(unread: model.unreadCount))
        .accessibilityIdentifier("home-inbox-bell")
    }
}

/// The unread count: mono digits on an ink capsule, like the count beside "Needs your vote".
struct InboxUnreadBadge: View {
    let text: String

    var body: some View {
        Text(text)
            .font(MonacoTheme.Typo.dataMicro)
            .foregroundStyle(MonacoTheme.onBrand)
            .padding(.horizontal, 5)
            .frame(minWidth: 18, minHeight: 18)
            .background(Capsule().fill(MonacoTheme.brandFill))
            .overlay(Capsule().strokeBorder(MonacoTheme.canvas, lineWidth: 1.5))
            .fixedSize()
            .accessibilityHidden(true)
    }
}
