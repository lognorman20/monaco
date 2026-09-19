import SwiftUI

/// Opens the backend session, then the tab shell.
struct SessionGateView: View {
    @ObservedObject var auth: PrivyAuthService
    @State private var session = AppSessionStore()

    /// #158 first-login username + tour inserts here. Keep false until that ticket ships.
    private var needsOnboarding: Bool { false }

    var body: some View {
        Group {
            // #217: the tabs open as soon as the session exists; Home loads its own data.
            if session.me != nil {
                if needsOnboarding {
                    Color.clear.accessibilityIdentifier("onboarding-hook")
                } else {
                    MainTabView(auth: auth)
                }
            } else if session.isLoading {
                SessionGateSkeleton()
            } else if session.errorMessage != nil {
                VStack(spacing: MonacoTheme.Space.m) {
                    EmptyState(
                        title: "Couldn't reach Monaco",
                        message: "Check your connection and try again."
                    )
                    Button("Try again") {
                        Task { await session.bootstrap(auth: auth) }
                    }
                    .buttonStyle(.monacoPrimary)
                }
                .padding(.horizontal, MonacoTheme.Space.gutter)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .monacoCanvas()
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

/// App-shaped placeholder while the session opens: hero figure, a pill, three rows, a tab bar.
private struct SessionGateSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                SkeletonBlock(width: 140, height: 14)
                SkeletonBlock(width: 200, height: 44)
                SkeletonBlock(width: 120, height: 24, radius: 12)
            }
            .padding(.top, 72)
            SkeletonBlock(height: 64, radius: MonacoTheme.Radius.card)
            VStack(spacing: MonacoTheme.Space.sm) {
                ForEach(0..<3, id: \.self) { _ in
                    SkeletonBlock(height: 60, radius: MonacoTheme.Radius.tile)
                }
            }
            Spacer()
        }
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .monacoCanvas()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }
}
