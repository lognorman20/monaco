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
            "noRoute", "notEntitled", "fallbackSeries", "chainlinkSeries", "emptyChart", "chartFailed",
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

        // The chip tapped last is on screen by construction, so the row is swiped
        // from there rather than from the middle of the screen (the chart).
        var lastTapped = anyElement(app, "asset-chart-range-1D")
        for range in ["1D", "1W", "1M", "3M", "1Y", "ALL"] {
            let chip = anyElement(app, "asset-chart-range-\(range)")
            XCTAssertTrue(chip.waitForExistence(timeout: 5), "\(range) chip is missing")
            // Hittable is what makes this a real check: a chip clipped out of a fixed
            // row would never become hittable however far the row is swiped.
            var swipes = 0
            while !chip.isHittable && swipes < 4 {
                lastTapped.swipeLeft()
                swipes += 1
            }
            XCTAssertTrue(chip.isHittable, "\(range) chip cannot be reached")
            chip.tap()
            lastTapped = chip
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
}
