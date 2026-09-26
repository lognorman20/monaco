import XCTest
@testable import MonacoCore

final class AlertCopyTests: XCTestCase {
    private let calendar: Calendar = {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        return calendar
    }()
    private let now = SharedFormatters.iso8601Date(from: "2026-09-25T16:00:00Z")!

    private func alert(created: String, fired: String? = nil) -> PriceAlertDTO {
        PriceAlertDTO(
            id: "a", symbol: "GOOGLx", direction: .above, priceUsdcMicros: 360_000_000,
            active: fired == nil,
            createdAt: SharedFormatters.iso8601Date(from: created)!,
            triggeredAt: fired.flatMap(SharedFormatters.iso8601Date(from:))
        )
    }

    func testTheSentenceReadsTheWayAMemberSaysIt() {
        XCTAssertEqual(
            AlertCopy.sentence(name: "Google", direction: .above, lineUsdcMicros: 360_000_000),
            "Tell me when Google is above $360.00"
        )
        XCTAssertEqual(
            AlertCopy.sentence(name: "Berkshire Hathaway", direction: .below, lineUsdcMicros: 1_250_500_000),
            "Tell me when Berkshire Hathaway is below $1,250.50"
        )
    }

    func testRowTitles() {
        XCTAssertEqual(AlertCopy.line(direction: .above, lineUsdcMicros: 360_000_000), "Above $360.00")
        XCTAssertEqual(AlertCopy.line(direction: .below, lineUsdcMicros: 330_000_000), "Below $330.00")
        XCTAssertEqual(AlertCopy.reached(priceUsdcMicros: 361_254_000), "Reached $361.25")
    }

    func testPresetsMoveThePriceToTheCent() {
        let current: Int64 = 352_100_000
        XCTAssertEqual(AlertCopy.presetLine(currentPriceUsdcMicros: current, percent: 2, direction: .above), 359_140_000)
        XCTAssertEqual(AlertCopy.presetLine(currentPriceUsdcMicros: current, percent: 5, direction: .above), 369_710_000)
        XCTAssertEqual(AlertCopy.presetLine(currentPriceUsdcMicros: current, percent: 10, direction: .below), 316_890_000)
        XCTAssertEqual(AlertCopy.presetLabel(percent: 5, direction: .above), "+5%")
        XCTAssertEqual(AlertCopy.presetLabel(percent: 5, direction: .below), "\u{2212}5%")
    }

    func testEveryPresetIsOnTheFarSideOfThePrice() {
        for direction in PriceAlertDirection.allCases {
            for percent in AlertCopy.presetPercents {
                let line = AlertCopy.presetLine(currentPriceUsdcMicros: 41_250_000, percent: percent, direction: direction)
                XCTAssertFalse(direction.isReached(priceUsdcMicros: 41_250_000, lineUsdcMicros: line), "\(direction) \(percent)%")
            }
        }
    }

    func testProblemSaysWhereThePriceIsWhenTheLineIsAlreadyReached() {
        XCTAssertEqual(
            AlertCopy.problem(name: "Google", direction: .above, currentPriceUsdcMicros: 352_100_000, lineUsdcMicros: 350_000_000),
            "Google is at $352.10 now. Pick a price above that."
        )
        XCTAssertEqual(
            AlertCopy.problem(name: "Google", direction: .below, currentPriceUsdcMicros: 352_100_000, lineUsdcMicros: 352_100_000),
            "Google is at $352.10 now. Pick a price below that."
        )
    }

    func testNoProblemForAGoodLineOrAnEmptyField() {
        XCTAssertNil(AlertCopy.problem(name: "Google", direction: .above, currentPriceUsdcMicros: 352_100_000, lineUsdcMicros: 360_000_000))
        XCTAssertNil(AlertCopy.problem(name: "Google", direction: .above, currentPriceUsdcMicros: 352_100_000, lineUsdcMicros: nil))
        XCTAssertNil(AlertCopy.problem(name: "Google", direction: .above, currentPriceUsdcMicros: nil, lineUsdcMicros: 100_000))
    }

    func testALineOverTheCeilingIsAProblem() {
        XCTAssertEqual(
            AlertCopy.problem(name: "Google", direction: .above, currentPriceUsdcMicros: 352_100_000, lineUsdcMicros: AlertCopy.maximumLineUsdcMicros + 10_000),
            "Pick a price under $1,000,000."
        )
    }

    func testWaitingStampsCountUpThenBecomeADate() {
        XCTAssertEqual(AlertCopy.stamp(for: alert(created: "2026-09-25T15:59:30Z"), now: now, calendar: calendar), "Set just now")
        XCTAssertEqual(AlertCopy.stamp(for: alert(created: "2026-09-25T15:48:00Z"), now: now, calendar: calendar), "Set 12m ago")
        XCTAssertEqual(AlertCopy.stamp(for: alert(created: "2026-09-25T13:00:00Z"), now: now, calendar: calendar), "Set 3h ago")
        XCTAssertEqual(AlertCopy.stamp(for: alert(created: "2026-09-23T15:00:00Z"), now: now, calendar: calendar), "Set 2d ago")
        XCTAssertEqual(AlertCopy.stamp(for: alert(created: "2026-09-14T15:00:00Z"), now: now, calendar: calendar), "Set Sep 14")
        XCTAssertEqual(AlertCopy.stamp(for: alert(created: "2025-12-30T15:00:00Z"), now: now, calendar: calendar), "Set Dec 30, 2025")
    }

    func testFiredStampsBecomeADateAfterADay() {
        XCTAssertEqual(AlertCopy.stamp(for: alert(created: "2026-09-20T10:00:00Z", fired: "2026-09-25T13:00:00Z"), now: now, calendar: calendar), "Fired 3h ago")
        XCTAssertEqual(AlertCopy.stamp(for: alert(created: "2026-09-20T10:00:00Z", fired: "2026-09-24T09:00:00Z"), now: now, calendar: calendar), "Fired Sep 24")
    }

    func testSpokenStampUsesWords() {
        XCTAssertEqual(AlertCopy.spokenStamp(for: alert(created: "2026-09-23T15:00:00Z"), now: now, calendar: calendar), "Set 2 days ago")
        XCTAssertEqual(AlertCopy.spokenStamp(for: alert(created: "2026-09-25T13:00:00Z"), now: now, calendar: calendar), "Set 3 hours ago")
    }

    func testButtonTitles() {
        XCTAssertEqual(AlertCopy.buttonTitle(alertCount: 0), "Set alert")
        XCTAssertEqual(AlertCopy.buttonTitle(alertCount: 1), "1 alert")
        XCTAssertEqual(AlertCopy.buttonTitle(alertCount: 4), "4 alerts")
    }

    func testEverySentenceIsInTheProductsWords() {
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean(AlertCopy.auditStrings + WatchlistCopy.auditStrings))
    }
}
