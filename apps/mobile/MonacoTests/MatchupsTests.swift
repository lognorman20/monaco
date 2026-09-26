import Foundation
import MonacoCore
import Testing
@testable import Monaco

private struct Offline: Error {}

/// A matchup source that answers from fields the test sets.
@MainActor
private final class StubMatchupSource: MatchupDataSource {
    var matchup: GroupMatchupDTO?
    var home: HomeMatchupsDTO?
    var readError: Error?
    var acceptError: Error?
    var challengeError: Error?
    var accepted: [String] = []
    var challenged: [String] = []

    func groupMatchup(groupId: String) async throws -> GroupMatchupDTO {
        if let readError { throw readError }
        return matchup!
    }

    func homeMatchups() async throws -> HomeMatchupsDTO {
        if let readError { throw readError }
        return home!
    }

    func table() async throws -> MatchupTableDTO {
        if let readError { throw readError }
        return MatchupTableDTO(throughWeek: nil, cabals: [])
    }

    func challenge(groupId: String, opponentId: String) async throws -> MatchupChallengeDTO {
        if let challengeError { throw challengeError }
        challenged.append(opponentId)
        return MatchupChallengeDTO(
            id: "c-\(opponentId)", weekStart: Self.week, direction: .outgoing, status: .pending,
            opponent: MatchupFaceDTO(groupID: opponentId, name: "Semis or bust", memberCount: 3), createdAt: Self.week
        )
    }

    func accept(groupId: String, challengeId: String) async throws -> MatchupChallengeDTO {
        if let acceptError { throw acceptError }
        accepted.append(challengeId)
        return MatchupChallengeDTO(
            id: challengeId, weekStart: Self.week, direction: .incoming, status: .accepted,
            opponent: MatchupFaceDTO(groupID: "g2", name: "Semis or bust", memberCount: 3), createdAt: Self.week
        )
    }

    func searchCabals(query: String, cursor: String?) async throws -> GroupSearchResponseDTO {
        GroupSearchResponseDTO(groups: [], nextCursor: nil)
    }

    /// Monday 21 September 2026, 00:00 UTC.
    static let week = Date(timeIntervalSince1970: 1_789_948_800)

    static func groupMatchup(challenges: [MatchupChallengeDTO] = []) -> GroupMatchupDTO {
        GroupMatchupDTO(
            nextDrawAt: week.addingTimeInterval(7 * 86_400),
            current: MatchupDTO(
                id: "m1", weekStart: week, weekEnd: week.addingTimeInterval(7 * 86_400), daysLeft: 3,
                a: MatchupSideDTO(groupID: "g1", name: "Weekend investors", memberCount: 5, score: "0.012"),
                b: MatchupSideDTO(groupID: "g2", name: "Semis or bust", memberCount: 4, score: "0.008"),
                leading: .a
            ),
            record: MatchupRecordDTO(wins: 4, losses: 1, ties: 0),
            recent: [],
            challenges: challenges,
            canChallenge: true
        )
    }

    static let incoming = MatchupChallengeDTO(
        id: "c-in", weekStart: week, direction: .incoming, status: .pending,
        opponent: MatchupFaceDTO(groupID: "g3", name: "Index huggers", memberCount: 3), createdAt: week
    )
}

@MainActor
struct MatchupScoreToneTests {
    @Test func leaderUpIsGreen() {
        #expect(MatchupScoreTone.tone(for: .a, leading: .a, score: "0.012") == .leadingGain)
    }

    @Test func leaderDownStandsOutInInkNotGreen() {
        #expect(MatchupScoreTone.tone(for: .b, leading: .b, score: "-0.004") == .leading)
    }

    @Test func leaderAtZeroIsNotAGain() {
        #expect(MatchupScoreTone.tone(for: .a, leading: .a, score: "0.0001") == .leading)
    }

    @Test func trailingSideAndATieArePlain() {
        #expect(MatchupScoreTone.tone(for: .b, leading: .a, score: "0.03") == .plain)
        #expect(MatchupScoreTone.tone(for: .a, leading: .tie, score: "0.01") == .plain)
        #expect(MatchupScoreTone.tone(for: .a, leading: nil, score: nil) == .plain)
    }
}

@MainActor
struct MatchupModelTests {
    @Test func firstReadFailingShowsTheFailure() async {
        // Arrange
        let source = StubMatchupSource()
        source.readError = Offline()
        let model = GroupMatchupModel(groupId: "g1")

        // Act
        try? await model.load(from: source)

        // Assert
        #expect(model.state == .failed)
    }

    @Test func aPollThatFailsLeavesTheMatchupOnScreen() async throws {
        // Arrange
        let source = StubMatchupSource()
        source.matchup = StubMatchupSource.groupMatchup()
        let model = GroupMatchupModel(groupId: "g1")
        try await model.load(from: source)
        source.readError = Offline()

        // Act
        await #expect(throws: Offline.self) { try await model.load(from: source, quiet: true) }

        // Assert
        #expect(model.state == .loaded(StubMatchupSource.groupMatchup()))
    }

    @Test func noSessionHidesTheSurface() async {
        // Arrange
        let source = StubMatchupSource()
        source.readError = Monaco.MonacoAPIError.missingAccessToken
        let model = HomeMatchupsModel()

        // Act
        try? await model.load(from: source)

        // Assert
        #expect(model.state == .hidden)
    }

    @Test func acceptingSaysWhoTheyPlayAndRereads() async throws {
        // Arrange
        let source = StubMatchupSource()
        source.matchup = StubMatchupSource.groupMatchup(challenges: [StubMatchupSource.incoming])
        let model = GroupMatchupModel(groupId: "g1")
        try await model.load(from: source)
        let acceptedView = StubMatchupSource.groupMatchup(challenges: [])
        source.matchup = acceptedView

        // Act
        let toast = await model.accept(StubMatchupSource.incoming, from: source)

        // Assert
        #expect(source.accepted == ["c-in"])
        #expect(toast?.message == "You play Index huggers next week")
        #expect(toast?.isSuccess == true)
        #expect(model.state == .loaded(acceptedView))
        #expect(model.accepting.isEmpty)
    }

    @Test func aRefusedAcceptSaysWhy() async throws {
        // Arrange
        let source = StubMatchupSource()
        source.matchup = StubMatchupSource.groupMatchup(challenges: [StubMatchupSource.incoming])
        source.acceptError = MatchupChallengeRefused(reason: .challengeClosed)
        let model = GroupMatchupModel(groupId: "g1")

        // Act
        let toast = await model.accept(StubMatchupSource.incoming, from: source)

        // Assert
        #expect(toast?.message == MatchupCopy.refusal(.challengeClosed))
        #expect(toast?.isSuccess == false)
    }

    @Test func aChallengeIsSentOnceAndTheRowRemembersIt() async {
        // Arrange
        let source = StubMatchupSource()
        let sender = ChallengeSendModel(groupId: "g1")
        let opponent = GroupDiscoveryRowDTO(
            groupID: "g2", name: "Semis or bust", memberCount: 3, potValueUsd: "100.00",
            percentReturn: nil, dollarPnl: "+0.00", isJoined: false, joinMode: .open
        )

        // Act
        let first = await sender.send(to: opponent, from: source)
        let second = await sender.send(to: opponent, from: source)

        // Assert
        #expect(first?.message == "Challenge sent to Semis or bust")
        #expect(second == nil)
        #expect(source.challenged == ["g2"])
        #expect(sender.sent == ["g2"])
    }

    @Test func aRefusedChallengeIsNotMarkedSent() async {
        // Arrange
        let source = StubMatchupSource()
        source.challengeError = MatchupChallengeRefused(reason: .incomingChallenge)
        let sender = ChallengeSendModel(groupId: "g1")
        let opponent = GroupDiscoveryRowDTO(
            groupID: "g2", name: "Semis or bust", memberCount: 3, potValueUsd: "100.00",
            percentReturn: nil, dollarPnl: "+0.00", isJoined: false, joinMode: .open
        )

        // Act
        let toast = await sender.send(to: opponent, from: source)

        // Assert
        #expect(toast?.message == MatchupCopy.refusal(.incomingChallenge))
        #expect(sender.sent.isEmpty)
    }
}
