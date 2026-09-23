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

    /// #341: the rows are the way into the cabal and into the vote. They were built
    /// with no `openCabal` and no `openProposal`, so every one of them fell into its
    /// non-Button branch and the card was a picture of a position rather than a way
    /// into one.
    ///
    /// The pushed screens read from the real API, which the harness does not run —
    /// what is asserted here is the push itself: a back button in the bar, which the
    /// stock screen alone does not have.
    @MainActor
    func testHoldingRowsAndVoteRowsActuallyGoSomewhere() throws {
        for row in ["asset-position-holding-g-weekend", "asset-position-vote-p-buy"] {
            let app = launch("cabals")
            waitForScreen(app, "cabals")
            XCTAssertTrue(scrollTo(app, "asset-detail-position"), "the position card never appeared")

            let target = anyElement(app, row)
            XCTAssertTrue(target.waitForExistence(timeout: 10), "\(row) is not in the tree")
            XCTAssertTrue(
                bringIntoOpenView(app, target),
                "\(row) never came out from under the trade bar: row \(target.frame), nav \(app.navigationBars.firstMatch.frame), buy \(app.buttons["asset-detail-buy"].frame)"
            )
            target.tap()

            // A pushed screen puts a back button in the bar; the stock screen alone has none.
            let back = app.navigationBars.buttons.firstMatch
            XCTAssertTrue(back.waitForExistence(timeout: 10), "\(row) did not push anything")
            attachScreenshot(app, name: "asset-detail-opened-from-\(row)")
            app.terminate()
        }
    }

    /// A card's rows are built whether or not they are on screen, so "exists" says
    /// nothing about where a tap lands. The trade bar is a material pinned over the
    /// bottom of the scroll view: a tap on a row still under it lands on the bar.
    /// Nudge the content until the row sits between the navigation bar and the
    /// trade bar, and only then tap.
    ///
    /// The drag runs in the page's gutter, never across the middle of the screen,
    /// where the chart sits.
    @MainActor
    private func bringIntoOpenView(_ app: XCUIApplication, _ element: XCUIElement) -> Bool {
        let origin = app.coordinate(withNormalizedOffset: .zero)
        for _ in 0..<12 {
            let top = app.navigationBars.firstMatch.frame.maxY
            let bottom = app.buttons["asset-detail-buy"].frame.minY - 24
            let frame = element.frame
            // At the accessibility sizes the trade bar takes half the screen and a
            // stacked row can be taller than what is left, so a row counts as open
            // once as much of it as fits is showing, from its top down.
            let visibleHeight = min(frame.height, bottom - top)
            if frame.minY >= top, frame.minY + visibleHeight <= bottom { return true }
            // Aim the row's top at the top of the open area, plus a little air.
            let target = top + 8
            let distance = min(abs(frame.minY - target), bottom - top - 16)
            let scrollUp = frame.minY > target
            let startY = scrollUp ? bottom - 8 : top + 8
            // In the page's own 16pt gutter, right of every card, so the drag is
            // never read as a scrub of the curve. The right gutter, because the left
            // edge belongs to the system's back-swipe.
            let gutterX = app.windows.firstMatch.frame.width - 6
            let start = origin.withOffset(CGVector(dx: gutterX, dy: startY))
            let end = start.withOffset(CGVector(dx: 0, dy: scrollUp ? -distance : distance))
            start.press(forDuration: 0.05, thenDragTo: end)
        }
        return false
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

    /// The card's whole argument, on the screen where it matters: after the bell the
    /// share's print is a closing price while the token and its mark keep moving, so
    /// the premium between the two per-token lines still stands.
    @MainActor
    func testStockVsTokenShowsAllThreeLinesAndThePremium() throws {
        let app = launch("afterHours")
        waitForScreen(app, "afterHours")

        XCTAssertTrue(scrollTo(app, "asset-detail-stock-vs-token"), "the stock-vs-token card never appeared")
        // Keyed on each line's role: the token and its mark share a ticker, so an
        // identifier built from the ticker would find whichever drew first.
        XCTAssertTrue(anyElement(app, "asset-stock-vs-token-leg-token").exists, "no Kyber quote line")
        XCTAssertTrue(anyElement(app, "asset-stock-vs-token-leg-mark").exists, "no Chainlink mark line")
        XCTAssertTrue(anyElement(app, "asset-stock-vs-token-leg-equity").exists, "no equity reference line")
        XCTAssertTrue(anyElement(app, "asset-stock-vs-token-premium").exists, "no premium pill")
        attachScreenshot(app, name: "asset-detail-stock-vs-token-after-hours")
    }

    /// The equity line is a reference, not the basis. Losing it costs the card its
    /// reference and nothing else: the premium is between the token and its mark, and
    /// both of those are still priced.
    @MainActor
    func testAnUnavailableEquityFeedKeepsThePremium() throws {
        let app = launch("notEntitled")
        waitForScreen(app, "notEntitled")

        XCTAssertTrue(scrollTo(app, "asset-detail-stock-vs-token"), "the card should still draw without the reference")
        XCTAssertTrue(
            anyElement(app, "asset-stock-vs-token-premium").exists,
            "the premium is the token against its mark and does not depend on the equity feed"
        )
        attachScreenshot(app, name: "asset-detail-stock-vs-token-not-entitled")
    }

    /// Over a weekend the mark holds Friday's close. The gap to the pools is the
    /// market's move since then, not a premium, and no pill may claim otherwise.
    @MainActor
    func testAMarkHoldingItsLastCloseDrawsNoPremium() throws {
        let app = launch("weekend")
        waitForScreen(app, "weekend")

        XCTAssertTrue(scrollTo(app, "asset-detail-stock-vs-token"), "the card never appeared")
        XCTAssertFalse(
            anyElement(app, "asset-stock-vs-token-premium").exists,
            "a premium was drawn against a mark that is holding its last close"
        )
        attachScreenshot(app, name: "asset-detail-stock-vs-token-weekend")
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

    /// The position card was the only one below the chart that kept its side-by-side
    /// rows at the accessibility sizes: a cabal name truncated and the money beside it
    /// scaled itself down, which is the one thing on this screen that must never be
    /// cut short. It stacks now, like the stats grid and the Pyth legs. What is
    /// asserted is that the card and its rows survive the switch, the same claim
    /// `testStatsGridSurvivesAnAccessibilityTextSize` makes. Like that test, the
    /// screenshot shows the top of the page: at this size synthesized drags do not
    /// move it in the harness, which is its own open question.
    @MainActor
    func testPositionCardSurvivesAnAccessibilityTextSize() throws {
        let app = launch("cabals", textSize: "UICTContentSizeCategoryAccessibilityL")
        waitForScreen(app, "position card at accessibility text size")

        XCTAssertTrue(scrollTo(app, "asset-detail-position", attempts: 12), "the position card never appeared")
        XCTAssertTrue(
            anyElement(app, "asset-position-holding-g-weekend").waitForExistence(timeout: 10),
            "a holding row is missing at the accessibility text sizes"
        )
        attachScreenshot(app, name: "asset-detail-position-accessibility-text")
    }

    /// The badge measures the cabals' return and the big figure is the member's own
    /// slice; they must not read as one pair. The screenshot shows the labelled line.
    @MainActor
    func testTheCabalsReturnIsLabelledAsTheirs() throws {
        let app = launch("cabals")
        waitForScreen(app, "cabals")
        let totals = anyElement(app, "asset-position-totals")
        XCTAssertTrue(totals.waitForExistence(timeout: 10), "the totals never drew")
        XCTAssertTrue(bringIntoOpenView(app, totals), "the totals never came out from under the trade bar")
        XCTAssertTrue(
            totals.label.contains("Your cabals' return"),
            "the P&L reached VoiceOver without saying whose it is: \(totals.label)"
        )
        attachScreenshot(app, name: "asset-detail-position-labelled-return")
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

    /// The spread is the token's and every cell in this grid is the share's. It
    /// belongs on the stock-vs-token card, beside the two prices it is measured
    /// between; a cell here would read as the share's own.
    @MainActor
    func testTheSpreadIsOnTheCardAndNotInTheStatsGrid() throws {
        let app = launch("open")
        waitForScreen(app, "open")

        XCTAssertTrue(scrollTo(app, "asset-detail-stats"), "the stats grid never appeared")
        XCTAssertFalse(
            app.descendants(matching: .any).matching(identifier: "asset-stat-spread").firstMatch.exists,
            "the token's spread was drawn among the share's own figures: \(visibleStatIdentifiers(app))"
        )
        XCTAssertTrue(scrollTo(app, "asset-stock-vs-token-spread"), "the spread is not on the card either")
        attachScreenshot(app, name: "asset-detail-spread-on-the-card")
    }

    /// The disclosure is the sentence a member is most likely to read on its own, so
    /// it says only what is true of a B20 token: no ownership and no vote. It must
    /// never claim there is no dividend — a cash dividend is reinvested into the
    /// stock behind the token and raises what one is worth, which is exactly what the
    /// total-return mark this screen is priced from carries.
    @MainActor
    func testTheDisclosureDoesNotClaimThereIsNoDividend() throws {
        let app = launch("open")
        waitForScreen(app, "open")

        XCTAssertTrue(scrollTo(app, "asset-detail-about"), "the About card never appeared")
        let disclosure = anyElement(app, "asset-about-disclosure")
        XCTAssertTrue(disclosure.waitForExistence(timeout: 10), "the tracker disclosure is not on screen")
        let text = disclosure.label.lowercased()
        XCTAssertFalse(text.contains("no dividend"), "the disclosure claims something false: \(disclosure.label)")
        XCTAssertTrue(text.contains("no ownership"), disclosure.label)
        XCTAssertTrue(text.contains("no vote"), disclosure.label)

        // And nothing anywhere on the About card puts this token on the chain it was
        // ported off.
        let chain = anyElement(app, "asset-about-fact-chain")
        XCTAssertTrue(chain.waitForExistence(timeout: 10), "the chain fact is missing")
        XCTAssertTrue(chain.label.contains("Base"), "the chain fact says \(chain.label)")
        attachScreenshot(app, name: "asset-detail-about-disclosure")
    }
}
