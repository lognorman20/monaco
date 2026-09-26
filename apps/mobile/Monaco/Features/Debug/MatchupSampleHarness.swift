#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: the matchup surfaces on canned data, no sign-in or backend. Launch with
/// `-MonacoMatchupSample <scenario>`:
/// `home` (Home with "This week") · `cabal` (the cabal screen's Matchup section between the pot
/// and the members) · `matchup` (live pair, challenges, results) · `matchupBye` · `matchupLoading`
/// · `table` (season table) · `challenge` (search with results, one already sent).
enum MatchupSampleScenario: String, CaseIterable {
    case home
    case cabal
    case matchup
    case matchupBye
    case matchupLoading
    case table
    case challenge

    static let launchArgument = "-MonacoMatchupSample"

    static var requested: MatchupSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument), arguments.indices.contains(flag + 1) else { return nil }
        return MatchupSampleScenario(rawValue: arguments[flag + 1])
    }
}

struct MatchupSampleHarness: View {
    let scenario: MatchupSampleScenario
    @ObservedObject var auth: PrivyAuthService
    @State private var session = MatchupSampleData.session()
    @State private var selectedTab: MainTab = .home

    private var source: SampleMatchupDataSource {
        SampleMatchupDataSource(
            matchup: scenario == .matchupBye ? MatchupSampleData.byeMatchup : MatchupSampleData.groupMatchup,
            neverAnswers: scenario == .matchupLoading
        )
    }

    var body: some View {
        NavigationStack {
            root
        }
        .tint(MonacoTheme.ink)
        .environment(session)
        .environment(\.matchupDataSource, source)
    }

    @ViewBuilder
    private var root: some View {
        switch scenario {
        case .home:
            HomeView(auth: auth, selectedTab: $selectedTab)
        case .cabal:
            cabalScreen
        case .matchup, .matchupBye, .matchupLoading:
            MatchupView(auth: auth, groupId: MatchupSampleData.weekend.groupID, groupName: MatchupSampleData.weekend.name, source: source)
        case .table:
            MatchupTableView(source: source)
        case .challenge:
            ChallengeCabalView(
                groupId: MatchupSampleData.weekend.groupID,
                groupName: MatchupSampleData.weekend.name,
                source: source,
                alreadyChallenged: [MatchupSampleData.rent.groupID],
                initialQuery: "in"
            )
        }
    }

    /// The Matchup section where the cabal screen puts it: after the pot, before the members.
    private var cabalScreen: some View {
        let view = GroupDetailSampleData.view
        return ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                PotSectionView(pot: view.pot, groupId: view.id)
                CabalMatchupSection(auth: auth, groupId: view.id, groupName: view.name)
                MemberBoardSection(members: view.members, currentUserId: GroupDetailSampleData.viewerId)
            }
            .padding(.vertical, MonacoTheme.Space.m)
        }
        .monacoCanvas()
        .navigationTitle(view.name)
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// Canned answers. Every read returns at once, except in `matchupLoading`, where none ever do.
@MainActor
struct SampleMatchupDataSource: MatchupDataSource {
    let matchup: GroupMatchupDTO
    var neverAnswers = false

    private func wait() async throws {
        if neverAnswers { try await Task.sleep(for: .seconds(3600)) }
    }

    func groupMatchup(groupId: String) async throws -> GroupMatchupDTO {
        try await wait()
        return matchup
    }

    func homeMatchups() async throws -> HomeMatchupsDTO {
        try await wait()
        return MatchupSampleData.home
    }

    func table() async throws -> MatchupTableDTO {
        try await wait()
        return MatchupSampleData.table
    }

    func challenge(groupId: String, opponentId: String) async throws -> MatchupChallengeDTO {
        let opponent = MatchupSampleData.searchRows.first { $0.groupID == opponentId }
        return MatchupChallengeDTO(
            id: "sample-challenge-\(opponentId)",
            weekStart: MatchupSampleData.nextWeek,
            direction: .outgoing,
            status: .pending,
            opponent: MatchupFaceDTO(groupID: opponentId, name: opponent?.name ?? "Cabal", memberCount: opponent?.memberCount ?? 2),
            createdAt: Date()
        )
    }

    func accept(groupId: String, challengeId: String) async throws -> MatchupChallengeDTO {
        guard let challenge = matchup.challenges.first(where: { $0.id == challengeId }) else {
            throw MatchupChallengeRefused(reason: .challengeClosed)
        }
        return MatchupChallengeDTO(
            id: challenge.id,
            weekStart: challenge.weekStart,
            direction: challenge.direction,
            status: .accepted,
            opponent: challenge.opponent,
            createdAt: challenge.createdAt,
            acceptedAt: Date()
        )
    }

    func searchCabals(query: String, cursor: String?) async throws -> GroupSearchResponseDTO {
        let needle = query.lowercased()
        return GroupSearchResponseDTO(
            groups: MatchupSampleData.searchRows.filter { $0.name.lowercased().contains(needle) },
            nextCursor: nil
        )
    }
}

enum MatchupSampleData {
    static let weekend = MatchupFaceDTO(groupID: GroupDetailSampleData.view.id, name: "Weekend investors", memberCount: 5)
    static let semis = MatchupFaceDTO(groupID: "5b1f0c9e-0002-4c55-9a51-000000000002", name: "Semis or bust", memberCount: 4)
    static let index = MatchupFaceDTO(groupID: "5b1f0c9e-0003-4c55-9a51-000000000003", name: "Index huggers", memberCount: 3)
    static let rent = MatchupFaceDTO(groupID: "5b1f0c9e-0009-4c55-9a51-000000000009", name: "Rent money", memberCount: 2)
    static let oil = MatchupFaceDTO(groupID: "5b1f0c9e-0004-4c55-9a51-000000000004", name: "Oil and chips", memberCount: 6)
    static let moon = MatchupFaceDTO(groupID: "5b1f0c9e-0005-4c55-9a51-000000000005", name: "Moonshots", memberCount: 7)
    static let dividend = MatchupFaceDTO(groupID: "5b1f0c9e-0006-4c55-9a51-000000000006", name: "Dividend club of Leith", memberCount: 3)

    /// Monday 00:00 UTC of the current week.
    static var weekStart: Date {
        var calendar = Calendar(identifier: .iso8601)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        return calendar.dateInterval(of: .weekOfYear, for: Date())?.start ?? Date()
    }

    static var nextWeek: Date { weekStart.addingTimeInterval(7 * 86_400) }

    static func weeksAgo(_ n: Int) -> Date { weekStart.addingTimeInterval(TimeInterval(-7 * 86_400 * n)) }

    static func side(_ face: MatchupFaceDTO, _ score: String?) -> MatchupSideDTO {
        MatchupSideDTO(groupID: face.groupID, name: face.name, pictureUrl: face.pictureUrl, memberCount: face.memberCount, score: score)
    }

    static func matchup(_ a: MatchupFaceDTO, _ scoreA: String?, _ b: MatchupFaceDTO?, _ scoreB: String?, leading: MatchupLeader?) -> MatchupDTO {
        MatchupDTO(
            id: "sample-\(a.groupID)",
            weekStart: weekStart,
            weekEnd: nextWeek,
            daysLeft: 3,
            a: side(a, scoreA),
            b: b.map { side($0, scoreB) },
            leading: leading
        )
    }

    static let recent: [MatchupResultDTO] = [
        MatchupResultDTO(id: "r1", weekStart: weeksAgo(1), result: .win, opponent: semis, score: "0.012", opponentScore: "0.008"),
        MatchupResultDTO(id: "r2", weekStart: weeksAgo(2), result: .win, opponent: oil, score: "0.021", opponentScore: "-0.004"),
        MatchupResultDTO(id: "r3", weekStart: weeksAgo(3), result: .bye, opponent: nil, score: nil, opponentScore: nil),
        MatchupResultDTO(id: "r4", weekStart: weeksAgo(4), result: .loss, opponent: moon, score: "-0.013", opponentScore: "0.034"),
    ]

    static var groupMatchup: GroupMatchupDTO {
        GroupMatchupDTO(
            nextDrawAt: nextWeek,
            current: matchup(weekend, "0.0123", semis, "0.0081", leading: .a),
            record: MatchupRecordDTO(wins: 4, losses: 1, ties: 0),
            recent: recent,
            challenges: [
                MatchupChallengeDTO(id: "c-in", weekStart: nextWeek, direction: .incoming, status: .pending, opponent: index, createdAt: Date().addingTimeInterval(-3600)),
                MatchupChallengeDTO(id: "c-out", weekStart: nextWeek, direction: .outgoing, status: .pending, opponent: rent, createdAt: Date().addingTimeInterval(-7200)),
            ],
            canChallenge: true
        )
    }

    static var byeMatchup: GroupMatchupDTO {
        GroupMatchupDTO(
            nextDrawAt: nextWeek,
            current: matchup(weekend, nil, nil, nil, leading: nil),
            record: MatchupRecordDTO(wins: 4, losses: 1, ties: 0),
            recent: recent,
            challenges: [],
            canChallenge: true
        )
    }

    static var home: HomeMatchupsDTO {
        HomeMatchupsDTO(
            weekStart: weekStart,
            weekEnd: nextWeek,
            daysLeft: 3,
            nextDrawAt: nextWeek,
            drawn: true,
            hasCabals: true,
            matchups: [
                matchup(weekend, "0.0123", semis, "0.0081", leading: .a),
                matchup(index, "-0.0042", rent, "-0.0118", leading: .a),
                matchup(oil, nil, nil, nil, leading: nil),
            ]
        )
    }

    static let table = MatchupTableDTO(
        throughWeek: weeksAgo(1),
        cabals: [
            MatchupTableRowDTO(rank: 1, groupID: moon.groupID, name: moon.name, wins: 5, losses: 0, ties: 0, points: "0.0912", streak: "W5", isMine: false),
            MatchupTableRowDTO(rank: 2, groupID: weekend.groupID, name: weekend.name, wins: 4, losses: 1, ties: 0, points: "0.0433", streak: "W2", isMine: true),
            MatchupTableRowDTO(rank: 3, groupID: semis.groupID, name: semis.name, wins: 3, losses: 1, ties: 1, points: "0.0301", streak: "L1", isMine: false),
            MatchupTableRowDTO(rank: 4, groupID: oil.groupID, name: oil.name, wins: 3, losses: 2, ties: 0, points: "0.0120", streak: "W1", isMine: false),
            MatchupTableRowDTO(rank: 5, groupID: dividend.groupID, name: dividend.name, wins: 2, losses: 2, ties: 1, points: "0.0044", streak: "T1", isMine: false),
            MatchupTableRowDTO(rank: 6, groupID: index.groupID, name: index.name, wins: 1, losses: 3, ties: 0, points: "-0.0110", streak: "L2", isMine: true),
            MatchupTableRowDTO(rank: 7, groupID: rent.groupID, name: rent.name, wins: 0, losses: 4, ties: 0, points: "-0.0391", streak: "L4", isMine: false),
        ]
    )

    static let searchRows: [GroupDiscoveryRowDTO] = [
        GroupDiscoveryRowDTO(groupID: index.groupID, name: index.name, memberCount: 3, potValueUsd: "120.00", percentReturn: "0.018", dollarPnl: "+2.16", isJoined: false, joinMode: .open),
        GroupDiscoveryRowDTO(groupID: weekend.groupID, name: weekend.name, memberCount: 5, potValueUsd: "548.20", percentReturn: "0.040", dollarPnl: "+20.87", isJoined: true, joinMode: .open),
        GroupDiscoveryRowDTO(groupID: dividend.groupID, name: dividend.name, memberCount: 3, potValueUsd: "910.40", percentReturn: "0.006", dollarPnl: "+5.43", isJoined: false, joinMode: .request),
        GroupDiscoveryRowDTO(groupID: "5b1f0c9e-0007-4c55-9a51-000000000007", name: "Tin and silicon", memberCount: 9, potValueUsd: "2210.00", percentReturn: "-0.012", dollarPnl: "-26.84", isJoined: false, joinMode: .open),
        GroupDiscoveryRowDTO(groupID: rent.groupID, name: "Rent money investors", memberCount: 2, potValueUsd: "64.00", percentReturn: nil, dollarPnl: "+0.00", isJoined: false, joinMode: .open),
    ]

    /// Home's session: the populated Home sample without the open vote, so "This week" sits
    /// directly under the money.
    static func session() -> AppSessionStore {
        let session = AppSessionStore()
        session.isLoading = false
        session.me = MeResponse(
            userId: "sample-user",
            displayName: "Logan Norman",
            memberWalletAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
            profilePhotoUrl: nil
        )
        session.platformBalance = PlatformBalanceDTO(
            availableUsdcMicros: 248_500_000,
            memberWalletAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
            pendingAllocationMicros: 0
        )
        session.home = HomeViewDTO(
            groups: [
                HomeGroupBoardRowDTO(groupId: weekend.groupID, name: weekend.name, potValueUsd: "548.20", percentReturn: "0.124", dollarPnl: "+48.20", isJoined: true),
                HomeGroupBoardRowDTO(groupId: index.groupID, name: index.name, potValueUsd: "120.00", percentReturn: "-0.031", dollarPnl: "-3.90", isJoined: true),
                HomeGroupBoardRowDTO(groupId: oil.groupID, name: oil.name, potValueUsd: "310.00", percentReturn: nil, dollarPnl: "+0.00", isJoined: true),
            ],
            people: []
        )
        session.dashboard = HomeDashboardDTO(
            netWorthUsd: "1248.50",
            netWorthDollarPnl: "+48.20",
            netWorthPercentReturn: "0.040",
            myGroups: [
                HomeMyGroupRowDTO(groupId: weekend.groupID, name: weekend.name, equityUsd: "311.50", slicePercent: "0.568", dollarPnl: "+27.40", percentReturn: "0.096"),
                HomeMyGroupRowDTO(groupId: index.groupID, name: index.name, equityUsd: "400.05", slicePercent: "0.173", dollarPnl: "-15.00", percentReturn: "-0.036"),
                HomeMyGroupRowDTO(groupId: oil.groupID, name: oil.name, equityUsd: "120.00", slicePercent: "1.0", dollarPnl: "+0.00", percentReturn: nil),
            ],
            pnlSeries1H: HomeSampleHarness.samplePnLSeries1H(),
            leaderboard: HomeLeaderboardSectionDTO(
                range: "ALL",
                people: [
                    HomePeopleBoardRowDTO(userId: "u1", displayName: "Alfred", percentReturn: "0.124", dollarPnl: "+48.20"),
                    HomePeopleBoardRowDTO(userId: "u2", displayName: "Priya Shah", percentReturn: "0.081", dollarPnl: "+22.10"),
                ]
            ),
            missedProposals: []
        )
        return session
    }
}
#endif
