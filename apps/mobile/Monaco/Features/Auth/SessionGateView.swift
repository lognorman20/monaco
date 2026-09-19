import SwiftUI

/// Opens backend session then the five-tab shell.
struct SessionGateView: View {
    @ObservedObject var auth: PrivyAuthService
    @State private var session = AppSessionStore()

    /// #158 first-login username + tour inserts here. Keep false until that ticket ships.
    private var needsOnboarding: Bool { false }

    var body: some View {
        Group {
            if session.me != nil {
                if needsOnboarding {
                    Color.clear.accessibilityIdentifier("onboarding-hook")
                } else {
                    MainTabView(auth: auth)
                }
            } else if session.isLoading {
                ProgressView("Opening session…")
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .tint(MonacoTheme.accent)
                    .frame(maxWidth: .infinity, minHeight: 200)
            } else if let errorMessage = session.errorMessage {
                VStack(alignment: .leading, spacing: 12) {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.destructive)

                    Button("Try again") {
                        Task { await session.bootstrap(auth: auth) }
                    }
                    .buttonStyle(.monacoPrimary)
                }
                .padding()
                .monacoSurfaceCard()
                .padding()
            }
        }
        .environment(session)
        .task(id: auth.accessToken) {
            await session.bootstrap(auth: auth)
        }
    }
}

#Preview {
    SessionGateView(auth: PrivyAuthService())
}
