import Combine
import LocalAuthentication
import MonacoCore
import os
import SwiftUI
import UIKit

/// What unlocks this phone. `LAContext` in the app; a stub in tests and sample screens.
@MainActor
protocol DeviceOwnerAuthenticating {
    /// "Face ID", "Touch ID", "Optic ID", or "passcode" when no biometry is enrolled. Nil when
    /// the phone has no passcode, so there is nothing to lock Monaco with.
    func availableMethod() -> String?
    /// Face ID (or Touch ID), with the passcode as the fallback. True when the member passed.
    func authenticate(reason: String) async -> Bool
}

struct LocalDeviceOwnerAuthenticator: DeviceOwnerAuthenticating {
    func availableMethod() -> String? {
        let context = LAContext()
        var error: NSError?
        guard context.canEvaluatePolicy(.deviceOwnerAuthentication, error: &error) else { return nil }
        // Hardware that has Face ID but no face enrolled still reports `.faceID`; the biometrics
        // policy is what says it can actually be used.
        guard context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &error) else {
            return "passcode"
        }
        switch context.biometryType {
        case .faceID: return "Face ID"
        case .touchID: return "Touch ID"
        case .opticID: return "Optic ID"
        default: return "passcode"
        }
    }

    func authenticate(reason: String) async -> Bool {
        let context = LAContext()
        do {
            return try await context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason)
        } catch {
            AppLogger.session.notice("App lock authentication did not pass: \((error as NSError).code, privacy: .public)")
            return false
        }
    }
}

/// Locks Monaco behind Face ID. When the member turns it on, the app opens locked, and locks
/// again when it comes back from the background after the chosen delay.
///
/// The lock screen draws in its own window above everything (see `AppLockWindow`), so a sheet
/// that was open when the member left cannot sit on top of it.
@MainActor
final class AppLock: ObservableObject {
    @Published private(set) var isLocked: Bool
    @Published private(set) var isAuthenticating = false

    let settings: SettingsStore
    private let authenticator: DeviceOwnerAuthenticating
    private let now: () -> Date
    /// When the app last went to the background, cleared when it comes back.
    private var backgroundedAt: Date?
    /// Set when the lock has just engaged (at launch, or coming back after the delay), so Face
    /// ID is offered once without a tap. A prompt the member cancelled is not offered again:
    /// cancelling makes the scene inactive and active again, and re-prompting on that would
    /// never let them stop.
    private var promptPending: Bool

    init(
        settings: SettingsStore,
        authenticator: DeviceOwnerAuthenticating? = nil,
        now: @escaping () -> Date = Date.init,
        isSuspended: Bool = false
    ) {
        self.settings = settings
        self.authenticator = authenticator ?? LocalDeviceOwnerAuthenticator()
        self.now = now
        self.isSuspended = isSuspended
        let locked = settings.lockEnabled && !isSuspended
        isLocked = locked
        promptPending = locked
    }

    /// Debug sample screens run with the lock suspended, so a developer who uses the lock on
    /// the simulator still gets screenshots of the screen asked for.
    let isSuspended: Bool

    var isEnabled: Bool { settings.lockEnabled && !isSuspended }

    /// What the toggle calls the lock: "Face ID", "Touch ID", "passcode". Nil when the phone
    /// has no passcode.
    var method: String? { authenticator.availableMethod() }

    func didEnterBackground() {
        guard isEnabled, backgroundedAt == nil else { return }
        backgroundedAt = now()
    }

    func didBecomeActive() {
        defer { backgroundedAt = nil }
        guard isEnabled, let backgroundedAt else { return }
        if settings.lockTimeout.requiresUnlock(backgroundedAt: backgroundedAt, now: now()) {
            isLocked = true
            promptPending = true
        }
    }

    /// True once after the lock engages: the caller offers Face ID straight away.
    func takePendingPrompt() -> Bool {
        defer { promptPending = false }
        return promptPending && isLocked
    }

    /// Asks for Face ID and opens the app when the member passes. A second call while the
    /// first prompt is up does nothing.
    @discardableResult
    func unlock() async -> Bool {
        guard isLocked else { return true }
        guard !isAuthenticating else { return false }
        isAuthenticating = true
        defer { isAuthenticating = false }
        let passed = await authenticator.authenticate(reason: SettingsCopy.unlockReason)
        if passed { isLocked = false }
        return passed
    }

    /// Turning the lock on asks for Face ID first, so nobody locks themselves out with a method
    /// they cannot pass. Turning it off does not ask: the app is already open.
    /// Returns whether the setting is now what was asked for.
    func setEnabled(_ enabled: Bool) async -> Bool {
        guard enabled else {
            settings.lockEnabled = false
            isLocked = false
            promptPending = false
            backgroundedAt = nil
            return true
        }
        guard !settings.lockEnabled else { return true }
        guard method != nil, !isAuthenticating else { return false }
        isAuthenticating = true
        defer { isAuthenticating = false }
        guard await authenticator.authenticate(reason: SettingsCopy.turnOnReason) else { return false }
        settings.lockEnabled = true
        return true
    }

    /// The account is gone: nothing left to protect, and the next member should not inherit it.
    func forget() {
        settings.forgetAccount()
        isLocked = false
        promptPending = false
        backgroundedAt = nil
    }
}

/// The lock screen: the mark, the name, one way in.
///
/// The mark sits where the launch screen puts it (a 96pt mark in the middle of the whole
/// screen, `LaunchScreen.storyboard`), so a locked cold launch opens without the mark jumping.
struct LockScreenView: View {
    let method: String?
    let isAuthenticating: Bool
    let onUnlock: () -> Void

    private static let markSize: CGFloat = 96

    var body: some View {
        ZStack {
            MonacoTheme.canvas
            MonacoMark(size: Self.markSize)
                .overlay(alignment: .top) {
                    Text("Monaco")
                        .font(MonacoTheme.Typo.display)
                        .foregroundStyle(MonacoTheme.ink)
                        .fixedSize()
                        .accessibilityAddTraits(.isHeader)
                        .offset(y: Self.markSize + MonacoTheme.Space.l)
                }
        }
        .ignoresSafeArea()
        .overlay(alignment: .bottom) {
            Button(action: onUnlock) {
                Label {
                    Text(SettingsCopy.unlockButton)
                } icon: {
                    Image(systemName: Self.glyph(for: method))
                        .accessibilityHidden(true)
                }
            }
            .buttonStyle(.monacoPrimary)
            .monacoFullWidthButtons()
            .disabled(isAuthenticating)
            .accessibilityHint(method.map { "Uses \($0)" } ?? "")
            .accessibilityIdentifier("app-lock-unlock")
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.bottom, MonacoTheme.Space.l)
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("app-lock-screen")
    }

    static func glyph(for method: String?) -> String {
        switch method {
        case "Face ID": return "faceid"
        case "Touch ID": return "touchid"
        case "Optic ID": return "opticid"
        default: return "lock.fill"
        }
    }
}

/// What the app switcher sees while the lock is on: the paper and the mark, nothing of the
/// member's money. Opaque rather than a blur, which the design does not use anywhere.
struct PrivacyCoverView: View {
    var body: some View {
        MonacoTheme.canvas
            .ignoresSafeArea()
            .overlay { MonacoMark(size: 96) }
            .accessibilityHidden(true)
    }
}

/// The lock screen and the privacy cover, in the window above the app.
private struct AppLockOverlay: View {
    @ObservedObject var lock: AppLock
    @ObservedObject var state: AppLockWindow.State

    var body: some View {
        if lock.isLocked {
            LockScreenView(method: lock.method, isAuthenticating: lock.isAuthenticating) {
                Task { await lock.unlock() }
            }
        } else if state.covered {
            PrivacyCoverView()
        }
    }
}

/// A window at alert level over the app's own, holding the lock screen and the privacy cover.
/// An overlay in the SwiftUI tree would sit under any sheet the member left open, with the
/// sheet's content in plain view on top of the lock.
@MainActor
final class AppLockWindow {
    final class State: ObservableObject {
        @Published var covered = false
    }

    private var window: UIWindow?
    private let state = State()

    func update(lock: AppLock, covered: Bool, appearance: AppearanceChoice) {
        state.covered = covered
        let visible = lock.isLocked || covered
        guard visible else {
            window?.isHidden = true
            return
        }
        if window == nil {
            window = makeWindow(lock: lock)
        }
        guard let window else { return }
        window.overrideUserInterfaceStyle = appearance.interfaceStyle
        if window.isHidden {
            // Whatever had the keyboard would otherwise keep it up over the lock screen.
            UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
            window.isHidden = false
        }
    }

    private func makeWindow(lock: AppLock) -> UIWindow? {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        guard let scene = scenes.first(where: { $0.activationState == .foregroundActive }) ?? scenes.first else {
            return nil
        }
        let window = UIWindow(windowScene: scene)
        window.windowLevel = .alert + 1
        let host = UIHostingController(rootView: AppLockOverlay(lock: lock, state: state))
        host.view.backgroundColor = .clear
        host.view.accessibilityViewIsModal = true
        window.rootViewController = host
        window.backgroundColor = .clear
        return window
    }
}

/// A confirmation that has to outlive the screen that raised it: "Your account is deleted."
/// lands after the session ends and the app has gone back to sign-in.
@MainActor
final class AccountFarewell: ObservableObject {
    static let shared = AccountFarewell()
    @Published var toast: MonacoToast?
}

/// Everything Settings changes about the whole app, applied once at the root: the appearance,
/// the app lock and its window, the privacy cover, and the farewell toast.
struct SettingsRootModifier: ViewModifier {
    @StateObject private var settings = SettingsStore.shared
    @StateObject private var lock = AppLock(settings: .shared, isSuspended: SettingsRootModifier.isSampleLaunch)
    @StateObject private var farewell = AccountFarewell.shared
    @State private var lockWindow = AppLockWindow()
    @Environment(\.scenePhase) private var scenePhase

    func body(content: Content) -> some View {
        content
            .environmentObject(settings)
            .environmentObject(lock)
            .monacoToast($farewell.toast)
            .preferredColorScheme(settings.appearance.colorScheme)
            .onChange(of: scenePhase, initial: true) { _, phase in
                switch phase {
                case .background:
                    lock.didEnterBackground()
                case .active:
                    lock.didBecomeActive()
                    if lock.takePendingPrompt() {
                        Task { await lock.unlock() }
                    }
                default:
                    break
                }
                syncWindow()
            }
            .onChange(of: lock.isLocked) { _, _ in syncWindow() }
            .onChange(of: settings.lockEnabled) { _, _ in syncWindow() }
            .onChange(of: settings.appearance) { _, _ in syncWindow() }
    }

    /// The cover goes up whenever the scene is not active and the lock is on, which is what
    /// the app switcher photographs.
    private func syncWindow() {
        let covered = lock.isEnabled && scenePhase != .active
        lockWindow.update(lock: lock, covered: covered, appearance: settings.appearance)
    }

    private static var isSampleLaunch: Bool {
        #if DEBUG
        return ProcessInfo.processInfo.arguments.contains { argument in
            argument.hasPrefix("-Monaco") && (argument.contains("Sample") || argument.contains("Gallery"))
        }
        #else
        return false
        #endif
    }
}

extension View {
    /// Applies Settings to the whole app. Once, at the root of the window.
    func monacoSettingsRoot() -> some View {
        modifier(SettingsRootModifier())
    }
}
