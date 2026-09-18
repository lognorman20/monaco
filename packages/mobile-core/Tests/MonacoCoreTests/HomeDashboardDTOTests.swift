import XCTest
@testable import MonacoCore

final class HomeDashboardDTOTests: XCTestCase {
    func testHomeDashboardDTO_decodesDashboardPayload() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "home_dashboard", withExtension: "json")
        )
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let data = try Data(contentsOf: fixtureURL)

        // Act
        let dto = try decoder.decode(HomeDashboardDTO.self, from: data)

        // Assert
        XCTAssertEqual(dto.netWorthUsd, "145.00")
        XCTAssertEqual(dto.myGroups.count, 1)
        XCTAssertEqual(dto.pnlSeries1H.count, 2)
        XCTAssertEqual(dto.leaderboard.people.count, 1)
        XCTAssertEqual(dto.missedProposals.count, 1)
        XCTAssertEqual(dto.missedProposals[0].symbol, "AAPL")
    }
}
