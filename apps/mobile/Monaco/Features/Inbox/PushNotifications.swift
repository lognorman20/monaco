import MonacoCore
import Observation
import os
import SwiftUI
import UIKit
import UserNotifications

// Apple push: permission, the device token's trip to the API, and where a tapped push goes.
//
// The order of events is not ours to choose. APNs may hand over a token before anyone is signed
// in, sign-in may finish before the token arrives, and a cold launch from a push delivers the
// tap before any screen exists. So the registrar waits for both halves (a signed-in member and a
// token) before it tells the API, and the router holds a tapped push until Home is there to
// open it.

private let pushLog = Logger(subsystem: "com.monaco.app", category: "push")

// MARK: - App delegate

/// The UIKit half of push, wired in `MonacoApp` through `@UIApplicationDelegateAdaptor`.
final class MonacoAppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        // A member who already said yes gets a fresh token every launch; Apple may rotate it.
        Task { await PushRegistrar.shared.registerIfAllowed() }
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        PushRegistrar.shared.deviceTokenArrived(DeviceTokenFormatter.hex(deviceToken))
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        // The simulator without an Apple account, or no aps-environment entitlement: the inbox
        // still works, there is just nothing to push to.
        pushLog.notice("remote notification registration failed: \(error.localizedDescription, privacy: .public)")
    }

    /// A push that arrives while Monaco is open still shows as a banner, and the bell catches up.
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        await MainActor.run { PushRouter.shared.noteArrival() }
        return [.banner, .list, .sound, .badge]
    }

    /// The member tapped a push: open the proposal or the cabal it is about.
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse
    ) async {
        let info = response.notification.request.content.userInfo
        let groupId = info["groupId"] as? String
        let proposalId = info["proposalId"] as? String
        let notificationId = info["notificationId"] as? String
        await MainActor.run {
            PushRouter.shared.open(
                NotificationDestination.fromPush(groupId: groupId, proposalId: proposalId),
                notificationId: notificationId
            )
        }
    }
}

// MARK: - Router

/// A tapped push waiting for Home to open it, and a counter of pushes that arrived in the foreground.
@Observable
@MainActor
final class PushRouter {
    static let shared = PushRouter()

    struct Pending: Equatable {
        let destination: NotificationDestination
        let notificationId: String?
    }

    private(set) var pending: Pending?
    /// Bumped for each push shown while the app is open, so the bell can re-read.
    private(set) var arrivals = 0

    func open(_ destination: NotificationDestination, notificationId: String?) {
        pending = Pending(destination: destination, notificationId: notificationId)
    }

    /// Home takes the tap it is about to open.
    func take() -> Pending? {
        defer { pending = nil }
        return pending
    }

    func noteArrival() {
        arrivals += 1
    }
}

// MARK: - Registrar

/// Sends this install's APNs token to the API once a member is signed in, again when the token
/// changes or someone else signs in, and takes it back on sign-out so a shared phone stops
/// receiving the last member's votes.
@MainActor
final class PushRegistrar {
    static let shared = PushRegistrar()

    private var deviceToken: String?
    private var identity: String?
    private var client: MonacoCore.MonacoAPIClient?
    /// The newest access token seen while signed in. Sign-out clears the session before this
    /// hears of it, so the unregister call is made with the token the member last held.
    private var lastAccessToken: String?
    /// "identity|token" last registered, so a relaunch or a token rotation does not re-send.
    private var registered: String?

    private let appEnv: DeviceAppEnv = {
        #if DEBUG
        return .debug
        #else
        return .production
        #endif
    }()

    /// Call when who is signed in changes (and once at launch).
    func sessionChanged(identity newIdentity: String?, auth: PrivyAuthService) async {
        guard let newIdentity else {
            if identity != nil { await unregister() }
            identity = nil
            client = nil
            registered = nil
            try? await UNUserNotificationCenter.current().setBadgeCount(0)
            return
        }
        if identity != newIdentity { registered = nil }
        identity = newIdentity
        lastAccessToken = auth.accessToken
        client = MonacoCore.MonacoAPIClient(
            baseURL: Config.apiBaseURL,
            accessTokenProvider: SessionTokenReader.provider(for: auth)
        )
        await registerIfAllowed()
        await sync()
    }

    /// The access token rotated (about hourly); remember the newest for sign-out.
    func accessTokenChanged(_ token: String?) {
        if let token, identity != nil { lastAccessToken = token }
    }

    func deviceTokenArrived(_ hex: String) {
        guard hex != deviceToken else { return }
        deviceToken = hex
        registered = nil
        Task { await sync() }
    }

    /// Asks APNs for a token when the member has already allowed notifications. Never prompts.
    func registerIfAllowed() async {
        let status = await SystemPushPermission().status()
        guard status == .authorized else { return }
        UIApplication.shared.registerForRemoteNotifications()
    }

    private func sync() async {
        guard let identity, let deviceToken, let client else { return }
        let key = identity + "|" + deviceToken
        guard registered != key else { return }
        do {
            try await client.registerDevice(token: deviceToken, appEnv: appEnv)
            registered = key
            pushLog.info("device registered for push")
        } catch {
            pushLog.error("device registration failed: \(String(describing: error), privacy: .public)")
        }
    }

    private func unregister() async {
        guard let deviceToken, let token = lastAccessToken else { return }
        let client = MonacoCore.MonacoAPIClient(baseURL: Config.apiBaseURL, accessTokenProvider: { token })
        do {
            try await client.unregisterDevice(token: deviceToken)
        } catch {
            pushLog.notice("device unregister on sign-out failed: \(String(describing: error), privacy: .public)")
        }
        lastAccessToken = nil
    }
}

extension View {
    /// Keeps the push registrar in step with who is signed in. Applied once, at the app root.
    func pushRegistration(auth: PrivyAuthService) -> some View {
        modifier(PushRegistrationBridge(auth: auth))
    }
}

private struct PushRegistrationBridge: ViewModifier {
    @ObservedObject var auth: PrivyAuthService

    func body(content: Content) -> some View {
        content
            .task(id: auth.sessionIdentity) {
                await PushRegistrar.shared.sessionChanged(identity: auth.sessionIdentity, auth: auth)
            }
            .onChange(of: auth.accessToken) { _, token in
                PushRegistrar.shared.accessTokenChanged(token)
            }
    }
}

// MARK: - Permission

enum PushPermissionStatus: Equatable {
    case notDetermined
    case denied
    case authorized
}

/// Whether Monaco may notify, and asking. A protocol so sample screens show a fixed answer.
@MainActor
protocol PushPermissionSource {
    func status() async -> PushPermissionStatus
    /// Shows the system prompt (only ever once per install) and registers for a token on yes.
    func request() async -> PushPermissionStatus
}

struct SystemPushPermission: PushPermissionSource {
    func status() async -> PushPermissionStatus {
        let settings = await UNUserNotificationCenter.current().notificationSettings()
        switch settings.authorizationStatus {
        case .authorized, .provisional, .ephemeral:
            return .authorized
        case .denied:
            return .denied
        case .notDetermined:
            return .notDetermined
        @unknown default:
            return .notDetermined
        }
    }

    func request() async -> PushPermissionStatus {
        let granted = (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .badge, .sound])) ?? false
        if granted {
            UIApplication.shared.registerForRemoteNotifications()
        }
        return await status()
    }
}

/// A fixed answer, for sample screens.
struct FixedPushPermission: PushPermissionSource {
    let fixed: PushPermissionStatus

    func status() async -> PushPermissionStatus { fixed }
    func request() async -> PushPermissionStatus { fixed }
}

/// When to show the "Friends will wait on your vote" sheet: right after the member joins or
/// starts a cabal (their count of cabals goes up while the app is open, not on the first read
/// at launch), when the system has not asked yet and Monaco has not asked before.
enum PushPromptGate {
    static let askedKey = "monaco.push.prePromptAsked"

    static func shouldAsk(previousJoined: Int?, joined: Int?, status: PushPermissionStatus, alreadyAsked: Bool) -> Bool {
        guard let previousJoined, let joined, joined > previousJoined else { return false }
        return status == .notDetermined && !alreadyAsked
    }
}

extension View {
    /// Offers push after the member joins or creates a cabal. Applied once, on the tab shell.
    func pushPermissionPrompt() -> some View {
        modifier(PushPermissionPrompt())
    }
}

private struct PushPermissionPrompt: ViewModifier {
    @Environment(AppSessionStore.self) private var session
    @State private var isPresented = false
    private let permission = SystemPushPermission()

    /// Nil until Home's boards first load, so the first read is a baseline, not a join.
    private var joinedCount: Int? {
        session.home.map { $0.groups.filter(\.isJoined).count }
    }

    func body(content: Content) -> some View {
        content
            .onChange(of: joinedCount) { old, new in
                Task {
                    let asked = UserDefaults.standard.bool(forKey: PushPromptGate.askedKey)
                    let status = await permission.status()
                    if PushPromptGate.shouldAsk(previousJoined: old, joined: new, status: status, alreadyAsked: asked) {
                        isPresented = true
                    }
                }
            }
            .sheet(isPresented: $isPresented) {
                PushPrePromptSheet(
                    onTurnOn: {
                        UserDefaults.standard.set(true, forKey: PushPromptGate.askedKey)
                        _ = await permission.request()
                        isPresented = false
                    },
                    onNotNow: {
                        UserDefaults.standard.set(true, forKey: PushPromptGate.askedKey)
                        isPresented = false
                    }
                )
            }
    }
}

/// The ask before the system's ask: one sentence on why, then "Turn on" or "Not now". The
/// system prompt can be shown once per install, so it is only spent on a member who said yes here.
struct PushPrePromptSheet: View {
    let onTurnOn: () async -> Void
    let onNotNow: () -> Void

    @State private var isAsking = false

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            SunkenGlyphMark(systemImage: "bell", size: 56)
                .padding(.top, MonacoTheme.Space.l)
            Text(InboxCopy.permissionTitle)
                .font(MonacoTheme.Typo.title)
                .foregroundStyle(MonacoTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
            Text(InboxCopy.permissionMessage)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: MonacoTheme.Space.m)
            Button {
                guard !isAsking else { return }
                isAsking = true
                Haptics.tap()
                Task {
                    await onTurnOn()
                    isAsking = false
                }
            } label: {
                Text(InboxCopy.permissionTurnOn)
            }
            .buttonStyle(.monacoPrimary)
            .monacoFullWidthButtons()
            .disabled(isAsking)
            .accessibilityIdentifier("push-preprompt-turn-on")
            Button(action: onNotNow) {
                Text(InboxCopy.permissionNotNow)
                    .font(MonacoTheme.Typo.calloutStrong)
                    .foregroundStyle(MonacoTheme.brand)
                    .frame(maxWidth: .infinity, minHeight: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("push-preprompt-not-now")
        }
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .padding(.bottom, MonacoTheme.Space.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(MonacoTheme.surface.ignoresSafeArea())
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .interactiveDismissDisabled(isAsking)
        .accessibilityIdentifier("push-preprompt")
    }
}
