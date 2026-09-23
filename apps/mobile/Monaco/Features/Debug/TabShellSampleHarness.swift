#if DEBUG
import SwiftUI

/// Renders the real `MainTabView` with no backend and no sign-in, so the tab chrome can be
/// reviewed on its own.
///
/// Every other sample harness renders one screen directly — `HomeSampleHarness` puts `HomeView`
/// on screen without a `TabView` around it — which means the app's most persistent surface, the
/// tab bar, had no harness at all and could only be seen after a Dynamic login. Its selected
/// colour was wrong for months for exactly that reason.
///
/// The tabs themselves come up empty or in their failure state: there is no session and no
/// network here, and that is the point. This harness is about the bar, the four glyphs, the
/// badge and the title voice, not about the screens inside.
///
/// Launch with `-MonacoTabShellSample`. Add `-MonacoTabShellSampleName <name>` to see the
/// monogram glyph a real member would get; leave it off for the no-name fallback.
enum TabShellSample {
    static let launchArgument = "-MonacoTabShellSample"
    static let nameArgument = "-MonacoTabShellSampleName"

    static var isEnabled: Bool {
        ProcessInfo.processInfo.arguments.contains(launchArgument)
    }

    /// The name the Profile glyph is drawn from. Nil renders the honest fallback.
    static var displayName: String? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: nameArgument),
              arguments.indices.contains(flag + 1)
        else { return nil }
        let name = arguments[flag + 1].trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty ? nil : name
    }

    static func rootView(auth: DynamicAuthService) -> some View {
        TabShellSampleHarness(auth: auth)
    }
}

private struct TabShellSampleHarness: View {
    @ObservedObject var auth: DynamicAuthService
    @State private var session = AppSessionStore()

    var body: some View {
        MainTabView(auth: auth)
            .environment(session)
            .monacoRootAppearance()
    }
}
#endif
