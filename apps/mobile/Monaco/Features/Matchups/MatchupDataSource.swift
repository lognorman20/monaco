import MonacoCore
import SwiftUI

/// Reads and writes for the matchup surfaces. The live source calls the API with the signed-in
/// member's token; Debug harnesses put a canned one in the environment.
@MainActor
protocol MatchupDataSource {
    func groupMatchup(groupId: String) async throws -> GroupMatchupDTO
    func homeMatchups() async throws -> HomeMatchupsDTO
    func table() async throws -> MatchupTableDTO
    func challenge(groupId: String, opponentId: String) async throws -> MatchupChallengeDTO
    func accept(groupId: String, challengeId: String) async throws -> MatchupChallengeDTO
    /// The cabal search the Cabals tab uses.
    func searchCabals(query: String, cursor: String?) async throws -> GroupSearchResponseDTO
}

/// Talks to the API. The token is read per call, so a screen that outlives a refresh keeps working,
/// and a rejected session signs out the way every other read does.
@MainActor
struct LiveMatchupDataSource: MatchupDataSource {
    let auth: PrivyAuthService

    private func core(_ token: String) -> MonacoCore.MonacoAPIClient {
        MonacoCore.MonacoAPIClient(baseURL: Config.apiBaseURL, accessTokenProvider: { token })
    }

    func groupMatchup(groupId: String) async throws -> GroupMatchupDTO {
        try await auth.withAccessToken { try await core($0).groupMatchup(groupId: groupId) }
    }

    func homeMatchups() async throws -> HomeMatchupsDTO {
        try await auth.withAccessToken { try await core($0).homeMatchups() }
    }

    func table() async throws -> MatchupTableDTO {
        try await auth.withAccessToken { try await core($0).matchupTable(limit: 50) }
    }

    func challenge(groupId: String, opponentId: String) async throws -> MatchupChallengeDTO {
        try await auth.withAccessToken { try await core($0).challengeCabal(groupId: groupId, opponentGroupId: opponentId) }
    }

    func accept(groupId: String, challengeId: String) async throws -> MatchupChallengeDTO {
        try await auth.withAccessToken { try await core($0).acceptMatchupChallenge(groupId: groupId, challengeId: challengeId) }
    }

    func searchCabals(query: String, cursor: String?) async throws -> GroupSearchResponseDTO {
        try await LiveCabalsTabDataSource(auth: auth).search(query: query, cursor: cursor)
    }
}

private struct MatchupDataSourceKey: EnvironmentKey {
    static let defaultValue: (any MatchupDataSource)? = nil
}

extension EnvironmentValues {
    /// A canned matchup source for the sample harnesses. Nil in the app, where the live one is used.
    var matchupDataSource: (any MatchupDataSource)? {
        get { self[MatchupDataSourceKey.self] }
        set { self[MatchupDataSourceKey.self] = newValue }
    }
}

/// The search half of the Cabals tab's data source, so challenging reuses the cabal search model.
@MainActor
struct MatchupCabalSearchSource: CabalsTabDataSource {
    let source: any MatchupDataSource

    func leaderboard() async throws -> GroupLeaderboardResponseDTO {
        GroupLeaderboardResponseDTO(groups: [])
    }

    func pnlHistory(range: GroupPnLRange) async throws -> MyGroupsPnLHistoryDTO {
        MyGroupsPnLHistoryDTO(range: range.rawValue, series: [])
    }

    func search(query: String, cursor: String?) async throws -> GroupSearchResponseDTO {
        try await source.searchCabals(query: query, cursor: cursor)
    }
}
