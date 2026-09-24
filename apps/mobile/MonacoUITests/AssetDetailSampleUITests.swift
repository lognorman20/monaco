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
            "ticking", "tickingChart", "slowRange", "staleRange",
            "cabals", "oneCabal", "noCabals", "cabalsPartial", "cabalsFailed", "notRoutable",
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

    /// The regression: the 200pt chart height was applied to all four chart states,
    /// including the two empty ones. An `EmptyState` is a wrapping title, a message
    /// and a button — forced into a fixed height it does not clip, it overflows, and
    /// at the accessibility sizes the button drew straight over the caption and the
    /// chip row below it.
    @MainActor
    func testAnEmptyChartDoesNotDrawOverTheRangeRowAtAnAccessibilityTextSize() throws {
        for (scenario, identifier) in [
            ("emptyChart", "asset-detail-chart-empty"),
            ("chartFailed", "asset-detail-chart-failed"),
        ] {
            let app = launch(scenario, textSize: "UICTContentSizeCategoryAccessibilityL")
            waitForScreen(app, scenario)

            let state = anyElement(app, identifier)
            XCTAssertTrue(state.waitForExistence(timeout: 20), "\(scenario) never drew its empty state")
            let chip = anyElement(app, "asset-chart-range-1D")
            XCTAssertTrue(chip.waitForExistence(timeout: 5))
            // Both frames have to be real, or "they do not intersect" would be true
            // of two elements that were never laid out.
            XCTAssertGreaterThan(state.frame.height, 0, "\(scenario)'s empty state has no height")
            XCTAssertGreaterThan(chip.frame.height, 0, "\(scenario)'s 1D chip has no height")
            XCTAssertFalse(
                state.frame.intersects(chip.frame),
                "\(scenario) overflowed its height and drew over the range row"
            )
            // The retry inside it is a real control, not a label buried under the row.
            let retry = app.buttons.matching(
                NSPredicate(format: "label == 'Try again' OR label == 'Retry'")
            ).firstMatch
            XCTAssertTrue(retry.waitForExistence(timeout: 5), "\(scenario) has no retry")
            XCTAssertFalse(
                retry.frame.intersects(chip.frame),
                "\(scenario)'s retry button draws over the range row"
            )

            attachScreenshot(app, name: "asset-detail-\(scenario)-accessibility-text")
            app.terminate()
        }
    }

    /// The regression this closes: the scrub gesture had `minimumDistance: 0` over the
    /// whole plot, so it claimed every touch that started on the curve. The chart sits
    /// mid-screen inside a `ScrollView`, which made the most natural place to put a
    /// thumb the one place the screen would not scroll — the drag scrubbed instead.
    ///
    /// Both drags run in one test on purpose. "The vertical drag did not scrub" on its
    /// own could pass against a chart that cannot scrub at all; the horizontal drag
    /// that follows it, on the same screen in the same state, is what proves the
    /// scrub was available and declined.
    @MainActor
    func testAVerticalDragIsLeftToTheScrollViewAndAHorizontalOneScrubs() throws {
        let app = XCUIApplication()
        app.launchArguments = ["-MonacoAssetDetailSample", "open", "-MonacoScrubHolds"]
        app.launch()
        waitForScreen(app, "open")

        let price = anyElement(app, "asset-detail-price")
        XCTAssertTrue(price.waitForExistence(timeout: 30))
        let livePrice = price.label

        let chart = anyElement(app, "asset-detail-chart")
        XCTAssertTrue(chart.waitForExistence(timeout: 30), "the curve never drew")
        // Starts in the middle of the plot — the spot the old gesture swallowed —
        // and goes straight down, which is a scroll and nothing else. Downwards
        // rather than up so the screen bounces back to where it was and the
        // horizontal drag below still lands on the same pixels.
        let middle = chart.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        middle.press(
            forDuration: 0.1,
            thenDragTo: chart.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 1.8)),
            withVelocity: .default,
            thenHoldForDuration: 0.1
        )
        XCTAssertEqual(price.label, livePrice, "a vertical drag scrubbed instead of scrolling")

        // And the same screen still scrubs when the drag means to.
        _ = dragAcrossTheCurve(app)
        XCTAssertNotEqual(price.label, livePrice, "a horizontal drag no longer scrubs")
        attachScreenshot(app, name: "asset-detail-vertical-drag")
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
    ///
    /// The same drag is run twice: once on a build that holds the selection, to learn
    /// what the screen looks like *while* a finger is down, and then on a shipping
    /// build. Without the first half this test could pass against a chart that never
    /// scrubbed at all — the `open` sample's price does not move, so "the label is
    /// what it was" is true of a dead gesture too.
    @MainActor
    func testTheHeroSnapsBackWhenTheFingerLifts() throws {
        let held = XCUIApplication()
        held.launchArguments = ["-MonacoAssetDetailSample", "open", "-MonacoScrubHolds"]
        held.launch()
        waitForScreen(held, "open")
        let heldPrice = anyElement(held, "asset-detail-price")
        XCTAssertTrue(heldPrice.waitForExistence(timeout: 30))
        let livePrice = heldPrice.label
        let heldChart = dragAcrossTheCurve(held)
        let scrubbedPrice = heldPrice.label
        let scrubbedValue = try XCTUnwrap(heldChart.value as? String)
        XCTAssertNotEqual(scrubbedPrice, livePrice, "the drag never reached the curve")
        held.terminate()

        let app = launch("open")
        waitForScreen(app, "open")
        let price = anyElement(app, "asset-detail-price")
        XCTAssertTrue(price.waitForExistence(timeout: 30))
        XCTAssertEqual(price.label, livePrice, "the two builds started from different prices")

        let chart = dragAcrossTheCurve(app)

        let restored = NSPredicate(format: "label == %@", livePrice)
        expectation(for: restored, evaluatedWith: price)
        waitForExpectations(timeout: 10)
        // The curve itself is back on its last sample too, not still reading the one
        // the finger was over.
        let value = try XCTUnwrap(chart.value as? String)
        XCTAssertNotEqual(value, scrubbedValue, "the curve is still reading the scrubbed sample")
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
