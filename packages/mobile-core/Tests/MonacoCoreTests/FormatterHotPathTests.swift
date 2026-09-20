import XCTest
@testable import MonacoCore

/// These formatters run once per visible row on every SwiftUI body pass, so they must stay
/// cheap and must give the same answer from any thread.
final class FormatterHotPathTests: XCTestCase {
    func testOneScreenOfLabels_staysWellInsideAFrame() {
        // The first call builds each formatter; scrolling cost is every call after that.
        renderOneScreenOfLabels()
        let started = Date()
        renderOneScreenOfLabels()
        let elapsedMilliseconds = Date().timeIntervalSince(started) * 1000
        // A 60Hz frame is 16.7ms. Building formatters per call took ~23ms here on a fast Mac.
        XCTAssertLessThan(elapsedMilliseconds, 8)
    }

    /// Roughly what a busy feed renders per pass: 60 money labels, 60 ages, 30 share counts.
    private func renderOneScreenOfLabels() {
        for index in 0..<60 {
            _ = UsdAmountFormatter.format(micros: Int64(index) * 1_234_567)
        }
        for _ in 0..<30 {
            _ = RelativeTimeFormatter.label(iso: "2026-09-18T15:04:05.123456Z")
            _ = ProposalTimeFormatter.ageLabel("2026-09-01T15:04:05Z")
            _ = ProposalShareFormatter.sharesLabel(fromAtomics: "120340000")
        }
    }

    func testSharedFormatters_giveTheSameAnswerFromManyThreads() {
        let expectedMoney = UsdAmountFormatter.format(micros: 1_250_500_000)
        let expectedShares = ProposalShareFormatter.sharesLabel(fromAtomics: "120340000")
        let expectedDate = ProposalTimeFormatter.parse("2026-09-18T15:04:05.123Z")
        XCTAssertEqual(expectedMoney, "$1,250.50")
        XCTAssertEqual(expectedShares, "1.2034 shares")
        XCTAssertNotNil(expectedDate)

        let mismatches = Mismatches()
        DispatchQueue.concurrentPerform(iterations: 2_000) { _ in
            if UsdAmountFormatter.format(micros: 1_250_500_000) != expectedMoney
                || ProposalShareFormatter.sharesLabel(fromAtomics: "120340000") != expectedShares
                || ProposalTimeFormatter.parse("2026-09-18T15:04:05.123Z") != expectedDate
                || RelativeTimeFormatter.parse("2026-09-18T15:04:05Z") == nil {
                mismatches.record()
            }
        }
        XCTAssertEqual(mismatches.count, 0)
    }

    func testRelativeLabel_followsTheCallersCalendar() {
        var tokyo = Calendar(identifier: .gregorian)
        tokyo.timeZone = TimeZone(identifier: "Asia/Tokyo")!
        var losAngeles = Calendar(identifier: .gregorian)
        losAngeles.timeZone = TimeZone(identifier: "America/Los_Angeles")!
        let now = ISO8601DateFormatter().date(from: "2026-09-18T12:00:00Z")!
        // 23:30 UTC on the 14th is already the 15th in Tokyo and still the 14th in LA.
        XCTAssertEqual(RelativeTimeFormatter.label(iso: "2026-09-14T23:30:00Z", now: now, calendar: tokyo), "Sep 15")
        XCTAssertEqual(RelativeTimeFormatter.label(iso: "2026-09-14T23:30:00Z", now: now, calendar: losAngeles), "Sep 14")
        XCTAssertEqual(RelativeTimeFormatter.label(iso: "2025-09-14T23:30:00Z", now: now, calendar: losAngeles), "Sep 14, 2025")
    }
}

private final class Mismatches: @unchecked Sendable {
    private let lock = NSLock()
    private var value = 0

    var count: Int {
        lock.lock()
        defer { lock.unlock() }
        return value
    }

    func record() {
        lock.lock()
        value += 1
        lock.unlock()
    }
}
