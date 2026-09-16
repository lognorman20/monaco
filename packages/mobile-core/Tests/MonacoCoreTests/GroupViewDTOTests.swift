import XCTest
@testable import MonacoCore

final class GroupViewDTOTests: XCTestCase {
    func testGroupViewDTO_decodesFixtureWithPotAndMemberBoard() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "group_view", withExtension: "json")
        )
        let data = try Data(contentsOf: fixtureURL)

        // Act
        let dto = try JSONDecoder().decode(GroupViewDTO.self, from: data)

        // Assert
        XCTAssertEqual(dto.name, "Weekend investors")
        XCTAssertEqual(dto.pot.count, 2)
        XCTAssertEqual(dto.members.count, 2)
        XCTAssertEqual(dto.you.equityUsd, "311.50")
    }

    func testPotRowDTO_afterHoursTrue_decodesLabelFlag() throws {
        // Arrange
        let json = """
        {"symbol":"AAPLx","units":"1","markUsd":"100","valueUsd":"100","afterHours":true}
        """

        // Act
        let row = try JSONDecoder().decode(PotRowDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertEqual(row.afterHours, true)
    }

    func testBoardCells_renderServerRankOrder_withoutResortingByDollars() {
        // Arrange
        let members = [
            LeaderboardRowDTO(rank: 1, userId: "a", displayName: "A", percentReturn: "0.20", dollarPnl: "+10"),
            LeaderboardRowDTO(rank: 2, userId: "b", displayName: "B", percentReturn: "0.15", dollarPnl: "+50"),
        ]

        // Act
        let rows = MemberBoardRenderer.displayRows(from: members)

        // Assert
        XCTAssertEqual(rows.map(\.rank), [1, 2])
        XCTAssertEqual(rows.map(\.displayName), ["A", "B"])
    }
}
