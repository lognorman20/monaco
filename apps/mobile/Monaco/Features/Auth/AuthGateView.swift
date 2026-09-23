import SwiftUI

struct AuthGateView: View {
    @ObservedObject var auth: DynamicAuthService
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        Group {
            if Config.dynamic.isConfigured {
                if hasLoginMethod {
                    if isAuthenticated {
                        if auth.accessToken != nil {
                            // Past the gate. **This is where the app opens into daylight**: nothing
                            // below here is ink-pinned, so Home arrives on paper in light mode.
                            SessionGateView(auth: auth)
                        } else {
                            preAuth { restoringView }
                        }
                    } else if auth.phase == .restoring {
                        preAuth { restoringView }
                    } else if case .restoreFailed(let message) = auth.phase {
                        preAuth { restoreFailedView(message: message) }
                    } else {
                        // `LoginView` pins its own surface; it is the one screen here that is a
                        // designed object rather than a state.
                        LoginView(auth: auth)
                    }
                } else {
                    preAuth { missingLoginMethodsView }
                }
            } else {
                preAuth { missingConfigView }
            }
        }
        .task {
            await auth.restoreSessionIfNeeded()
        }
        .onChange(of: scenePhase) { _, newPhase in
            guard newPhase == .active else { return }
            Task { await auth.restoreSessionIfNeeded() }
        }
    }

    /// The ink surface, applied to a pre-auth state rather than to the gate as a whole.
    ///
    /// It used to wrap the `Group`, which meant `SessionGateView` — and therefore the entire
    /// signed-in app — inherited the pre-auth colour scheme. Ink is the *pre*-auth world; the app
    /// opens into daylight the moment the token is in hand.
    private func preAuth<Content: View>(@ViewBuilder content: () -> Content) -> some View {
        content()
            .authScreenBackground()
            .tint(MonacoTheme.Ink.fgPrimary)
            .foregroundStyle(MonacoTheme.Ink.fgPrimary)
    }

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
            return true
        }
        return false
    }

    private var hasLoginMethod: Bool {
        Config.dynamic.smsLoginEnabled || Config.dynamic.emailLoginEnabled
    }

    private var missingConfigView: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label("Sign-in is not configured", systemImage: "key.fill")
                .font(.headline)
                .foregroundStyle(MonacoTheme.Ink.fgPrimary)

            Text("Set DYNAMIC_ENVIRONMENT_ID in your Xcode scheme or shell env. Copy values from `.env.example`.")
                .authSecondaryCaption()
        }
    }

    private var missingLoginMethodsView: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label("No login methods enabled", systemImage: "person.crop.circle.badge.exclamationmark")
                .font(.headline)
                .foregroundStyle(MonacoTheme.Ink.fgPrimary)

            Text("Enable AUTH_SMS_LOGIN_ENABLED and/or AUTH_EMAIL_LOGIN_ENABLED.")
                .authSecondaryCaption()
        }
    }
}

#Preview {
    AuthGateView(auth: DynamicAuthService())
}
