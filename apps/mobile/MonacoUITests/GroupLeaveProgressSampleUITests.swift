//
//  GroupLeaveProgressSampleUITests.swift
//  MonacoUITests
//
//  "Sell and leave" sells the member's slice and waits for the payout to confirm, which the
//  backend does inside the request — tens of seconds on a slow swap. Before the fix nothing on
//  the cabal screen said so: the details sheet that carried the only "Leaving…" label had already
//  closed before the confirmation dialog appeared, so the member confirmed the most consequential
//  action on the screen and then watched a fully live screen do nothing, with Add money, Propose
//  and Cash out all still tappable on a cabal they were mid-exit from.
//
//  The harness behind `-MonacoGroupDetailSample sellAndLeave` puts the real cover up over the
//  real cabal screen, through the same `groupLeaveProgress` modifier the product uses.
//

import XCTest

final class GroupLeaveProgressSampleUITests: XCTestCase {

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    @MainActor
    private func launchApp(scenario: String) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-MonacoGroupDetailSample", scenario]
        app.launch()
        return app
    }

    private func anyElement(_ app: XCUIApplication, _ identifier: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: identifier).firstMatch
    }

    @MainActor
    func testLeavingCoversTheCabalScreenAndSaysWhatIsHappening() {
        let app = launchApp(scenario: "sellAndLeave")

        let cover = anyElement(app, "group-leaving-cover")
        XCTAssertTrue(cover.waitForExistence(timeout: 20), "the leave cover should be on screen")
        // The cover combines its children into one element, so the two Texts are not separate
        // `staticTexts` to query — the label of the combined element is what VoiceOver reads.
        XCTAssertTrue(
            cover.label.contains("Selling your slice"),
            "the cover should say the slice is being sold, not just spin — label was \(cover.label)"
        )
    }

    /// The point of the cover: no second tap on a money action while the first one is still running.
    @MainActor
    func testLeavingBlocksTheActionRow() {
        let app = launchApp(scenario: "sellAndLeave")
        XCTAssertTrue(anyElement(app, "group-leaving-cover").waitForExistence(timeout: 20))

        for action in ["group-action-fund", "group-action-propose", "group-action-sell", "group-action-chat"] {
            let button = anyElement(app, action)
            // Without this the `isHittable` check below passes on a query that matches
            // nothing, which is how it would rot into saying nothing at all.
            XCTAssertTrue(button.exists, "\(action) should still be on screen while leaving")
            XCTAssertFalse(button.isHittable, "\(action) should not be tappable while leaving")
        }
        // The product removes the toolbar item rather than disabling it, so absence is the
        // assertion. `isHittable` on a missing element is false for the wrong reason.
        XCTAssertFalse(
            anyElement(app, "group-details-button").exists,
            "the details item should be gone while leaving, not merely untappable"
        )
    }

    /// The same screen without a leave in flight: the cover is absent and the actions work.
    @MainActor
    func testTheCoverIsOnlyThereWhileLeaving() {
        let app = launchApp(scenario: "populated")

        let fund = anyElement(app, "group-action-fund")
        XCTAssertTrue(fund.waitForExistence(timeout: 20), "the action row should be on screen")
        XCTAssertTrue(fund.isHittable, "Add money should be tappable on a cabal nobody is leaving")
        XCTAssertFalse(anyElement(app, "group-leaving-cover").exists, "no leave is running")
        // The positive half of the leaving-screen assertion: the details item really does carry
        // this identifier when it is on screen. Without this, "it is gone while leaving" could
        // go on passing against a query that never matches anything.
        let details = anyElement(app, "group-details-button")
        XCTAssertTrue(details.exists, "the details item should be on screen when no leave is running")
        XCTAssertTrue(details.isHittable, "the details item should open the sheet when no leave is running")
    }
}
