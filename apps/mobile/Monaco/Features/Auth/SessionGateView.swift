import MonacoCore
import SwiftUI

/// Opens the backend session, then first run or the tab shell.
struct SessionGateView: View {
    @ObservedObject var auth: DynamicAuthService
    @State private var session = AppSessionStore()

    private var destination: FirstRunDestination {
        FirstRunGate.destination(for: session.me)
    }

    var body: some View {
        Group {
            // #217: the tabs open as soon as the session exists; Home loads its own data.
            if destination == .app {
                MainTabView(auth: auth)
            } else if destination == .nameSetup {
                // #158: a new account has no name, and every social surface would call it
                // "Member". Ask once, here, before anything is on screen under that name.
                OnboardingNameView(
                    auth: auth,
                    save: { await session.updateDisplayName($0, auth: auth, optimistic: false) },
                    signOut: { await auth.logout() }
                )
                .transition(.opacity)
            } else if session.isLoading {
                SessionGateSkeleton()
            } else if let errorMessage = session.errorMessage {
                VStack(spacing: MonacoTheme.Space.m) {
                    EmptyState(
                        title: "Couldn't open Monaco",
                        message: errorMessage
                    )
                    #if DEBUG
                    if let detail = session.errorDebugDetail {
                        Text(detail)
                            .font(.system(.footnote, design: .monospaced))
                            .foregroundStyle(MonacoTheme.tertiaryText)
                            .multilineTextAlignment(.center)
                            .accessibilityIdentifier("sessionErrorDebugDetail")
                    }
                    #endif
                    Button("Try again") {
                        Task { await session.bootstrap(auth: auth) }
                    }
                    .buttonStyle(.monacoPrimary)
                    Button("Sign out") {
                        Task { await auth.logout() }
                    }
                    .buttonStyle(.monacoSecondary)
                }
                .padding(.horizontal, MonacoTheme.Space.gutter)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .monacoCanvas()
            }
        }
        .environment(session)
        // The name landing is a real step forward, not a flicker: cross-fade it.
        .animation(.easeInOut(duration: 0.28), value: destination)
        // Keyed on who is signed in, not on the token: Dynamic rotates the access token, and
        // the transport already retries with the fresh one. Keying on the token string made
        // every rotation look like a new sign-in and re-ran the whole bootstrap — seven
        // requests, and every screen keyed the same way reloaded under the member's hands.
        .task(id: auth.sessionIdentity) {
            await session.bootstrap(auth: auth)
        }
    }
}

#Preview {
    SessionGateView(auth: DynamicAuthService())
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
