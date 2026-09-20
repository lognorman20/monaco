import XCTest
@testable import MonacoCore

final class MarketStatusDTOTests: XCTestCase {
    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try JSONDecoder().decode(type, from: Data(json.utf8))
    }

    func testMarketStatus_decodesEverySessionAndItsNextTransition() throws {
        let dto = try decode(MarketStatusDTO.self, """
        {
          "session": "after_hours",
          "isOpen": false,
          "afterHours": true,
          "nextSession": "closed",
          "nextTransition": "2026-09-23T00:00:00Z",
          "asOf": "2026-09-22T21:30:00Z"
        }
        """)

        XCTAssertEqual(dto.session, .afterHours)
        XCTAssertFalse(dto.isOpen)
        XCTAssertTrue(dto.afterHours)
        XCTAssertEqual(dto.nextSession, .closed)
        XCTAssertEqual(dto.nextTransition?.timeIntervalSince1970, 1_790_121_600)
        XCTAssertEqual(dto.asOf?.timeIntervalSince1970, 1_790_112_600)
        XCTAssertNil(dto.holiday)
        XCTAssertFalse(dto.earlyClose)
    }

    func testMarketStatus_decodesAHolidayAndAHalfDay() throws {
        let holiday = try decode(MarketStatusDTO.self, """
        {"session":"closed","isOpen":false,"afterHours":true,"asOf":"2026-11-26T16:00:00Z","holiday":"Thanksgiving Day"}
        """)
        XCTAssertEqual(holiday.holiday, "Thanksgiving Day")

        let halfDay = try decode(MarketStatusDTO.self, """
        {"session":"open","isOpen":true,"afterHours":false,"asOf":"2026-11-27T16:00:00Z","earlyClose":true}
        """)
        XCTAssertTrue(halfDay.earlyClose)
        XCTAssertTrue(halfDay.session.isRegularSession)
    }

    func testMarketStatus_unknownSessionDoesNotFailTheWholeResponse() throws {
        // A session name added server-side must not take the asset screen down.
        let dto = try decode(MarketStatusDTO.self, """
        {"session":"maintenance_window","isOpen":false,"afterHours":true,"asOf":"2026-09-22T21:30:00Z"}
        """)
        XCTAssertEqual(dto.session, .unknown)
        XCTAssertTrue(dto.afterHours)
    }

    func testMarketStatus_missingAfterHoursIsDerivedFromIsOpen() throws {
        let dto = try decode(MarketStatusDTO.self, """
        {"session":"pre_market","isOpen":false,"asOf":"2026-09-22T12:00:00Z"}
        """)
        XCTAssertTrue(dto.afterHours)
    }

    func testMarketStatus_missingTimestampsDecodeToNilRatherThanThrow() throws {
        let dto = try decode(MarketStatusDTO.self, """
        {"session":"open","isOpen":true,"afterHours":false,"asOf":"2026-09-22T14:00:00Z"}
        """)
        XCTAssertNil(dto.nextTransition)
        XCTAssertNil(dto.nextSession)
    }

    func testMarketStatus_malformedTimestampThrows() {
        // A timestamp we cannot parse is a bug worth surfacing, not a nil to paper
        // over — "next transition" silently missing would read as "never".
        XCTAssertThrowsError(try decode(MarketStatusDTO.self, """
        {"session":"open","isOpen":true,"afterHours":false,"asOf":"yesterday afternoon"}
        """))
    }

    func testMarketStatus_acceptsFractionalSeconds() throws {
        let dto = try decode(MarketStatusDTO.self, """
        {"session":"open","isOpen":true,"afterHours":false,"asOf":"2026-09-22T14:00:00.482Z"}
        """)
        XCTAssertEqual(dto.asOf?.timeIntervalSince1970 ?? 0, 1_790_085_600.482, accuracy: 0.001)
    }

    func testMarketStatus_roundTripsThroughEncoding() throws {
        let encoded = try JSONEncoder().encode(MarketSampleData.sessionAfterHours)
        let decoded = try JSONDecoder().decode(MarketStatusDTO.self, from: encoded)
        XCTAssertEqual(decoded, MarketSampleData.sessionAfterHours)
    }
}
