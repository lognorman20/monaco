//
//  StocksTabSampleUITests.swift
//  MonacoUITests
//
//  QA coverage for the Stocks tab against the debug sample harness
//  (-MonacoStocksTabSample <scenario>). No sign-in and no backend: the harness
//  answers all three reads from MarketSampleData, so every state the four
//  sections can be in is screenshottable here.
//
//  Everything is asked of an element that really publishes itself: the navigation
//  bar, a section header's text, a row's button. The identifiers on the SwiftUI
//  containers around them are for reading the hierarchy, not for finding it — a
//  VStack does not reliably reach the accessibility tree, and a test that waits on
//  one is a test that lies.
//

import XCTest

final class StocksTabSampleUITests: XCTestCase {

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    @MainActor
    private func launch(_ scenario: String, textSize: String? = nil) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-MonacoStocksTabSample", scenario]
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

    private func anyElement(_ app: XCUIApplication, _ identifier: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: identifier).firstMatch
    }

    /// "The tab drew" is asked of the navigation bar: it is there in every state,
    /// including the skeleton and both failures.
    @MainActor
    private func waitForTab(_ app: XCUIApplication, _ context: String) {
        XCTAssertTrue(
            app.navigationBars["Stocks"].waitForExistence(timeout: 30),
            "\(context) never drew the Stocks tab"
        )
    }

    /// Every scenario reaches a drawn tab. This is the screenshot sweep: one
    /// attachment per state the backend can put the tab in.
    @MainActor
    func testEveryScenarioDraws() throws {
        for scenario in ["full", "noCabals", "cabalsFailed", "popularFailed", "loading", "noSeries", "afterHours"] {
            let app = launch(scenario)
            waitForTab(app, scenario)
            attachScreenshot(app, name: "stocks-\(scenario)")
            app.terminate()
        }
    }

    /// The populated tab shows all four sections in order, and a held row carries
    /// the line that makes the section personal.
    @MainActor
    func testPopulatedTabShowsEverySection() throws {
        let app = launch("full")
        waitForTab(app, "full")

        XCTAssertTrue(app.staticTexts["In your cabals"].waitForExistence(timeout: 25), "In your cabals is missing")
        XCTAssertTrue(app.staticTexts["Up for vote"].exists, "Up for vote is missing")
        XCTAssertTrue(app.staticTexts["Top movers"].exists, "Top movers is missing")
        XCTAssertTrue(app.staticTexts["Popular"].exists, "Popular is missing")
        XCTAssertTrue(anyElement(app, "assets-held-AAPLx").exists, "the held row for AAPLx is missing")
        attachScreenshot(app, name: "stocks-sections")
    }

    /// A member whose cabals own nothing gets an answer, not a failure.
    @MainActor
    func testNoCabalsIsAnEmptyStateNotAnError() throws {
        let app = launch("noCabals")
        waitForTab(app, "noCabals")

        XCTAssertTrue(app.staticTexts["Your cabals own nothing yet"].waitForExistence(timeout: 25))
        XCTAssertFalse(app.staticTexts["Could not load your cabals"].exists)
        // Up for vote hides itself entirely rather than teaching people to skip it.
        XCTAssertFalse(app.staticTexts["Up for vote"].exists)
        XCTAssertTrue(app.staticTexts["Popular"].exists, "the market is still live")
    }

    /// A failed cabal read is its own failure: the market below it stays live.
    @MainActor
    func testAFailedCabalReadLeavesTheMarketAlone() throws {
        let app = launch("cabalsFailed")
        waitForTab(app, "cabalsFailed")

        XCTAssertTrue(app.staticTexts["Could not load your cabals"].waitForExistence(timeout: 25))
        XCTAssertTrue(app.staticTexts["Popular"].exists, "the catalogue should still be listed")
        attachScreenshot(app, name: "stocks-cabals-failed")
    }

    /// A catalogue that will not load gets the retry, not a half-empty tab.
    @MainActor
    func testAFailedCatalogueOffersRetry() throws {
        let app = launch("popularFailed")
        waitForTab(app, "popularFailed")

        XCTAssertTrue(app.staticTexts["Could not load popular stocks"].waitForExistence(timeout: 25))
        XCTAssertTrue(app.buttons["Retry"].exists)
    }

    /// A symbol with no day series still renders as a row. The old list would have
    /// been happy to draw a flat line here, which reads as "it did not move".
    @MainActor
    func testASymbolWithoutADaySeriesStillRenders() throws {
        let app = launch("noSeries")
        waitForTab(app, "noSeries")

        XCTAssertTrue(anyElement(app, "assets-popular-AAPLx").waitForExistence(timeout: 25))
        attachScreenshot(app, name: "stocks-no-series")
    }

    /// Searching replaces the sections; clearing the field brings them back.
    @MainActor
    func testSearchReplacesTheSectionsAndComesBack() throws {
        let app = launch("full")
        waitForTab(app, "full")
        XCTAssertTrue(app.staticTexts["Popular"].waitForExistence(timeout: 25))

        let field = app.textFields["monaco-search-field"].firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 15))
        field.tap()
        field.typeText("Tesla")

        XCTAssertTrue(anyElement(app, "assets-row-TSLAx").waitForExistence(timeout: 25), "search results never drew")
        XCTAssertFalse(app.staticTexts["In your cabals"].exists, "a search is a different question")

        // Clearing puts the sections back. Asked of the first section rather than
        // "Popular": the browse list is a LazyVStack, and with the keyboard still up
        // the viewport is short enough that the fourth section is not built yet.
        // "In your cabals" is always built, so it is the honest signal that browsing
        // came back — waiting on a section that may never be instantiated is a test
        // that fails for a reason that has nothing to do with the behaviour.
        app.buttons["Clear search"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["In your cabals"].waitForExistence(timeout: 25))
        XCTAssertFalse(anyElement(app, "assets-row-TSLAx").exists, "the search results should be gone")
    }

    /// The rows stack at accessibility text sizes rather than squeezing the name
    /// out, and the tab still draws.
    @MainActor
    func testTheTabSurvivesAccessibilityTextSizes() throws {
        let app = launch("full", textSize: "UICTContentSizeCategoryAccessibilityXXXL")
        waitForTab(app, "full at AX5")

        // The held row, not a popular one: at AX5 every row is tall enough that the
        // fourth section is far below the fold and the LazyVStack has not built it.
        // A row in the first section proves the same thing — rows render, stacked,
        // at accessibility text sizes.
        XCTAssertTrue(anyElement(app, "assets-held-AAPLx").waitForExistence(timeout: 30))
        attachScreenshot(app, name: "stocks-ax5")
    }

    /// Tapping a row opens the stock it is about. The pushed screen is the live
    /// detail view, which has no backend here, so what is asserted is that the tab
    /// was left at all: its navigation bar is gone once it has been.
    @MainActor
    func testTappingARowOpensTheStock() throws {
        let app = launch("full")
        waitForTab(app, "full")

        let row = anyElement(app, "assets-held-AAPLx")
        XCTAssertTrue(row.waitForExistence(timeout: 25))
        // Tapped a quarter of the way in, over the logo and name, rather than at the
        // element's own hit point: that lands on the right-hand edge of the row,
        // where the day-change pill is its own control and deliberately swallows the
        // tap to switch % and $. Opening the stock is what the rest of the row does.
        row.coordinate(withNormalizedOffset: CGVector(dx: 0.25, dy: 0.5)).tap()

        let leftTheTab = NSPredicate(format: "exists == false")
        expectation(for: leftTheTab, evaluatedWith: app.navigationBars["Stocks"])
        waitForExpectations(timeout: 25)
    }
}
