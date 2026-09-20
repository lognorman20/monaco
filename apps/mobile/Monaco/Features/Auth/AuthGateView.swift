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
                        restoringView
                    } else if case .restoreFailed(let message) = auth.phase {
                        restoreFailedView(message: message)
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

    /// Shown while a saved sign-in is being restored, so a returning user never
    /// sees the login form flash before the app opens.
    private var restoringView: some View {
        VStack(spacing: MonacoTheme.Space.l) {
            MonacoMark(size: 88)
            ProgressView()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Signing you in")
        .accessibilityIdentifier("sessionRestoringView")
    }

    private func restoreFailedView(message: String) -> some View {
        VStack(spacing: MonacoTheme.Space.m) {
            EmptyState(title: "Can't sign you in yet", message: message)
            Button("Try again") {
                Task { await auth.restoreSessionIfNeeded() }
            }
            .buttonStyle(.monacoPrimary)
            Button("Sign out") {
                Task { await auth.logout() }
            }
            .buttonStyle(.monacoSecondary)
        }
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityIdentifier("sessionRestoreFailedView")
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

    private var missingConfigView: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label("Privy not configured", systemImage: "key.fill")
                .font(.headline)
                .foregroundStyle(MonacoTheme.primaryText)

            Text("Set PRIVY_APP_ID and PRIVY_APP_CLIENT_ID in your Xcode scheme or shell env. Copy values from `.env.example`.")
                .authSecondaryCaption()
        }
    }

    private var missingLoginMethodsView: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label("No login methods enabled", systemImage: "person.crop.circle.badge.exclamationmark")
                .font(.headline)
                .foregroundStyle(MonacoTheme.primaryText)

            Text("Enable PRIVY_SMS_LOGIN_ENABLED and/or PRIVY_EMAIL_LOGIN_ENABLED, or turn SMS/email on in Privy dashboard Login Methods.")
                .authSecondaryCaption()
        }
    }
}

#Preview {
    AuthGateView(auth: PrivyAuthService())
}
