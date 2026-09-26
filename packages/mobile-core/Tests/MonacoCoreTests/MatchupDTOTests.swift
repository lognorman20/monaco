import XCTest
@testable import MonacoCore

final class MatchupDTOTests: XCTestCase {
    private func fixture(_ name: String) throws -> Data {
        let url = try XCTUnwrap(Bundle.module.url(forResource: name, withExtension: "json"))
        return try Data(contentsOf: url)
    }

    func testGroupMatchupDTO_decodesTheLiveMatchupRecordResultsAndChallenges() throws {
        // Arrange
        let data = try fixture("group_matchup")

        // Act
        let dto = try monacoISO8601JSONDecoder().decode(GroupMatchupDTO.self, from: data)

        // Assert
        let current = try XCTUnwrap(dto.current)
        XCTAssertEqual(current.daysLeft, 3)
        XCTAssertEqual(current.a.name, "Weekend investors")
        XCTAssertEqual(current.a.score, "0.0123")
        XCTAssertEqual(current.b?.pictureUrl, "https://img.test/semis.jpg")
        XCTAssertEqual(current.leading, .a)
        XCTAssertFalse(current.isBye)
        XCTAssertEqual(current.weekEnd, dto.nextDrawAt)
        XCTAssertEqual(dto.record, MatchupRecordDTO(wins: 4, losses: 1, ties: 0))
        XCTAssertTrue(dto.canChallenge)
    }

    func testGroupMatchupDTO_readsAByeAndUnknownValuesWithoutFailing() throws {
        // Arrange
        let data = try fixture("group_matchup")

        // Act
        let dto = try monacoISO8601JSONDecoder().decode(GroupMatchupDTO.self, from: data)

        // Assert: a bye has no opponent; a result or status this build does not know is not a crash.
        XCTAssertEqual(dto.recent.map(\.result), [.win, .bye, nil])
        XCTAssertNil(dto.recent[1].opponent)
        XCTAssertEqual(dto.challenges.map(\.status), [.pending, .expired])
        XCTAssertTrue(dto.challenges[0].canAccept)
        XCTAssertFalse(dto.challenges[1].canAccept)
    }

    func testHomeMatchupsDTO_decodesPairsAndByes() throws {
        // Arrange
        let data = try fixture("home_matchups")

        // Act
        let dto = try monacoISO8601JSONDecoder().decode(HomeMatchupsDTO.self, from: data)

        // Assert
        XCTAssertTrue(dto.drawn)
        XCTAssertEqual(dto.matchups.count, 2)
        XCTAssertTrue(dto.matchups[0].fromChallenge)
        XCTAssertTrue(dto.matchups[1].isBye)
        XCTAssertNil(dto.matchups[1].leading)
        XCTAssertNil(dto.matchups[1].a.score)
    }

    func testMatchupTableDTO_decodesRowsAndRecords() throws {
        // Arrange
        let data = try fixture("matchup_table")

        // Act
        let dto = try monacoISO8601JSONDecoder().decode(MatchupTableDTO.self, from: data)

        // Assert
        XCTAssertNotNil(dto.throughWeek)
        XCTAssertEqual(dto.cabals.map(\.rank), [1, 2])
        XCTAssertEqual(dto.cabals[0].streak, "W3")
        XCTAssertTrue(dto.cabals[0].isMine)
        XCTAssertNil(dto.cabals[1].streak)
        XCTAssertEqual(dto.cabals[1].record, MatchupRecordDTO(wins: 3, losses: 1, ties: 1))
    }

    func testGroupMatchupDTO_nextOpponentIsTheAcceptedChallenge() {
        // Arrange
        let week = Date(timeIntervalSince1970: 1_789_948_800)
        let face = MatchupFaceDTO(groupID: "g2", name: "Semis or bust", memberCount: 4)
        let accepted = MatchupChallengeDTO(id: "c2", weekStart: week, direction: .incoming, status: .accepted, opponent: face, createdAt: week)
        let pending = MatchupChallengeDTO(id: "c1", weekStart: week, direction: .outgoing, status: .pending, opponent: face, createdAt: week)

        // Act
        let dto = GroupMatchupDTO(nextDrawAt: week, current: nil, record: .empty, recent: [], challenges: [pending, accepted], canChallenge: true)

        // Assert
        XCTAssertEqual(dto.nextOpponent?.id, "c2")
    }
}
