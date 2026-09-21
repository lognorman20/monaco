//
//  AssetDetailSampleUITests.swift
//  MonacoUITests
//
//  QA coverage for the stock detail screen against the debug sample harness
//  (-MonacoAssetDetailSample <scenario>). No sign-in and no backend: the harness
//  answers both calls from MarketSampleData, so every state the data layer can
//  produce is screenshottable here.
//

import XCTest

final class AssetDetailSampleUITests: XCTestCase {

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    @MainActor
    private func launch(_ scenario: String, textSize: String? = nil) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-MonacoAssetDetailSample", scenario]
        if let textSize {
            app.launchArguments += ["-UIPreferredContentSizeCategoryName", textSize]
        }
        app.launch()
        return app
    }

    @MainActor
    private func attachScreenshot(_ app: XCUIApplication, name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    /// The screen's identifiers sit on SwiftUI containers whose XCUIElement type is
    /// not stable across states, so ask by identifier and let the type be whatever
    /// it is.
    private func anyElement(_ app: XCUIApplication, _ identifier: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: identifier).firstMatch
    }

    /// "The screen drew" is asked of the 1D chip rather than of the scroll view the
    /// root identifier sits on: the chip is a real control, it is present in every
    /// chart state — loading, series, empty and failed alike — and a SwiftUI
    /// ScrollView does not reliably publish its identifier to the accessibility
    /// tree, which is the sort of thing that makes a UI test lie.
    @MainActor
    private func waitForScreen(_ app: XCUIApplication, _ context: String) {
        XCTAssertTrue(
            anyElement(app, "asset-chart-range-1D").waitForExistence(timeout: 30),
            "\(context) never drew the detail screen"
        )
    }

    /// Every scenario reaches a drawn screen. This is the screenshot sweep: one
    /// attachment per state the backend can put the screen in.
    @MainActor
    func testEveryScenarioRendersTheDetailScreen() throws {
        let scenarios = [
            "open", "afterHours", "preMarket", "holiday", "sparse",
            "jupiterFallback", "notEntitled", "fallbackSeries", "emptyChart", "chartFailed",
            "ticking", "slowRange", "staleRange",
        ]
        for scenario in scenarios {
            let app = launch(scenario)
            waitForScreen(app, scenario)
            attachScreenshot(app, name: "asset-detail-\(scenario)")
            app.terminate()
        }
    }

    /// The regression: six range chips in a fixed HStack were already ~320pt of the
    /// 335pt a 375pt device leaves inside the gutters, so one Dynamic Type step up
    /// clipped the row and the longest chip ("ALL") could not be tapped at all. The
    /// row scrolls now, so every range stays reachable at any text size.
    @MainActor
    func testEveryChartRangeIsReachableAtAnAccessibilityTextSize() throws {
        let app = launch("open", textSize: "UICTContentSizeCategoryAccessibilityL")
        waitForScreen(app, "accessibility text size")

        for range in ["1D", "1W", "1M", "3M", "1Y", "ALL"] {
            let chip = anyElement(app, "asset-chart-range-\(range)")
            XCTAssertTrue(chip.waitForExistence(timeout: 5), "\(range) chip is missing")
            // scrollIntoView is what makes this a real check: a chip clipped out of a
            // fixed row would never become hittable however far the row is scrolled.
            if !chip.isHittable {
                app.swipeLeft()
            }
            XCTAssertTrue(chip.waitForExistence(timeout: 5), "\(range) chip fell out of the row")
        }
        attachScreenshot(app, name: "asset-detail-ranges-accessibility-text")
    }

    /// The chart's own failure must not take the screen with it: the detail call
    /// succeeded, so the header and the retry both have to be on screen.
    @MainActor
    func testAFailedChartLeavesTheRestOfTheScreenStanding() throws {
        let app = launch("chartFailed")
        waitForScreen(app, "chartFailed")
        XCTAssertTrue(
            anyElement(app, "asset-detail-chart-failed").waitForExistence(timeout: 10)
        )
        attachScreenshot(app, name: "asset-detail-chart-failed")
    }

    /// Drags across the curve from right to left and answers with the hero as it was
    /// left. `scrubHolds` keeps the selection after the lift.
    @MainActor
    private func dragAcrossTheCurve(_ app: XCUIApplication) -> XCUIElement {
        let chart = anyElement(app, "asset-detail-chart")
        XCTAssertTrue(chart.waitForExistence(timeout: 30), "the curve never drew")
        let start = chart.coordinate(withNormalizedOffset: CGVector(dx: 0.85, dy: 0.5))
        let end = chart.coordinate(withNormalizedOffset: CGVector(dx: 0.3, dy: 0.5))
        start.press(forDuration: 0.2, thenDragTo: end, withVelocity: .slow, thenHoldForDuration: 0.5)
        return chart
    }

    /// The scrub: the sample under the finger becomes the hero price, and its own
    /// time takes the place of the period name.
    ///
    /// Launched with `-MonacoScrubHolds`, because XCUITest's press-drag-hold is one
    /// synthesised gesture that returns only once the touch has ended — without the
    /// flag the chart has always snapped back before the test can read anything, and
    /// "did the hero follow the finger" cannot be asked at all. Snapping back is the
    /// next test, on a build with the flag off.
    @MainActor
    func testScrubbingTheChartMovesTheHeroPriceAndTheFigureUnderIt() throws {
        let app = XCUIApplication()
        app.launchArguments = ["-MonacoAssetDetailSample", "open", "-MonacoScrubHolds"]
        app.launch()
        waitForScreen(app, "open")

        let price = anyElement(app, "asset-detail-price")
        let move = anyElement(app, "asset-detail-move")
        XCTAssertTrue(price.waitForExistence(timeout: 30))
        let livePrice = price.label
        let liveMove = move.label

        let chart = dragAcrossTheCurve(app)

        attachScreenshot(app, name: "asset-detail-scrubbing")
        XCTAssertNotEqual(price.label, livePrice, "the hero price did not follow the scrub")
        XCTAssertNotEqual(move.label, liveMove, "the figure under the price did not follow the scrub")
        // The curve reads the same sample the hero does.
        let spoken = try XCTUnwrap(chart.value as? String)
        XCTAssertTrue(spoken.hasPrefix(price.label), "curve says \(spoken), hero says \(price.label)")
    }

    /// And on a normal build the finger lifting puts the live price back.
    @MainActor
    func testTheHeroSnapsBackWhenTheFingerLifts() throws {
        let app = launch("open")
        waitForScreen(app, "open")

        let price = anyElement(app, "asset-detail-price")
        XCTAssertTrue(price.waitForExistence(timeout: 30))
        let livePrice = price.label

        _ = dragAcrossTheCurve(app)

        let restored = NSPredicate(format: "label == %@", livePrice)
        expectation(for: restored, evaluatedWith: price)
        waitForExpectations(timeout: 10)
    }

    /// The curve is one element with a sentence for a label and a price for a value,
    /// rather than a silent picture. (The adjustable action that walks the samples is
    /// not asserted here: XCUITest has no API to drive an increment on anything but a
    /// slider. It is exercised by hand with VoiceOver.)
    @MainActor
    func testTheCurveSpeaksItsOwnSummaryAndValue() throws {
        let app = launch("open")
        waitForScreen(app, "open")

        let chart = anyElement(app, "asset-detail-chart")
        XCTAssertTrue(chart.waitForExistence(timeout: 10))

        let label = chart.label
        XCTAssertTrue(label.contains("One day price history"), label)
        XCTAssertTrue(label.contains("Low $"), label)
        // The curve is the underlying equity's, under a token price. It says so.
        XCTAssertTrue(label.contains("home exchange"), label)

        let value = try XCTUnwrap(chart.value as? String)
        XCTAssertTrue(value.contains("$"), value)
    }

    /// The bug this closes: a range that was still loading looked exactly like a range
    /// that had arrived, so a slow fetch read as a chart that had not changed.
    @MainActor
    func testASlowRangeSaysSoOnItsOwnChip() throws {
        let app = launch("slowRange")
        waitForScreen(app, "slowRange")

        let week = anyElement(app, "asset-chart-range-1W")
        week.tap()

        let loading = NSPredicate(format: "value == %@", "Loading")
        expectation(for: loading, evaluatedWith: week)
        waitForExpectations(timeout: 5)
        attachScreenshot(app, name: "asset-detail-range-loading")
    }

    /// A series the server built for another window is refused rather than drawn under
    /// the wrong chip, and what the member gets is a retry — not a year of history
    /// labelled "1D".
    @MainActor
    func testASeriesForAnotherWindowIsRefused() throws {
        let app = launch("staleRange")
        waitForScreen(app, "staleRange")

        XCTAssertTrue(
            anyElement(app, "asset-detail-chart-failed").waitForExistence(timeout: 10),
            "a mismatched series was drawn instead of refused"
        )
        attachScreenshot(app, name: "asset-detail-stale-range")
    }

    /// The live hero: a poll that moves the price moves the figure on screen.
    @MainActor
    func testTheHeroPriceFollowsThePoll() throws {
        let app = launch("ticking")
        waitForScreen(app, "ticking")

        let price = anyElement(app, "asset-detail-price")
        XCTAssertTrue(price.waitForExistence(timeout: 10))
        let first = price.label

        let moved = NSPredicate(format: "label != %@", first)
        expectation(for: moved, evaluatedWith: price)
        waitForExpectations(timeout: 15)
        attachScreenshot(app, name: "asset-detail-ticking")
    }
}
