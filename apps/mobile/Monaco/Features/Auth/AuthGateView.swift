import SwiftUI

struct AuthGateView: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        Group {
            if Config.privy.isConfigured {
                if hasLoginMethod {
                    if isAuthenticated {
                        SessionGateView(auth: auth)
                    } else if auth.phase == .restoring {
                        // A returning member never sees the login form flash before the app opens.
                        SessionRestoringView()
                    } else if case .restoreFailed(let message) = auth.phase {
                        SessionFailureView(
                            title: SessionGateCopy.restoreFailedTitle,
                            message: message,
                            onRetry: { await auth.restoreSessionIfNeeded() },
                            onSignOut: { await auth.logout() }
                        )
                        .accessibilityIdentifier("sessionRestoreFailedView")
                    } else {
                        LoginView(auth: auth)
                    }
                } else {
                    missingLoginMethodsView
                }
            } else {
                missingConfigView
            }
        }
        .authScreenBackground()
        .tint(MonacoTheme.accent)
        .foregroundStyle(MonacoTheme.primaryText)
        .task {
            await auth.restoreSessionIfNeeded()
        }
        .onChange(of: scenePhase) { _, newPhase in
            // Coming back to the app (e.g. after turning Wi-Fi on) retries a restore
            // that failed offline. A no-op in every other phase.
            guard newPhase == .active else { return }
            Task { await auth.restoreSessionIfNeeded() }
        }
    }

    private var isAuthenticated: Bool {
        if case .authenticated = auth.phase {
            return auth.accessToken != nil
        }
        return false
    }

    private var hasLoginMethod: Bool {
        Config.privy.smsLoginEnabled || Config.privy.emailLoginEnabled
    }

    // Developer-facing only: a build without Privy credentials, or with every method off.

    private var missingConfigView: some View {
        EmptyState(
            title: "Privy isn't configured",
            message: "Set PRIVY_APP_ID and PRIVY_APP_CLIENT_ID in your Xcode scheme or shell env. Copy values from .env.example."
        )
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var missingLoginMethodsView: some View {
        EmptyState(
            title: "No sign-in methods are on",
            message: "Enable PRIVY_SMS_LOGIN_ENABLED and/or PRIVY_EMAIL_LOGIN_ENABLED, or turn SMS/email on in Privy dashboard Login Methods."
        )
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// The launch screen, held while a saved sign-in is restored: the same mark at the same size in
/// the same place (`LaunchScreen.storyboard` centres a 96pt mark in the whole screen), so a
/// returning member sees the app open rather than a second splash that jumps.
///
/// A spinner joins the mark only if the restore runs long. A quick one never shows it, so the
/// handoff from the launch screen stays invisible.
struct SessionRestoringView: View {
    /// `LaunchScreen.storyboard`'s mark.
    private static let markSize: CGFloat = 96

    @State private var showsProgress = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        ZStack {
            MonacoTheme.canvas
            MonacoMark(size: Self.markSize)
                // Below the mark without moving it off the launch screen's centre.
                .overlay(alignment: .top) {
                    if showsProgress {
                        ProgressView()
                            .tint(MonacoTheme.muted)
                            .offset(y: Self.markSize + MonacoTheme.Space.l)
                            .transition(.opacity)
                    }
                }
        }
        .ignoresSafeArea()
        .task {
            do {
                try await Task.sleep(for: .milliseconds(800))
            } catch {
                return
            }
            withAnimation(reduceMotion ? nil : .easeOut(duration: 0.2)) {
                showsProgress = true
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Signing you in")
        .accessibilityIdentifier("sessionRestoringView")
    }
}

#Preview {
    AuthGateView(auth: PrivyAuthService())
}
