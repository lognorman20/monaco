import XCTest
@testable import MonacoCore

final class SharedFormattersTests: XCTestCase {
    func testUTCTimestamps_matchTheISOFormatter() {
        let samples = [
            "2026-09-18T15:04:05Z", "2026-09-18T15:04:05.123Z", "1970-01-01T00:00:00Z",
            "2000-02-29T23:59:59.999Z", "2024-12-31T00:00:01Z", "1969-12-31T23:59:59Z",
        ]
        for raw in samples {
            let expected = SharedFormatters.iso8601Fractional.date(from: raw)
                ?? SharedFormatters.iso8601WholeSeconds.date(from: raw)
            XCTAssertNotNil(expected, raw)
            XCTAssertEqual(SharedFormatters.utcTimestamp(raw)!.timeIntervalSince1970,
                           expected!.timeIntervalSince1970, accuracy: 0.0005, raw)
        }
    }

    func testMicrosecondTimestamps_parse() {
        let date = SharedFormatters.iso8601Date(from: "2026-09-18T15:04:05.123456Z")
        XCTAssertEqual(date!.timeIntervalSince1970, 1_789_743_845.123456, accuracy: 0.000001)
    }

    func testMalformedOrOffsetTimestamps_leaveTheFastPath() {
        for raw in ["2026-02-30T00:00:00Z", "2026-13-01T00:00:00Z", "2026-09-18T24:00:00Z",
                    "2026-09-18T15:04:05.Z", "2026-09-18 15:04:05Z", "garbage", ""] {
            XCTAssertNil(SharedFormatters.utcTimestamp(raw), raw)
        }
        XCTAssertNil(SharedFormatters.utcTimestamp("2026-09-18T15:04:05+02:00"))
        XCTAssertEqual(SharedFormatters.iso8601Date(from: "2026-09-18T15:04:05+02:00")?.timeIntervalSince1970,
                       1_789_736_645)
    }
}
