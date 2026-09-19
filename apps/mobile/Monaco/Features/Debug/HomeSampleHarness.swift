#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: renders Home from canned `AppSessionStore` data so QA can screenshot
/// each state without Privy or a backend. Launch with
/// `-MonacoHomeSample <populated|empty|loading|missedVote>`.
enum HomeSampleScenario: String, CaseIterable {
    case populated
    case empty
    case loading
    case missedVote

    static var requested: HomeSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: "-MonacoHomeSample"),
              arguments.indices.contains(flag + 1)
        else { return nil }
        return HomeSampleScenario(rawValue: arguments[flag + 1])
    }
}

struct HomeSampleHarness: View {
    let scenario: HomeSampleScenario
    @ObservedObject var auth: PrivyAuthService
    @State private var session: AppSessionStore
    @State private var selectedTab: MainTab = .home

    init(scenario: HomeSampleScenario, auth: PrivyAuthService) {
        self.scenario = scenario
        self.auth = auth
        _session = State(initialValue: Self.makeSession(for: scenario))
    }

    var body: some View {
        NavigationStack {
            HomeView(auth: auth, selectedTab: $selectedTab)
        }
        .environment(session)
    }

    private static func makeSession(for scenario: HomeSampleScenario) -> AppSessionStore {
        let session = AppSessionStore()
        session.isLoading = false

        if scenario == .loading {
            session.isLoading = true
            return session
        }

        session.me = MeResponse(
            userId: "sample-user",
            displayName: "Logan Norman",
            memberWalletAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
            profilePhotoUrl: scenario == .empty ? nil : ProfileSampleHarness.samplePhotoURL()?.absoluteString
        )

        session.platformBalance = PlatformBalanceDTO(
            availableUsdcMicros: 248_500_000,
            memberWalletAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
            pendingAllocationMicros: 0
        )

        let joined = scenario != .empty
        let missed = scenario == .missedVote || scenario == .populated

        session.dashboard = HomeDashboardDTO(
            netWorthUsd: joined ? "1248.50" : "0.00",
            netWorthDollarPnl: joined ? "+48.20" : "+0.00",
            netWorthPercentReturn: joined ? "0.040" : nil,
            myGroups: joined ? [
                HomeMyGroupRowDTO(groupId: "g1", name: "Weekend investors", equityUsd: "311.50", slicePercent: "0.568", dollarPnl: "+27.40", percentReturn: "0.096"),
                HomeMyGroupRowDTO(groupId: "g2", name: "Semis or bust", equityUsd: "400.05", slicePercent: "0.173", dollarPnl: "-15.00", percentReturn: "-0.036"),
                HomeMyGroupRowDTO(groupId: "g3", name: "Index huggers", equityUsd: "120.00", slicePercent: "1.0", dollarPnl: "+0.00", percentReturn: nil),
            ] : [],
            pnlSeries1H: [],
            leaderboard: HomeLeaderboardSectionDTO(
                range: "ALL",
                people: joined ? [
                    HomePeopleBoardRowDTO(userId: "u1", displayName: "Alfred", percentReturn: "0.124", dollarPnl: "+48.20"),
                    HomePeopleBoardRowDTO(userId: "u2", displayName: "Priya Shah", percentReturn: "0.081", dollarPnl: "+22.10"),
                ] : []
            ),
            missedProposals: missed ? [
                HomeMissedProposalRowDTO(
                    groupId: "g1",
                    groupName: "Weekend investors",
                    proposalId: "p1",
                    symbol: "AAPLx",
                    status: "open",
                    createdAt: Date().addingTimeInterval(-3600 * 2),
                    expiresAt: Date().addingTimeInterval(3600 * 22)
                ),
            ] : []
        )
        return session
    }
}
#endif
