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

    /// Every scenario reaches a drawn screen. This is the screenshot sweep: one
    /// attachment per state the backend can put the screen in.
    @MainActor
    func testEveryScenarioRendersTheDetailScreen() throws {
        let scenarios = [
            "open", "afterHours", "preMarket", "holiday", "sparse",
            "jupiterFallback", "notEntitled", "fallbackSeries", "emptyChart", "chartFailed",
        ]
        for scenario in scenarios {
            let app = launch(scenario)
            XCTAssertTrue(
                app.otherElements["asset-detail-root"].waitForExistence(timeout: 20),
                "\(scenario) never drew the detail screen"
            )
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
        XCTAssertTrue(app.otherElements["asset-detail-root"].waitForExistence(timeout: 20))

        for range in ["1D", "1W", "1M", "3M", "1Y", "ALL"] {
            let chip = app.descendants(matching: .any)
                .matching(identifier: "asset-chart-range-\(range)").firstMatch
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
        XCTAssertTrue(app.otherElements["asset-detail-root"].waitForExistence(timeout: 20))
        XCTAssertTrue(
            app.descendants(matching: .any)
                .matching(identifier: "asset-detail-chart-failed").firstMatch
                .waitForExistence(timeout: 10)
        )
        attachScreenshot(app, name: "asset-detail-chart-failed")
    }
}
