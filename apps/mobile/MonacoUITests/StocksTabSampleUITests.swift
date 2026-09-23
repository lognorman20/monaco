//
//  StocksTabSampleUITests.swift
//  MonacoUITests
//
//  QA coverage for the Stocks tab, the stock detail and the cabal picker against the debug
//  sample harness (launch argument -MonacoStocksTabSample, optionally followed by a
//  scenario). No sign-in and no backend: the app boots into a Stocks tab backed by
//  StocksTabSampleData and MarketSampleData, with three joined cabals. One holds Apple, one
//  holds only cash, and one never answers.
//
//  Everything is asked of an element that really publishes itself: the navigation bar, a
//  section header's text, a row's button. The identifiers on the SwiftUI containers around
//  them are for reading the hierarchy, not for finding it.
//

import XCTest

final class StocksTabSampleUITests: XCTestCase {
    private let holderId = "5a0c7d10-0001-4b1e-8f00-000000000001"
    private let cashOnlyId = "5a0c7d10-0002-4b1e-8f00-000000000002"
    private let silentId = "5a0c7d10-0003-4b1e-8f00-000000000003"

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    @MainActor
    private func launchApp(_ scenario: String? = nil, textSize: String? = nil) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-MonacoStocksTabSample"]
        if let scenario {
            app.launchArguments.append(scenario)
        }
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

    /// "The tab drew" is asked of the navigation bar: it is there in every state, including
    /// the skeleton and both failures.
    @MainActor
    private func waitForTab(_ app: XCUIApplication, _ context: String) {
        XCTAssertTrue(
            app.navigationBars["Stocks"].waitForExistence(timeout: 30),
            "\(context) never drew the Stocks tab"
        )
    }

    /// The search field names itself `monaco-search-field` (the design system's name). The tab
    /// no longer relabels it, so there is one identifier to ask for.
    @MainActor
    private func searchField(_ app: XCUIApplication) -> XCUIElement {
        app.textFields["monaco-search-field"].firstMatch
    }

    /// Opens a stock from its row in "In your cabals": the first section is always built,
    /// while Popular, fourth in a LazyVStack, may be below the fold. Tapped a quarter of the
    /// way in, over the logo and name: the trailing edge is the day pill, which is its own
    /// control and switches % and $.
    @MainActor
    private func openHeld(_ app: XCUIApplication, symbol: String) {
        let row = anyElement(app, "assets-held-\(symbol)")
        XCTAssertTrue(row.waitForExistence(timeout: 25), "held row \(symbol)")
        row.coordinate(withNormalizedOffset: CGVector(dx: 0.25, dy: 0.5)).tap()
        XCTAssertTrue(anyElement(app, "asset-detail-root").waitForExistence(timeout: 10))
    }

    // MARK: - The four sections

    /// Every scenario reaches a drawn tab. This is the screenshot sweep: one attachment per
    /// state the backend can put the tab in.
    @MainActor
    func testEveryScenarioDraws() throws {
        for scenario in ["full", "noCabals", "cabalsFailed", "cabalsStale", "popularFailed", "loading", "noSeries", "afterHours"] {
            let app = launchApp(scenario)
            waitForTab(app, scenario)
            attachScreenshot(app, name: "stocks-\(scenario)")
            app.terminate()
        }
    }

    /// The populated tab shows all four sections in order, and a held row carries the line
    /// that makes the section personal.
    @MainActor
    func testPopulatedTabShowsEverySection() throws {
        let app = launchApp("full")
        waitForTab(app, "full")

        XCTAssertTrue(app.staticTexts["In your cabals"].waitForExistence(timeout: 25), "In your cabals is missing")
        XCTAssertTrue(app.staticTexts["Up for vote"].exists, "Up for vote is missing")
        XCTAssertTrue(app.staticTexts["Top movers"].exists, "Top movers is missing")
        XCTAssertTrue(anyElement(app, "assets-held-AAPLc").exists, "the held row for AAPLc is missing")
        attachScreenshot(app, name: "stocks-sections")
    }

    /// A member whose cabals own nothing gets an answer, not a failure.
    @MainActor
    func testNoCabalsIsAnEmptyStateNotAnError() throws {
        let app = launchApp("noCabals")
        waitForTab(app, "noCabals")

        XCTAssertTrue(app.staticTexts["Your cabals own nothing yet"].waitForExistence(timeout: 25))
        XCTAssertFalse(app.staticTexts["Could not load your cabals"].exists)
        // Up for vote hides itself entirely rather than teaching people to skip it.
        XCTAssertFalse(app.staticTexts["Up for vote"].exists)
    }

    /// A failed cabal read is its own failure: the market below it stays live.
    @MainActor
    func testAFailedCabalReadLeavesTheMarketAlone() throws {
        let app = launchApp("cabalsFailed")
        waitForTab(app, "cabalsFailed")

        XCTAssertTrue(app.staticTexts["Could not load your cabals"].waitForExistence(timeout: 25))
        XCTAssertTrue(app.staticTexts["Top movers"].exists, "the market should still be listed")
        attachScreenshot(app, name: "stocks-cabals-failed")
    }

    /// A catalogue that will not load gets the retry, not a half-empty tab.
    @MainActor
    func testAFailedCatalogueOffersRetry() throws {
        let app = launchApp("popularFailed")
        waitForTab(app, "popularFailed")

        XCTAssertTrue(app.staticTexts["Could not load popular stocks"].waitForExistence(timeout: 25))
        XCTAssertTrue(app.buttons["Retry"].exists)
    }

    /// A symbol with no day series still renders as a row, with no line rather than a flat one.
    @MainActor
    func testASymbolWithoutADaySeriesStillRenders() throws {
        let app = launchApp("noSeries")
        waitForTab(app, "noSeries")

        XCTAssertTrue(anyElement(app, "assets-held-AAPLc").waitForExistence(timeout: 25))
        attachScreenshot(app, name: "stocks-no-series")
    }

    /// A cabal read that lands and is then not refreshed keeps its rows, which are the
    /// member's own money, but says so. Silence here reads as "this is fresh".
    @MainActor
    func testAStaleCabalSectionSaysItIsStale() throws {
        let app = launchApp("cabalsStale")
        waitForTab(app, "cabalsStale")

        XCTAssertTrue(
            anyElement(app, "assets-held-AAPLc").waitForExistence(timeout: 25),
            "the rows should survive the failed refresh"
        )
        XCTAssertTrue(
            anyElement(app, "assets-held-stale").waitForExistence(timeout: 10),
            "a stale cabal section must carry the same caption the search region uses"
        )
        XCTAssertFalse(app.staticTexts["Could not load your cabals"].exists, "rows on screen is not the failure state")
        attachScreenshot(app, name: "stocks-cabals-stale")
    }

    /// The rows stack at accessibility text sizes rather than squeezing the name out, and the
    /// mover strip, a scanning affordance, steps aside: every mover is also in Popular.
    @MainActor
    func testTheMoverStripStepsAsideAtAccessibilityTextSizes() throws {
        let normal = launchApp("full")
        waitForTab(normal, "full")
        XCTAssertTrue(normal.staticTexts["Top movers"].waitForExistence(timeout: 25))
        XCTAssertTrue(anyElement(normal, "assets-mover-NVDAc").exists, "a mover card should be built")
        attachScreenshot(normal, name: "stocks-movers-default")
        normal.terminate()

        let large = launchApp("full", textSize: "UICTContentSizeCategoryAccessibilityXXXL")
        waitForTab(large, "full at AX5")
        XCTAssertTrue(anyElement(large, "assets-held-AAPLc").waitForExistence(timeout: 30))
        XCTAssertFalse(large.staticTexts["Top movers"].exists, "the strip should be hidden rather than overflowing")
        attachScreenshot(large, name: "stocks-ax5")
    }

    /// The day pill has a 44pt tap target. Growing it must not grow what it swallows: the
    /// middle of the row still opens the stock.
    @MainActor
    func testTappingTheMiddleOfARowOpensTheStock() throws {
        let app = launchApp("full")
        waitForTab(app, "full")

        let row = anyElement(app, "assets-held-AAPLc")
        XCTAssertTrue(row.waitForExistence(timeout: 25))
        row.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
        XCTAssertTrue(anyElement(app, "asset-detail-root").waitForExistence(timeout: 10))
    }

    // MARK: - Search

    /// Searching replaces the sections and opens the detail; clearing brings the sections back.
    @MainActor
    func testSearchReplacesTheSectionsAndOpensTheDetail() throws {
        let app = launchApp()
        waitForTab(app, "default")
        XCTAssertTrue(app.staticTexts["In your cabals"].waitForExistence(timeout: 25))
        attachScreenshot(app, name: "stocks-browse")

        let field = searchField(app)
        XCTAssertTrue(field.waitForExistence(timeout: 15), app.debugDescription)
        field.tap()
        field.typeText("tes")

        XCTAssertTrue(anyElement(app, "assets-row-TSLAc").waitForExistence(timeout: 25), "Tesla matches")
        XCTAssertFalse(anyElement(app, "assets-row-AAPLc").exists, "Apple does not match \"tes\"")
        XCTAssertFalse(app.staticTexts["In your cabals"].exists, "a search is a different question")
        attachScreenshot(app, name: "stocks-search")

        app.buttons["Clear search"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["In your cabals"].waitForExistence(timeout: 25))
        XCTAssertFalse(anyElement(app, "assets-row-TSLAc").exists, "the search results should be gone")

        field.tap()
        field.typeText("tes")
        let tesla = anyElement(app, "assets-row-TSLAc")
        XCTAssertTrue(tesla.waitForExistence(timeout: 25))
        tesla.coordinate(withNormalizedOffset: CGVector(dx: 0.25, dy: 0.5)).tap()
        XCTAssertTrue(anyElement(app, "asset-detail-root").waitForExistence(timeout: 10))
    }

    // MARK: - Detail

    /// The figure under the price measures the window the curve draws, and says which.
    @MainActor
    func testTheHeaderFigureFollowsTheChartRange() throws {
        let app = launchApp()
        openHeld(app, symbol: "AAPLc")

        XCTAssertTrue(anyElement(app, "asset-detail-chart").waitForExistence(timeout: 10))
        let move = anyElement(app, "asset-detail-move")
        XCTAssertTrue(move.waitForExistence(timeout: 10))
        XCTAssertTrue(move.label.contains("Past day"), "got \(move.label)")
        attachScreenshot(app, name: "stock-detail-1D")

        anyElement(app, "asset-chart-range-1W").tap()
        let week = NSPredicate(format: "label CONTAINS %@", "Past week")
        expectation(for: week, evaluatedWith: move)
        waitForExpectations(timeout: 10)
        attachScreenshot(app, name: "stock-detail-1W")
    }

    // MARK: - Cabal picker

    /// Buy lists every cabal that answered, counts the one that did not, and a pick goes
    /// straight to the amount step with the stock already set.
    @MainActor
    func testBuyGoesStraightToTheAmountStep() throws {
        let app = launchApp()
        openHeld(app, symbol: "AAPLc")

        let buy = app.buttons["asset-detail-buy"]
        XCTAssertTrue(buy.waitForExistence(timeout: 10))
        XCTAssertTrue(buy.isEnabled, "a routable stock can be bought")
        buy.tap()

        XCTAssertTrue(anyElement(app, "group-picker-root").waitForExistence(timeout: 10))
        let holder = anyElement(app, "pick-cabal-\(holderId)")
        XCTAssertTrue(holder.waitForExistence(timeout: 10))
        XCTAssertTrue(anyElement(app, "pick-cabal-\(cashOnlyId)").exists, "a cash-only cabal can still buy")
        XCTAssertFalse(anyElement(app, "pick-cabal-\(silentId)").exists, "a cabal that did not answer has no pot to offer")
        XCTAssertTrue(anyElement(app, "group-picker-unreachable").exists, "the silent cabal is counted, not hidden")
        attachScreenshot(app, name: "stock-picker-buy")

        holder.tap()
        XCTAssertTrue(
            anyElement(app, "propose-amount").waitForExistence(timeout: 10)
                || app.textFields["amount-entry-field"].waitForExistence(timeout: 5),
            "amount step with the stock already set"
        )
        attachScreenshot(app, name: "stock-picker-amount")
    }

    /// Sell offers only the cabal that holds the stock, and never dead-ends after the pick.
    @MainActor
    func testSellListsOnlyTheCabalThatHoldsTheStock() throws {
        let app = launchApp()
        openHeld(app, symbol: "AAPLc")

        let sell = app.buttons["asset-detail-sell"]
        XCTAssertTrue(sell.waitForExistence(timeout: 10))
        sell.tap()

        XCTAssertTrue(anyElement(app, "pick-cabal-\(holderId)").waitForExistence(timeout: 10))
        XCTAssertFalse(anyElement(app, "pick-cabal-\(cashOnlyId)").exists, "a cabal without Apple is not offered for a sell")
        XCTAssertTrue(anyElement(app, "group-picker-unreachable").exists)
        attachScreenshot(app, name: "stock-picker-sell")
    }

    /// With a cabal silent, "none of your cabals hold this" is not ours to say.
    @MainActor
    func testNoHolderIsNotClaimedWhileACabalIsSilent() throws {
        let app = launchApp()
        openHeld(app, symbol: "TSLAc")

        let sell = app.buttons["asset-detail-sell"]
        XCTAssertTrue(sell.waitForExistence(timeout: 10))
        sell.tap()

        XCTAssertTrue(anyElement(app, "group-picker-holdings-failed").waitForExistence(timeout: 10))
        XCTAssertFalse(anyElement(app, "group-picker-no-holders").exists)
        attachScreenshot(app, name: "stock-picker-sell-partial")
    }
}
