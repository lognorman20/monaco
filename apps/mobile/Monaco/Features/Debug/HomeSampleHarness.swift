#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: renders Home from canned `AppSessionStore` data so QA can screenshot
/// each state without auth or a backend. Launch with
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
    @ObservedObject var auth: DynamicAuthService
    @State private var session: AppSessionStore
    @State private var selectedTab: MainTab = .home

    init(scenario: HomeSampleScenario, auth: DynamicAuthService) {
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

        // Pot values for the "Your cabals" subtitles; same cabals as `ProfileSampleHarness`.
        session.home = HomeViewDTO(
            groups: joined ? [
                HomeGroupBoardRowDTO(groupId: "g1", name: "Weekend investors", potValueUsd: "548.20", percentReturn: "0.124", dollarPnl: "+48.20", isJoined: true),
                HomeGroupBoardRowDTO(groupId: "g2", name: "Semis or bust", potValueUsd: "2310.75", percentReturn: "-0.031", dollarPnl: "-73.90", isJoined: true),
                HomeGroupBoardRowDTO(groupId: "g3", name: "Index huggers", potValueUsd: "120.00", percentReturn: nil, dollarPnl: "+0.00", isJoined: true),
            ] : [],
            people: []
        )

        session.dashboard = HomeDashboardDTO(
            netWorthUsd: joined ? "1248.50" : "0.00",
            netWorthDollarPnl: joined ? "+48.20" : "+0.00",
            netWorthPercentReturn: joined ? "0.040" : nil,
            myGroups: joined ? [
                HomeMyGroupRowDTO(groupId: "g1", name: "Weekend investors", equityUsd: "311.50", slicePercent: "0.568", dollarPnl: "+27.40", percentReturn: "0.096"),
                HomeMyGroupRowDTO(groupId: "g2", name: "Semis or bust", equityUsd: "400.05", slicePercent: "0.173", dollarPnl: "-15.00", percentReturn: "-0.036"),
                HomeMyGroupRowDTO(groupId: "g3", name: "Index huggers", equityUsd: "120.00", slicePercent: "1.0", dollarPnl: "+0.00", percentReturn: nil),
            ] : [],
            pnlSeries1H: joined ? HomeSampleHarness.samplePnLSeries1H() : [],
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

    /// Sample 1H net-worth P&L curve for the Home hero chart. Deterministic, five-minute
    /// steps back from launch, and it lands exactly on the dashboard's "+48.20" so the
    /// curve and the badge agree. Harness-only: the app never synthesises a series.
    static func samplePnLSeries1H(now: Date = Date()) -> [HomePnLSeriesPointDTO] {
        let steps: [(Int, String, String)] = [
            (12, "1212.70", "+12.40"),
            (11, "1210.10", "+9.80"),
            (10, "1218.50", "+18.20"),
            (9, "1215.90", "+15.60"),
            (8, "1224.40", "+24.10"),
            (7, "1221.60", "+21.30"),
            (6, "1230.20", "+29.90"),
            (5, "1234.50", "+34.20"),
            (4, "1231.10", "+30.80"),
            (3, "1238.90", "+38.60"),
            (2, "1243.40", "+43.10"),
            (1, "1241.50", "+41.20"),
            (0, "1248.50", "+48.20"),
        ]
        return steps.map { minutesAgo, equity, pnl in
            HomePnLSeriesPointDTO(
                ts: now.addingTimeInterval(TimeInterval(-minutesAgo * 300)),
                equityUsd: equity,
                dollarPnl: pnl
            )
        }
    }
}
#endif
