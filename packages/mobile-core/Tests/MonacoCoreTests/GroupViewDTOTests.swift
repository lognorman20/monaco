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
        XCTAssertEqual(dto.potTotalUsd, "623.01")
        XCTAssertEqual(dto.resolvedPotTotalUsd, "623.01")
        XCTAssertEqual(dto.pot.count, 2)
        XCTAssertEqual(dto.pot[0].dollarPnl, "+0.00")
        XCTAssertEqual(dto.pot[1].dollarPnl, "+47.51")
        XCTAssertEqual(dto.members.count, 2)
        XCTAssertEqual(dto.you.equityUsd, "311.50")
    }

    func testPotRowDTO_afterHoursTrue_decodesLabelFlag() throws {
        // Arrange
        let json = """
        {"symbol":"AAPLx","units":"1","markUsd":"100","valueUsd":"100","dollarPnl":"+0.00","afterHours":true}
        """

        // Act
        let row = try JSONDecoder().decode(PotRowDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertEqual(row.afterHours, true)
    }

    func testPotRowDTO_decodesExactTokenAmount() throws {
        let json = """
        {"symbol":"AAPLx","units":"0.5","markUsd":"100","valueUsd":"50","dollarPnl":"+0.00","tokenAmount":"50000000"}
        """

        let row = try JSONDecoder().decode(PotRowDTO.self, from: Data(json.utf8))

        XCTAssertEqual(row.tokenAmount, "50000000")
    }

    func testGroupAgentDTO_decodesApiKey() throws {
        let json = """
        {"id":"a1","status":"active","agentDisplayName":"Scout","allocationUsdcMicros":"100000000","apiKey":"scout"}
        """

        let agent = try JSONDecoder().decode(GroupAgentDTO.self, from: Data(json.utf8))

        XCTAssertEqual(agent.apiKey, "scout")
    }

    func testGroupAgentDTO_decodesConnectText() throws {
        let json = """
        {"id":"a1","status":"active","agentDisplayName":"Scout","allocationUsdcMicros":"100000000","apiKey":"scout","connectText":"Send this header on every request: X-Monaco-Agent-Key: scout"}
        """

        let agent = try JSONDecoder().decode(GroupAgentDTO.self, from: Data(json.utf8))

        XCTAssertEqual(agent.connectText, "Send this header on every request: X-Monaco-Agent-Key: scout")
    }

    func testGroupAgentDTO_connectTextAbsent_decodesNil() throws {
        let json = """
        {"id":"a1","status":"active","agentDisplayName":"Scout","allocationUsdcMicros":"100000000"}
        """

        let agent = try JSONDecoder().decode(GroupAgentDTO.self, from: Data(json.utf8))

        XCTAssertNil(agent.apiKey)
        XCTAssertNil(agent.connectText)
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
