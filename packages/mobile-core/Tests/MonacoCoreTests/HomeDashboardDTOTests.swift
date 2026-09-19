import XCTest
@testable import MonacoCore

final class HomeDashboardDTOTests: XCTestCase {
    func testHomeDashboardDTO_decodesDashboardPayload() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "home_dashboard", withExtension: "json")
        )
        let decoder = monacoISO8601JSONDecoder()
        let data = try Data(contentsOf: fixtureURL)

        // Act
        let dto = try decoder.decode(HomeDashboardDTO.self, from: data)

        // Assert
        XCTAssertEqual(dto.netWorthUsd, "145.00")
        XCTAssertEqual(dto.myGroups.count, 1)
        XCTAssertEqual(dto.myGroups[0].groupID, "g1")
        XCTAssertEqual(dto.pnlSeries1H.count, 2)
        XCTAssertEqual(dto.leaderboard.people.count, 1)
        XCTAssertEqual(dto.missedProposals.count, 1)
        XCTAssertEqual(dto.missedProposals[0].proposalID, "p1")
        XCTAssertEqual(dto.missedProposals[0].symbol, "AAPL")
    }

    func testHomeDashboardDTO_decodesFractionalISO8601Timestamps() throws {
        let json = """
        {
          "netWorthUsd": "145.00",
          "netWorthDollarPnl": "+15.00",
          "netWorthPercentReturn": "0.115",
          "myGroups": [],
          "pnlSeries1H": [
            {
              "ts": "2026-09-17T22:00:00.123456Z",
              "equityUsd": "130.00",
              "dollarPnl": "+0.00"
            },
            {
              "ts": "2026-09-17T23:00:00Z",
              "equityUsd": "145.00",
              "dollarPnl": "+15.00"
            }
          ],
          "leaderboard": { "range": "ALL", "people": [] },
          "missedProposals": []
        }
        """

        let dto = try monacoISO8601JSONDecoder().decode(HomeDashboardDTO.self, from: Data(json.utf8))
        XCTAssertEqual(dto.pnlSeries1H.count, 2)
        XCTAssertEqual(dto.pnlSeries1H[0].dollarPnl, "+0.00")
    }
}
