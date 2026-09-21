//
//  AssetDetailCardsUITests.swift
//  MonacoUITests
//
//  The cards under the chart, against the debug sample harness
//  (-MonacoAssetDetailSample <scenario>). No sign-in and no backend.
//
//  These check the things a screenshot of the top of the screen cannot: that each
//  card is reachable by scrolling, that the ones with nothing to say are absent
//  rather than empty, and that the trade bar offers a sell only when a cabal
//  actually holds the stock.
//

import XCTest

final class AssetDetailCardsUITests: XCTestCase {

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

    /// The identifiers sit on SwiftUI containers whose XCUIElement type is not stable
    /// across states, so ask by identifier and let the type be whatever it is.
    private func anyElement(_ app: XCUIApplication, _ identifier: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: identifier).firstMatch
    }

    @MainActor
    private func waitForScreen(_ app: XCUIApplication, _ context: String) {
        XCTAssertTrue(
            anyElement(app, "asset-chart-range-1D").waitForExistence(timeout: 30),
            "\(context) never drew the detail screen"
        )
    }

    /// Scrolls until the element exists, or gives up. A card below the fold is not a
    /// missing card, and asserting on `exists` without scrolling would pass or fail
    /// on where the screen happened to settle.
    ///
    /// It scrolls back up as well as down. Cells inside a `LazyVGrid` only exist while
    /// they are near the viewport, and at the accessibility text sizes a single swipe
    /// can clear a whole card — so "swipe down eight times" alone can pass the target
    /// on the way and then report it missing.
    @MainActor
    @discardableResult
    private func scrollTo(_ app: XCUIApplication, _ identifier: String, attempts: Int = 8) -> Bool {
        for _ in 0..<attempts {
            if anyElement(app, identifier).exists { return true }
            app.swipeUp(velocity: .slow)
        }
        for _ in 0..<attempts {
            if anyElement(app, identifier).exists { return true }
            app.swipeDown(velocity: .slow)
        }
        return anyElement(app, identifier).exists
    }

    /// Whatever stats cells the tree currently holds, so a failure says what it found
    /// instead of only what it wanted.
    @MainActor
    private func visibleStatIdentifiers(_ app: XCUIApplication) -> [String] {
        app.descendants(matching: .any)
            .allElementsBoundByIndex
            .map(\.identifier)
            .filter { $0.hasPrefix("asset-stat") }
    }

    @MainActor
    private func attachScreenshot(_ app: XCUIApplication, name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    /// Every card in its place, in the order the slot list declares.
    @MainActor
    func testEveryCardIsReachableWhenThereIsSomethingToSay() throws {
        let app = launch("cabals")
        waitForScreen(app, "cabals")

        for identifier in [
            "asset-detail-position",
            "asset-detail-stats",
            "asset-detail-stock-vs-token",
            "asset-detail-about",
            "asset-detail-activity",
        ] {
            XCTAssertTrue(scrollTo(app, identifier), "\(identifier) never appeared")
            attachScreenshot(app, name: identifier)
        }
    }

    /// The cards are not decorations with empty states. Nothing held, nothing voted
    /// and nothing done means the position and activity cards are simply not there,
    /// and the screen is shorter rather than padded with empty frames.
    @MainActor
    func testCardsWithNothingToSayAreAbsentRatherThanEmpty() throws {
        let app = launch("noCabals")
        waitForScreen(app, "noCabals")

        // Scroll to the bottom so "not there" is a real answer rather than
        // "not there yet".
        for _ in 0..<8 { app.swipeUp() }

        XCTAssertFalse(anyElement(app, "asset-detail-position").exists, "the position card drew with nothing to show")
        XCTAssertFalse(anyElement(app, "asset-detail-activity").exists, "the activity card drew with nothing to show")
        // The market cards do not depend on the member's cabals and must still be there.
        XCTAssertTrue(anyElement(app, "asset-detail-about").exists, "the About card should not depend on cabals")
        attachScreenshot(app, name: "asset-detail-no-cabals")
    }

    /// The dead end this bar exists to remove: Sell used to be offered
    /// unconditionally and then walked the member into "This cabal does not hold
    /// this stock."
    @MainActor
    func testSellIsOfferedOnlyWhenACabalHoldsTheStock() throws {
        let held = launch("cabals")
        waitForScreen(held, "cabals")
        XCTAssertTrue(
            held.buttons["asset-detail-sell"].waitForExistence(timeout: 10),
            "a stock three cabals hold should offer a sell"
        )
        held.terminate()

        let unheld = launch("noCabals")
        waitForScreen(unheld, "noCabals")
        // The buy button proves the bar itself drew, so "no sell" is a decision and
        // not a bar that never appeared.
        XCTAssertTrue(unheld.buttons["asset-detail-buy"].waitForExistence(timeout: 10))
        XCTAssertFalse(
            unheld.buttons["asset-detail-sell"].exists,
            "a stock no cabal holds must not offer a sell"
        )
    }

    /// An unroutable token says so next to the button it disables, rather than in a
    /// toast after a tap that was never going to work.
    @MainActor
    func testUnroutableTokenExplainsItselfInTheBar() throws {
        let app = launch("notRoutable")
        waitForScreen(app, "notRoutable")

        XCTAssertTrue(
            anyElement(app, "asset-trade-bar-blocked").waitForExistence(timeout: 10),
            "the bar never said why buying is off"
        )
        XCTAssertFalse(app.buttons["asset-detail-buy"].isEnabled, "buy should be disabled with no route")
        attachScreenshot(app, name: "asset-detail-not-routable")
    }

    /// A read that failed is not an answer of "nobody holds this". The card says so
    /// and offers the way back, and the bar does not let a missing Sell button make
    /// the claim the card refused to make.
    @MainActor
    func testAFailedSocialReadSaysSoRatherThanShowingNothing() throws {
        let app = launch("cabalsFailed")
        waitForScreen(app, "cabalsFailed")

        XCTAssertTrue(
            scrollTo(app, "asset-detail-position-failed"),
            "a failed read drew no card at all, which reads as 'no cabal holds this'"
        )
        XCTAssertTrue(app.buttons["asset-position-retry"].exists, "no way back from the failure")
        XCTAssertTrue(
            anyElement(app, "asset-trade-bar-sell-unknown").exists,
            "the bar let an absent Sell button speak for a read that never came back"
        )
        XCTAssertFalse(app.buttons["asset-detail-sell"].exists, "Sell on an unknown position would dead-end")
        XCTAssertTrue(app.buttons["asset-detail-buy"].isEnabled, "buying never depended on this read")
        attachScreenshot(app, name: "asset-detail-cabals-failed")
    }

    /// A cabal that could not be priced is said out loud. Silence would read as "no
    /// cabal holds this", which is a lie about someone's money.
    @MainActor
    func testACabalThatCouldNotBePricedIsNeverSilent() throws {
        let app = launch("cabalsPartial")
        waitForScreen(app, "cabalsPartial")

        XCTAssertTrue(scrollTo(app, "asset-position-unvalued"), "the card never said a cabal could not be priced")
        attachScreenshot(app, name: "asset-detail-cabals-partial")
    }

    /// The Pyth card's whole argument, on the screen where it matters: after the bell
    /// the equity leg is a closing print and the token leg is still live.
    @MainActor
    func testStockVsTokenShowsBothLegsAndThePremium() throws {
        let app = launch("afterHours")
        waitForScreen(app, "afterHours")

        XCTAssertTrue(scrollTo(app, "asset-detail-stock-vs-token"), "the stock-vs-token card never appeared")
        XCTAssertTrue(anyElement(app, "asset-stock-vs-token-leg-aapl").exists, "no equity leg")
        XCTAssertTrue(anyElement(app, "asset-stock-vs-token-leg-aaplx").exists, "no token leg")
        XCTAssertTrue(anyElement(app, "asset-stock-vs-token-premium").exists, "no premium pill")
        attachScreenshot(app, name: "asset-detail-stock-vs-token-after-hours")
    }

    /// A leg with no price must not draw a premium: a pill against a missing leg is
    /// a number nobody measured.
    @MainActor
    func testAnUnavailableFeedDrawsNoPremium() throws {
        let app = launch("notEntitled")
        waitForScreen(app, "notEntitled")

        XCTAssertTrue(scrollTo(app, "asset-detail-stock-vs-token"), "the card should still draw on one leg")
        XCTAssertFalse(
            anyElement(app, "asset-stock-vs-token-premium").exists,
            "a premium was drawn against a leg with no price"
        )
        attachScreenshot(app, name: "asset-detail-stock-vs-token-not-entitled")
    }

    /// The disclosure is never inside the clamped paragraph, so "Show more" being
    /// collapsed cannot hide it.
    @MainActor
    func testTheTrackerDisclosureIsVisibleWithoutExpandingTheAbout() throws {
        let app = launch("open")
        waitForScreen(app, "open")

        XCTAssertTrue(scrollTo(app, "asset-detail-about"), "the About card never appeared")
        XCTAssertTrue(anyElement(app, "asset-about-disclosure").exists, "the tracker disclosure is not on screen")
        XCTAssertTrue(anyElement(app, "asset-about-toggle").exists, "the body is not clamped with a Show more")
    }

    /// Two columns of money truncate at the accessibility text sizes, so the grid
    /// falls back to one column. The card has to stay reachable through that switch —
    /// the earlier version of this screen put a fixed height on a wrapping view and
    /// the content overflowed rather than clipping.
    ///
    /// The cells themselves are asserted at the default text size, in
    /// `testStatsGridCarriesTheCellsItCouldSource`. A `LazyVGrid` only builds the rows
    /// near the viewport, and at these text sizes one card fills more than a screen,
    /// so "which cells exist right now" is a fact about scroll position rather than
    /// about the grid. The screenshot is what carries the layout claim here.
    @MainActor
    func testStatsGridSurvivesAnAccessibilityTextSize() throws {
        let app = launch("open", textSize: "UICTContentSizeCategoryAccessibilityL")
        waitForScreen(app, "stats at accessibility text size")

        XCTAssertTrue(scrollTo(app, "asset-detail-stats", attempts: 12), "the stats grid never appeared")
        attachScreenshot(app, name: "asset-detail-stats-accessibility-text")
    }

    /// The honest-grid rule: a cell we could source is there, and a cell we could not
    /// is absent rather than a dash. `statsComplete` has every figure; `statsPartial`
    /// (the `sparse` scenario) has no year of history and no Pyth interval.
    @MainActor
    func testStatsGridCarriesTheCellsItCouldSource() throws {
        let full = launch("open")
        waitForScreen(full, "open")
        XCTAssertTrue(scrollTo(full, "asset-detail-stats"), "the stats grid never appeared")
        for cell in ["asset-stat-open", "asset-stat-prev-close", "asset-stat-certainty"] {
            XCTAssertTrue(
                scrollTo(full, cell),
                "\(cell) is missing; on screen: \(visibleStatIdentifiers(full))"
            )
        }
        attachScreenshot(full, name: "asset-detail-stats-complete")
        full.terminate()

        let partial = launch("sparse")
        waitForScreen(partial, "sparse")
        XCTAssertTrue(scrollTo(partial, "asset-detail-stats"), "the partial stats grid never appeared")
        XCTAssertTrue(scrollTo(partial, "asset-stat-open"), "a sourced cell is missing")
        XCTAssertFalse(
            partial.descendants(matching: .any).matching(identifier: "asset-stat-52w-high").firstMatch.exists,
            "a 52-week cell was drawn for a symbol with no year of history"
        )
        attachScreenshot(partial, name: "asset-detail-stats-partial")
    }
}
