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

    func testHomeDashboardDTO_wholeSecondUTCTimestamps_decodeToExactInstants() throws {
        // Arrange: the fixture carries the wire format the backend emits, `2026-09-17T22:00:00Z`.
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "home_dashboard", withExtension: "json")
        )
        let data = try Data(contentsOf: fixtureURL)

        // Act
        let dto = try monacoISO8601JSONDecoder().decode(HomeDashboardDTO.self, from: data)

        // Assert: 2026-09-17T22:00:00Z is 1_789_682_400 seconds after the epoch.
        XCTAssertEqual(dto.pnlSeries1H.map(\.ts.timeIntervalSince1970), [1_789_682_400, 1_789_686_000])
        XCTAssertEqual(dto.missedProposals[0].createdAt.timeIntervalSince1970, 1_789_675_200)
        XCTAssertEqual(dto.missedProposals[0].expiresAt.timeIntervalSince1970, 1_789_761_600)
    }

    func testHomePnLSeriesDTO_wholeSecondUTCTimestamps_keepDistinctPointIDs() throws {
        // Arrange: points one second apart, the closest the backend series can produce.
        let json = """
        {
          "points": [
            { "ts": "2026-09-17T22:00:00Z", "equityUsd": "130.00", "dollarPnl": "+0.00" },
            { "ts": "2026-09-17T22:00:01Z", "equityUsd": "131.00", "dollarPnl": "+1.00" }
          ]
        }
        """

        // Act
        let dto = try monacoISO8601JSONDecoder().decode(HomePnLSeriesDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertEqual(dto.points.map(\.id), [1_789_682_400, 1_789_682_401])
    }

    func testHomeMissedProposalsDTO_nonISO8601Timestamp_throwsDataCorrupted() {
        // Arrange: a Unix number-as-string is not a format the API emits.
        let json = """
        {
          "proposals": [
            {
              "groupId": "g1",
              "groupName": "Vote cabal",
              "proposalId": "p1",
              "symbol": "AAPL",
              "status": "open",
              "createdAt": "1789675200",
              "expiresAt": "2026-09-18T20:00:00Z"
            }
          ]
        }
        """

        // Act / Assert
        XCTAssertThrowsError(
            try monacoISO8601JSONDecoder().decode(HomeMissedProposalsDTO.self, from: Data(json.utf8))
        ) { error in
            guard case DecodingError.dataCorrupted = error else {
                return XCTFail("expected dataCorrupted, got \(error)")
            }
        }
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
