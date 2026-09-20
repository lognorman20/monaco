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
        XCTAssertTrue(
            app.staticTexts["Selling your slice…"].exists,
            "the cover should say the slice is being sold, not just spin"
        )
    }

    /// The point of the cover: no second tap on a money action while the first one is still running.
    @MainActor
    func testLeavingBlocksTheActionRow() {
        let app = launchApp(scenario: "sellAndLeave")
        XCTAssertTrue(anyElement(app, "group-leaving-cover").waitForExistence(timeout: 20))

        for action in ["group-action-fund", "group-action-propose", "group-action-sell", "group-action-chat"] {
            let button = anyElement(app, action)
            XCTAssertFalse(button.isHittable, "\(action) should not be tappable while leaving")
        }
        XCTAssertFalse(
            anyElement(app, "group-details-button").isHittable,
            "the details sheet should not open while leaving"
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
    }
}
