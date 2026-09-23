#if DEBUG
import MonacoCore
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
/// monogram glyph a real member would get; leave it off for the no-name fallback. The harness
/// seeds two open votes on the dashboard so the Cabals badge renders — it is amber (`warning`),
/// not systemRed, because §1.6 keeps the danger ramp for money that went wrong.
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
            .onAppear { session.dashboard = TabShellSample.dashboard }
    }
}

private extension TabShellSample {
    /// The smallest dashboard that makes the badge appear: two votes that have not expired.
    /// Nothing else on it is read by the shell.
    static var dashboard: HomeDashboardDTO {
        let now = Date()
        let rows = [("Weekend investors", "AAPLc"), ("Semis or bust", "NVDAc")]
        return HomeDashboardDTO(
            netWorthUsd: "0",
            netWorthDollarPnl: "+0.00",
            netWorthPercentReturn: nil,
            myGroups: [],
            pnlSeries1H: [],
            leaderboard: HomeLeaderboardSectionDTO(range: "24h", people: []),
            missedProposals: rows.enumerated().map { index, row in
                HomeMissedProposalRowDTO(
                    groupId: "8f1c2d3e-000\(index + 1)",
                    groupName: row.0,
                    proposalId: "proposal-\(index + 1)",
                    symbol: row.1,
                    status: "open",
                    createdAt: now.addingTimeInterval(-3600),
                    expiresAt: now.addingTimeInterval(3600 * Double(index + 2))
                )
            }
        )
    }
}
#endif
