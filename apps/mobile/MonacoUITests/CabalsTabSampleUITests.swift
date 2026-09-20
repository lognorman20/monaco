//
//  CabalsTabSampleUITests.swift
//  MonacoUITests
//
//  QA coverage for the Cabals tab against the debug sample data harness
//  (launch argument -MonacoCabalsTabSample). No sign-in and no backend are
//  needed: the app boots straight into the Cabals tab with fixed sample
//  cabals so this suite (and the screenshots it attaches) stay reproducible.
//

import XCTest

final class CabalsTabSampleUITests: XCTestCase {

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    @MainActor
    private func launchApp(scenario: String? = nil) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-MonacoCabalsTabSample"]
        if let scenario {
            app.launchArguments += ["-MonacoCabalsTabSampleScenario", scenario]
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

    /// Looks up an accessibility identifier regardless of the underlying
    /// element type (VStack/HStack/NavigationLink-backed rows don't always
    /// surface as the same XCUIElementType), mirroring the
    /// `identifier BEGINSWITH` pattern already used for cabal rows elsewhere
    /// in this UI test target.
    private func anyElement(_ app: XCUIApplication, _ identifier: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: identifier).firstMatch
    }

    /// Brings `element` somewhere it can actually be tapped, and reports whether
    /// it got there. A `Form` is lazy: a row below the fold is not in the
    /// accessibility tree at all, so existence has to be re-checked after each
    /// scroll rather than waited on up front. The keyboard counts as fold.
    @MainActor
    @discardableResult
    private func scrollUntilHittable(_ app: XCUIApplication, _ element: XCUIElement, attempts: Int = 8) -> Bool {
        for _ in 0..<attempts {
            if element.exists && element.isHittable { return true }
            app.swipeUp()
        }
        return element.exists && element.isHittable
    }

    /// The "New cabal" card sits after every joined cabal in a horizontal strip,
    /// so on a phone it always starts off-screen.
    @MainActor
    private func newCabalStripCard(_ app: XCUIApplication) -> XCUIElement {
        let strip = anyElement(app, "cabals-strip")
        XCTAssertTrue(strip.waitForExistence(timeout: 10), "cabals strip should exist")
        let newCard = anyElement(app, "cabals-strip-new")
        var tries = 0
        while !newCard.isHittable && tries < 6 {
            strip.swipeLeft()
            tries += 1
        }
        return newCard
    }

    /// The search field is `cabals-search-field` on its container HStack; the
    /// actual text field inside carries `monaco-search-field`. Try several
    /// ways to find it so the test tolerates either identifier resolving.
    @MainActor
    private func searchField(_ app: XCUIApplication) -> XCUIElement {
        let outerTextField = app.textFields["cabals-search-field"]
        if outerTextField.waitForExistence(timeout: 2) {
            return outerTextField
        }
        let innerTextField = app.textFields["monaco-search-field"]
        if innerTextField.waitForExistence(timeout: 2) {
            return innerTextField
        }
        let outerAny = anyElement(app, "cabals-search-field")
        if outerAny.waitForExistence(timeout: 2) {
            return outerAny
        }
        return app.textFields.firstMatch
    }

    // MARK: - (a) Overview

    @MainActor
    func testOverviewShowsChartStripAndLeaderboard() throws {
        let app = launchApp()

        let root = anyElement(app, "cabals-root")
        XCTAssertTrue(root.waitForExistence(timeout: 10), "cabals-root should exist after launch")

        let pnlSection = anyElement(app, "cabals-pnl-section")
        XCTAssertTrue(pnlSection.waitForExistence(timeout: 10), "P&L section should exist")
        XCTAssertTrue(anyElement(app, "cabals-pnl-chart").waitForExistence(timeout: 10), "P&L chart should render")
        XCTAssertTrue(anyElement(app, "cabals-pnl-range-1M").exists, "range chips should exist")

        // Three joined cabals have P&L history in the sample data.
        for index in 1...3 {
            let groupId = String(format: "5b1f0c9e-%04d-4c55-9a51-%012d", index, index)
            XCTAssertTrue(
                anyElement(app, "cabals-pnl-legend-\(groupId)").waitForExistence(timeout: 5),
                "missing legend row for \(groupId)"
            )
        }

        let strip = anyElement(app, "cabals-strip")
        XCTAssertTrue(strip.waitForExistence(timeout: 5), "cabals strip should exist")
        XCTAssertTrue(
            anyElement(app, "cabals-strip-card-5b1f0c9e-0001-4c55-9a51-000000000001").waitForExistence(timeout: 5),
            "Weekend investors strip card should exist"
        )

        let leaderboard = anyElement(app, "cabals-leaderboard")
        XCTAssertTrue(leaderboard.waitForExistence(timeout: 5), "leaderboard should exist")

        attachScreenshot(app, name: "01-overview")
    }

    // MARK: - (b) Leaderboard, scrolled

    @MainActor
    func testLeaderboardShowsRankedRows() throws {
        let app = launchApp()

        let leaderboard = anyElement(app, "cabals-leaderboard")
        XCTAssertTrue(leaderboard.waitForExistence(timeout: 10), "leaderboard should exist")

        app.swipeUp()
        app.swipeUp()

        XCTAssertTrue(leaderboard.waitForExistence(timeout: 5), "leaderboard should still exist after scrolling")

        // All six sample cabals carry a percent return, so all six are ranked.
        let groupIds = (1...6).map { String(format: "5b1f0c9e-%04d-4c55-9a51-%012d", $0, $0) }
        var visibleRows = 0
        for groupId in groupIds where anyElement(app, "cabals-leaderboard-row-\(groupId)").exists {
            visibleRows += 1
        }
        XCTAssertGreaterThan(visibleRows, 0, "expected at least one ranked leaderboard row visible after scrolling")

        attachScreenshot(app, name: "02-leaderboard")
    }

    // MARK: - (c) Search results

    @MainActor
    func testSearchWeekShowsTwoResults() throws {
        let app = launchApp()

        let field = searchField(app)
        XCTAssertTrue(field.waitForExistence(timeout: 10), "search field should exist")
        field.tap()
        field.typeText("week")

        // Debounce is 300ms; give it real headroom.
        let results = anyElement(app, "cabals-search-results")
        XCTAssertTrue(results.waitForExistence(timeout: 5), "search results container should appear")

        let weekendInvestors = anyElement(app, "cabals-search-result-5b1f0c9e-0001-4c55-9a51-000000000001")
        let weekendWarriors = anyElement(app, "cabals-search-result-5b1f0c9e-0006-4c55-9a51-000000000006")
        XCTAssertTrue(weekendInvestors.waitForExistence(timeout: 5), "Weekend investors should be a result")
        XCTAssertTrue(weekendWarriors.waitForExistence(timeout: 5), "Weekend warriors should be a result")

        attachScreenshot(app, name: "03-search-results")
    }

    // MARK: - (d) Search empty state

    @MainActor
    func testSearchZzzzShowsEmptyState() throws {
        let app = launchApp()

        let field = searchField(app)
        XCTAssertTrue(field.waitForExistence(timeout: 10), "search field should exist")
        field.tap()
        field.typeText("zzzz")

        let empty = anyElement(app, "cabals-search-empty")
        XCTAssertTrue(empty.waitForExistence(timeout: 5), "empty search state should appear")

        attachScreenshot(app, name: "04-search-empty")
    }

    // MARK: - (e) Search too-short hint

    @MainActor
    func testSearchSingleCharacterShowsTooShortHint() throws {
        let app = launchApp()

        let field = searchField(app)
        XCTAssertTrue(field.waitForExistence(timeout: 10), "search field should exist")
        field.tap()
        field.typeText("w")

        let tooShort = anyElement(app, "cabals-search-too-short")
        XCTAssertTrue(tooShort.waitForExistence(timeout: 5), "too-short hint should appear")
    }

    // MARK: - (f) Approval join screen

    @MainActor
    func testTeslaOrBustOpensAskToJoinScreen() throws {
        let app = launchApp()

        let field = searchField(app)
        XCTAssertTrue(field.waitForExistence(timeout: 10), "search field should exist")
        field.tap()
        field.typeText("tesla")

        let teslaResult = anyElement(app, "cabals-search-result-5b1f0c9e-0005-4c55-9a51-000000000005")
        XCTAssertTrue(teslaResult.waitForExistence(timeout: 5), "Tesla or bust should be a search result")
        teslaResult.tap()

        let joinName = anyElement(app, "join-group-name")
        XCTAssertTrue(joinName.waitForExistence(timeout: 8), "join screen should show the group name")
        XCTAssertEqual(joinName.label, "Tesla or bust")

        let askToJoin = app.buttons["Ask to join"]
        XCTAssertTrue(askToJoin.waitForExistence(timeout: 5), "button should be titled 'Ask to join' for approval cabals")

        // Do NOT tap join-group-submit: there is no backend behind this sample harness.
        attachScreenshot(app, name: "05-join-approval")
    }

    // MARK: - (g) A thin range keeps the chart section and its picker (#294)

    @MainActor
    func testPickingADayWithThinHistoryKeepsTheRangePicker() throws {
        let app = launchApp()

        let pnlSection = anyElement(app, "cabals-pnl-section")
        XCTAssertTrue(pnlSection.waitForExistence(timeout: 10), "P&L section should exist on the default range")

        // The sample cabals have no points inside a single day, so 1D is the
        // sparse range. Before this fix, tapping it deleted the whole section —
        // picker included — with no way back.
        let oneDay = anyElement(app, "cabals-pnl-range-1D")
        XCTAssertTrue(oneDay.waitForExistence(timeout: 5), "1D chip should exist")
        oneDay.tap()

        XCTAssertTrue(
            anyElement(app, "cabals-pnl-sparse").waitForExistence(timeout: 5),
            "a range with thin history should say so inside the card"
        )
        XCTAssertTrue(pnlSection.exists, "the P&L section should survive a sparse range")

        let oneMonth = anyElement(app, "cabals-pnl-range-1M")
        XCTAssertTrue(oneMonth.exists, "the range picker should still be on screen")
        oneMonth.tap()
        XCTAssertTrue(
            anyElement(app, "cabals-pnl-chart").waitForExistence(timeout: 5),
            "switching back to 1M should draw the chart again"
        )

        attachScreenshot(app, name: "06-pnl-sparse-range")
    }

    // MARK: - (h) Cabals that have not loaded are not "no cabals" (#295)

    @MainActor
    func testUnloadedCabalsDoNotClaimTheMemberHasNone() throws {
        let app = launchApp(scenario: "cabalsUnavailable")

        let root = anyElement(app, "cabals-root")
        XCTAssertTrue(root.waitForExistence(timeout: 10), "cabals-root should exist after launch")

        // The read finished without a list and nothing else is in flight: the
        // failed branch. The strip must offer a retry, not tell a funded member
        // they have no cabals and nudge them to create one.
        XCTAssertTrue(
            anyElement(app, "cabals-strip-error").waitForExistence(timeout: 10),
            "an unloaded cabals list should offer a retry"
        )
        XCTAssertFalse(
            anyElement(app, "cabals-strip-empty").exists,
            "an unloaded cabals list must not claim 'No cabals yet'"
        )

        attachScreenshot(app, name: "07-cabals-unavailable")
    }

    // MARK: - (i) Board rank reaches VoiceOver (#332)

    @MainActor
    func testLeaderboardRowAnnouncesItsRank() throws {
        let app = launchApp()

        XCTAssertTrue(anyElement(app, "cabals-leaderboard").waitForExistence(timeout: 10), "leaderboard should exist")
        app.swipeUp()
        app.swipeUp()

        // Dorm 4B fund is +32%, the top of the sample board.
        let topRow = anyElement(app, "cabals-leaderboard-row-5b1f0c9e-0004-4c55-9a51-000000000004")
        XCTAssertTrue(topRow.waitForExistence(timeout: 10), "the top board row should exist")
        XCTAssertTrue(
            topRow.label.contains("Rank 1"),
            "the rank is the point of this board; it should be in the row's label, got: \(topRow.label)"
        )
    }

    // MARK: - (j) A list still loading is not a list that failed (#295)

    @MainActor
    func testACabalsListStillLoadingShowsPlaceholdersNotAnError() throws {
        let app = launchApp(scenario: "cabalsLoading")

        let root = anyElement(app, "cabals-root")
        XCTAssertTrue(root.waitForExistence(timeout: 10), "cabals-root should exist after launch")

        // The shell's first load is still running, so the strip has nothing to
        // say yet. It must not claim a failure before an attempt has finished.
        XCTAssertTrue(
            anyElement(app, "cabals-strip-loading").waitForExistence(timeout: 10),
            "a cabals list that has not arrived yet should show placeholder cards"
        )
        XCTAssertFalse(anyElement(app, "cabals-strip-error").exists, "nothing has failed yet")
        XCTAssertFalse(anyElement(app, "cabals-strip-empty").exists, "the list is unknown, not empty")

        attachScreenshot(app, name: "08-cabals-loading")
    }

    // MARK: - (k) Creating replaces the form with the new cabal (#292, #317)

    @MainActor
    func testCreatingACabalReplacesTheFormWithTheNewCabal() throws {
        let app = launchApp()

        let newCard = newCabalStripCard(app)
        XCTAssertTrue(newCard.isHittable, "the New cabal strip card should be reachable")
        newCard.tap()

        let nameField = app.textFields["create-group-name"]
        XCTAssertTrue(nameField.waitForExistence(timeout: 8), "the New cabal form should be pushed")
        nameField.tap()
        // The trailing newline resigns the keyboard so the button below is free.
        nameField.typeText("Lunch money\n")

        // The submit button sits below four sections of inline pickers.
        let submit = app.buttons["create-group-submit"]
        XCTAssertTrue(
            scrollUntilHittable(app, submit),
            "Create cabal button should be reachable on the New cabal form"
        )
        submit.tap()

        // The whole point of #292: the form is *replaced*, not covered. A second
        // tap on a form still sitting underneath is a second cabal and a second
        // treasury.
        XCTAssertTrue(
            app.navigationBars["Lunch money"].waitForExistence(timeout: 10),
            "the new cabal should be the top screen"
        )
        XCTAssertFalse(
            app.textFields["create-group-name"].exists,
            "the New cabal form must be gone, not underneath the cabal"
        )

        attachScreenshot(app, name: "09-created-cabal")

        // Back lands on the tab, not on a filled-in form.
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(
            anyElement(app, "cabals-root").waitForExistence(timeout: 8),
            "Back from a new cabal should land on the Cabals tab"
        )
        XCTAssertFalse(
            app.textFields["create-group-name"].exists,
            "Back must not land on an armed New cabal form"
        )
    }

    // MARK: - (l) Joining replaces the join form with the cabal (#293)

    @MainActor
    func testJoiningFromSearchReplacesTheJoinFormWithTheCabal() throws {
        let app = launchApp()

        let field = searchField(app)
        XCTAssertTrue(field.waitForExistence(timeout: 10), "search field should exist")
        field.tap()
        field.typeText("dorm")

        // Dorm 4B fund is open and the viewer is not in it, so this is a real join.
        let dormRow = anyElement(app, "cabals-search-result-5b1f0c9e-0004-4c55-9a51-000000000004")
        XCTAssertTrue(dormRow.waitForExistence(timeout: 5), "Dorm 4B fund should be a search result")
        dormRow.tap()

        let joinName = anyElement(app, "join-group-name")
        XCTAssertTrue(joinName.waitForExistence(timeout: 8), "the join form should be pushed")
        XCTAssertEqual(joinName.label, "Dorm 4B fund")

        let submit = app.buttons["join-group-submit"]
        XCTAssertTrue(scrollUntilHittable(app, submit), "Join cabal button should be reachable")
        submit.tap()

        XCTAssertTrue(
            app.navigationBars["Dorm 4B fund"].waitForExistence(timeout: 10),
            "a member who joined should land inside the cabal"
        )
        XCTAssertFalse(
            app.buttons["join-group-submit"].exists,
            "the join form must be gone, not underneath the cabal"
        )

        attachScreenshot(app, name: "10-joined-cabal")

        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(
            anyElement(app, "cabals-root").waitForExistence(timeout: 8),
            "Back from a joined cabal should land on the Cabals tab"
        )
        XCTAssertFalse(
            app.buttons["join-group-submit"].exists,
            "Back must not land on a join form for a cabal the member is already in"
        )

        // And the row the member came from says so, rather than still offering
        // to join a cabal they are in.
        let joinedRow = anyElement(app, "cabals-search-result-5b1f0c9e-0004-4c55-9a51-000000000004")
        XCTAssertTrue(joinedRow.waitForExistence(timeout: 5), "the search row should still be there")
        XCTAssertTrue(
            joinedRow.label.contains("You're in"),
            "the row should read as joined, got: \(joinedRow.label)"
        )
    }

    // MARK: - (m) Strip card pushes detail

    @MainActor
    func testWeekendInvestorsStripCardPushesDetail() throws {
        let app = launchApp()

        let card = anyElement(app, "cabals-strip-card-5b1f0c9e-0001-4c55-9a51-000000000001")
        XCTAssertTrue(card.waitForExistence(timeout: 10), "Weekend investors strip card should exist")
        card.tap()

        // No backend is wired up for the sample harness, so the detail screen
        // will show a load error; only navigation itself is asserted here.
        XCTAssertTrue(
            app.navigationBars.buttons.element(boundBy: 0).waitForExistence(timeout: 8),
            "tapping the strip card should push a detail screen with a back button"
        )
    }
}
