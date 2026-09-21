//
//  StocksTabSampleUITests.swift
//  MonacoUITests
//
//  QA coverage for the Stocks tab, the stock detail and the cabal picker against the debug
//  sample harness (launch argument -MonacoStocksTabSample). No sign-in and no backend: the
//  app boots into a Stocks tab backed by StocksTabSampleData, with three joined cabals. One
//  holds Apple, one holds only cash, and one never answers.
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
    private func launchApp() -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-MonacoStocksTabSample"]
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

    /// `assets-search-field` is set on the MonacoSearchField container and the text field inside
    /// carries `monaco-search-field`; which one XCUITest resolves depends on how SwiftUI flattens
    /// the pair, so try both before settling for the screen's only text field.
    @MainActor
    private func searchField(_ app: XCUIApplication) -> XCUIElement {
        for candidate in [app.textFields["assets-search-field"], app.textFields["monaco-search-field"]]
        where candidate.waitForExistence(timeout: 2) {
            return candidate
        }
        return app.textFields.firstMatch
    }

    @MainActor
    private func openDetail(_ app: XCUIApplication, symbol: String) {
        let row = anyElement(app, "assets-popular-\(symbol)")
        XCTAssertTrue(row.waitForExistence(timeout: 10), "popular row \(symbol)")
        row.tap()
        XCTAssertTrue(anyElement(app, "asset-detail-root").waitForExistence(timeout: 10))
    }

    // MARK: - Tab and search

    @MainActor
    func testSearchReplacesPopularAndOpensTheDetail() throws {
        let app = launchApp()

        XCTAssertTrue(anyElement(app, "assets-root").waitForExistence(timeout: 10))
        XCTAssertTrue(anyElement(app, "assets-popular-AAPLc").waitForExistence(timeout: 10))
        attachScreenshot(app, name: "stocks-popular")

        let field = searchField(app)
        XCTAssertTrue(field.waitForExistence(timeout: 5), app.debugDescription)
        field.tap()
        field.typeText("tes")

        XCTAssertTrue(anyElement(app, "assets-row-TSLAc").waitForExistence(timeout: 10), "Tesla matches")
        XCTAssertFalse(anyElement(app, "assets-row-AAPLc").exists, "Apple does not match \"tes\"")
        XCTAssertFalse(anyElement(app, "assets-popular-AAPLc").exists, "search replaces Popular")
        attachScreenshot(app, name: "stocks-search")

        anyElement(app, "assets-row-TSLAc").tap()
        XCTAssertTrue(anyElement(app, "asset-detail-root").waitForExistence(timeout: 10))
    }

    // MARK: - Detail

    /// The figure under the price measures the window the curve draws, and says which.
    @MainActor
    func testTheHeaderFigureFollowsTheChartRange() throws {
        let app = launchApp()
        openDetail(app, symbol: "AAPLc")

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
        openDetail(app, symbol: "AAPLc")

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
        openDetail(app, symbol: "AAPLc")

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
        openDetail(app, symbol: "TSLAc")

        let sell = app.buttons["asset-detail-sell"]
        XCTAssertTrue(sell.waitForExistence(timeout: 10))
        sell.tap()

        XCTAssertTrue(anyElement(app, "group-picker-holdings-failed").waitForExistence(timeout: 10))
        XCTAssertFalse(anyElement(app, "group-picker-no-holders").exists)
        attachScreenshot(app, name: "stock-picker-sell-partial")
    }
}
