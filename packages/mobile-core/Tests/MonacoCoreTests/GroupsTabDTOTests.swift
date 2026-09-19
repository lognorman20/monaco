import XCTest
import MonacoCore

final class GroupsTabDTOTests: XCTestCase {
    func testGroupSearchResponseDTO_decodesGroups() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "groups_tab_search", withExtension: "json")
        )
        let data = try Data(contentsOf: fixtureURL)

        // Act
        let dto = try JSONDecoder().decode(GroupSearchResponseDTO.self, from: data)

        // Assert
        XCTAssertEqual(dto.groups.count, 1)
        XCTAssertEqual(dto.groups[0].name, "Weekend investors")
        XCTAssertTrue(dto.groups[0].isJoined)
        XCTAssertEqual(dto.groups[0].joinMode, "open")
        XCTAssertEqual(dto.groups[0].groupId, dto.groups[0].id)
    }

    func testGroupLeaderboardResponseDTO_decodesRankedRows() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "groups_tab_leaderboard", withExtension: "json")
        )
        let data = try Data(contentsOf: fixtureURL)

        // Act
        let dto = try JSONDecoder().decode(GroupLeaderboardResponseDTO.self, from: data)

        // Assert
        XCTAssertEqual(dto.groups.count, 1)
        XCTAssertEqual(dto.groups[0].rank, 1)
        XCTAssertEqual(dto.groups[0].groupId, dto.groups[0].id)
        XCTAssertEqual(dto.groups[0].potValueUsd, "548.20")
    }

    func testGroupPnLHistoryDTO_decodesPoints() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "groups_tab_pnl_history", withExtension: "json")
        )
        let data = try Data(contentsOf: fixtureURL)

        // Act
        let dto = try JSONDecoder().decode(GroupPnLHistoryDTO.self, from: data)

        // Assert
        XCTAssertEqual(dto.name, "Weekend investors")
        XCTAssertEqual(dto.points.count, 2)
        XCTAssertEqual(dto.groupId, dto.groupID)
        XCTAssertEqual(dto.points[1].dollarPnl, "+48.20")
    }
}
