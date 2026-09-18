import XCTest
import MonacoCore

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
        XCTAssertEqual(dto.myGroups[0].groupId, dto.myGroups[0].id)
        XCTAssertEqual(dto.missedProposals[0].proposalId, dto.missedProposals[0].id)
        XCTAssertEqual(dto.missedProposals[0].groupId, dto.missedProposals[0].groupID)
        XCTAssertEqual(dto.leaderboard.people[0].userId, dto.leaderboard.people[0].id)

        let encoded = try JSONEncoder().encode(dto)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
        let groups = try XCTUnwrap(object["myGroups"] as? [[String: Any]])
        XCTAssertEqual(groups[0]["groupId"] as? String, dto.myGroups[0].groupId)
        XCTAssertNil(groups[0]["groupID"])
    }

    func testChartValuePreservesSignedAndInvalidAmountHandling() {
        for (amount, expected) in [("+12.50", 12.5), ("-2.50", -2.5), ("invalid", 0)] {
            let point = HomePnLSeriesPointDTO(ts: Date(timeIntervalSince1970: 1), equityUsd: "100", dollarPnl: amount)
            XCTAssertEqual(point.chartValue, expected)
            XCTAssertEqual(point.id, 1)
        }
    }
}
