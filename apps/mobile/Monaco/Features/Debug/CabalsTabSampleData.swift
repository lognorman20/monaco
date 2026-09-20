#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only sample data for the Cabals tab. Launch with
/// `-MonacoCabalsTabSample` to open the tab without sign-in or a backend
/// (screenshots and XCUITests). Never compiled into Release builds.
enum CabalsTabSampleData {
    static let launchArgument = "-MonacoCabalsTabSample"
    static let scenarioArgument = "-MonacoCabalsTabSampleScenario"

    /// What the tab is booted into. Add a scenario rather than a second harness:
    /// the point of this file is that every Cabals screenshot comes from the
    /// same fixed cabals.
    enum Scenario: String {
        /// Signed in, cabals loaded. The default.
        case normal
        /// The cabals list never lands, the way a cold start on a slow network
        /// or a failing `GET /v1/home` leaves it.
        case cabalsUnavailable
    }

    static var isEnabled: Bool {
        ProcessInfo.processInfo.arguments.contains(launchArgument)
    }

    static var scenario: Scenario {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: scenarioArgument),
              arguments.indices.contains(flag + 1),
              let scenario = Scenario(rawValue: arguments[flag + 1]) else { return .normal }
        return scenario
    }

    struct Cabal {
        let id: String
        let name: String
        let members: Int
        let pot: String
        let pnl: String
        let percent: String?
        let joined: Bool
        let mode: GroupJoinMode
        /// Daily P&L path for the chart, oldest first.
        let path: [Double]
    }

    static let cabals: [Cabal] = [
        Cabal(id: "5b1f0c9e-0001-4c55-9a51-000000000001", name: "Weekend investors", members: 4, pot: "548.20", pnl: "+48.20", percent: "0.0964", joined: true, mode: .open,
              path: [0, 4.1, 9.8, 7.2, 15.5, 22.0, 19.4, 31.7, 38.9, 48.2]),
        Cabal(id: "5b1f0c9e-0002-4c55-9a51-000000000002", name: "Rent money", members: 3, pot: "212.40", pnl: "-7.60", percent: "-0.0345", joined: true, mode: .request,
              path: [0, -1.2, 2.4, 3.1, -0.8, -4.5, -2.2, -6.0, -5.1, -7.6]),
        Cabal(id: "5b1f0c9e-0003-4c55-9a51-000000000003", name: "Apple heads", members: 2, pot: "91.35", pnl: "+1.35", percent: "0.015", joined: true, mode: .open,
              path: [0, 0.2, 0.9, 1.1, 0.4, 1.35]),
        Cabal(id: "5b1f0c9e-0004-4c55-9a51-000000000004", name: "Dorm 4B fund", members: 9, pot: "1320.00", pnl: "+320.00", percent: "0.32", joined: false, mode: .open, path: []),
        Cabal(id: "5b1f0c9e-0005-4c55-9a51-000000000005", name: "Tesla or bust", members: 5, pot: "760.10", pnl: "+110.10", percent: "0.1694", joined: false, mode: .request, path: []),
        Cabal(id: "5b1f0c9e-0006-4c55-9a51-000000000006", name: "Weekend warriors", members: 3, pot: "64.00", pnl: "-6.00", percent: "-0.0857", joined: false, mode: .open, path: []),
    ]

    static var home: HomeViewDTO {
        HomeViewDTO(
            groups: cabals.filter(\.joined).map {
                HomeGroupBoardRowDTO(groupId: $0.id, name: $0.name, potValueUsd: $0.pot, percentReturn: $0.percent, dollarPnl: $0.pnl, isJoined: true)
            },
            people: []
        )
    }

    @MainActor
    struct DataSource: CabalsTabDataSource {
        func leaderboard() async throws -> GroupLeaderboardResponseDTO {
            let ranked = cabals
                .filter { $0.percent != nil }
                .sorted { (Double($0.percent ?? "0") ?? 0) > (Double($1.percent ?? "0") ?? 0) }
            return GroupLeaderboardResponseDTO(groups: ranked.enumerated().map { index, cabal in
                GroupLeaderboardRowDTO(
                    rank: index + 1, groupID: cabal.id, name: cabal.name, memberCount: cabal.members,
                    potValueUsd: cabal.pot, percentReturn: cabal.percent, dollarPnl: cabal.pnl,
                    isJoined: cabal.joined, joinMode: cabal.mode
                )
            })
        }

        func pnlHistory(range: GroupPnLRange) async throws -> MyGroupsPnLHistoryDTO {
            let now = Date()
            let series = cabals.filter(\.joined).map { cabal in
                let step: TimeInterval = 3 * 24 * 3600
                let points = cabal.path.enumerated().map { index, value in
                    GroupPnLPointDTO(
                        at: now.addingTimeInterval(-step * Double(cabal.path.count - 1 - index)),
                        potValueUsd: cabal.pot,
                        netInUsd: cabal.pot,
                        dollarPnl: String(format: "%+.2f", value)
                    )
                }
                let windowDays: Double = switch range {
                case .oneDay: 1
                case .oneWeek: 7
                case .oneMonth: 30
                case .threeMonths: 90
                }
                let since = now.addingTimeInterval(-windowDays * 24 * 3600)
                return GroupPnLSeriesDTO(
                    groupID: cabal.id,
                    name: cabal.name,
                    range: range.rawValue,
                    points: points.filter { $0.at >= since }
                )
            }
            return MyGroupsPnLHistoryDTO(range: range.rawValue, series: series)
        }

        func search(query: String, cursor: String?) async throws -> GroupSearchResponseDTO {
            try await Task.sleep(for: .milliseconds(150))
            let matches = cabals.filter { $0.name.localizedCaseInsensitiveContains(query) }
            return GroupSearchResponseDTO(
                groups: matches.map {
                    GroupDiscoveryRowDTO(
                        groupID: $0.id, name: $0.name, memberCount: $0.members, potValueUsd: $0.pot,
                        percentReturn: $0.percent, dollarPnl: $0.pnl, isJoined: $0.joined, joinMode: $0.mode
                    )
                },
                nextCursor: nil
            )
        }
    }
}

/// Root view for `-MonacoCabalsTabSample`: the Cabals tab on sample data.
struct CabalsTabSampleHarness: View {
    @ObservedObject var auth: DynamicAuthService
    @State private var session: AppSessionStore = {
        let session = AppSessionStore()
        // `home` stays nil in the unavailable scenario, which is exactly what the
        // tab sees before the cabals list lands.
        if CabalsTabSampleData.scenario == .normal {
            session.home = CabalsTabSampleData.home
        }
        session.isLoading = false
        return session
    }()

    var body: some View {
        TabView {
            NavigationStack {
                CabalsTabView(auth: auth, dataSource: CabalsTabSampleData.DataSource())
            }
            .tabItem {
                Label("Cabals", systemImage: "person.3")
                    .accessibilityIdentifier("tab-cabals")
            }
        }
        .tint(MonacoTheme.ink)
        .environment(session)
    }
}
#endif
