//
//  GroupNavSampleUITests.swift
//  MonacoUITests
//
//  Regression coverage for the cabal action row (Add money / Cash out / Chat).
//  Those actions push their screens from `GroupDetailView`, and the push has to work
//  no matter how the cabal screen itself was reached. The debug harness behind
//  `-MonacoGroupNavSample <entry>` reaches the *real* `GroupDetailView` through each
//  entry chain the product uses, on sample data and with no backend.
//
//  Before the fix, `root` passed and `list` / `create` hung: `GroupDetailView` read
//  `@Environment(\.dismiss)` in the same body that declares its `navigationDestination`,
//  so a pushed cabal screen invalidated itself every time it pushed one of these
//  screens — an update loop that pegged the main thread and swallowed the tap.
//

import XCTest

final class GroupNavSampleUITests: XCTestCase {

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    @MainActor
    private func launchApp(entry: String) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-MonacoGroupNavSample", entry]
        app.launch()
        return app
    }

    private func anyElement(_ app: XCUIApplication, _ identifier: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: identifier).firstMatch
    }

    @MainActor
    private func attachScreenshot(_ app: XCUIApplication, name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    /// Walks the given entry chain until the cabal screen's action row is on screen.
    @MainActor
    private func openCabalScreen(entry: String) -> XCUIApplication {
        let app = launchApp(entry: entry)

        switch entry {
        case "list":
            let row = anyElement(app, "nav-sample-list-row")
            XCTAssertTrue(row.waitForExistence(timeout: 15), "[\(entry)] the cabal row should exist")
            row.tap()
        case "create":
            let start = anyElement(app, "nav-sample-start-create")
            XCTAssertTrue(start.waitForExistence(timeout: 15), "[\(entry)] the create entry should exist")
            start.tap()
            let submit = anyElement(app, "nav-sample-create-submit")
            XCTAssertTrue(submit.waitForExistence(timeout: 15), "[\(entry)] the create form should be pushed")
            submit.tap()
        default:
            break
        }

        let fund = anyElement(app, "group-action-fund")
        XCTAssertTrue(fund.waitForExistence(timeout: 20), "[\(entry)] the cabal action row should be on screen")
        return app
    }

    /// Taps an action on the cabal action row and asserts its screen is pushed.
    @MainActor
    private func assertAction(
        _ app: XCUIApplication,
        entry: String,
        actionIdentifier: String,
        destinationIdentifier: String
    ) {
        let action = anyElement(app, actionIdentifier)
        XCTAssertTrue(action.waitForExistence(timeout: 10), "[\(entry)] \(actionIdentifier) should exist")
        action.tap()

        let destination = anyElement(app, destinationIdentifier)
        let arrived = destination.waitForExistence(timeout: 10)
        if !arrived {
            attachScreenshot(app, name: "\(entry)-\(actionIdentifier)-no-push")
        }
        XCTAssertTrue(
            arrived,
            "[\(entry)] tapping \(actionIdentifier) should push \(destinationIdentifier); nothing happened"
        )

        // Back to the cabal screen so the next action starts from the same place.
        let back = app.navigationBars.buttons.element(boundBy: 0)
        if back.exists {
            back.tap()
        }
        _ = anyElement(app, "group-action-fund").waitForExistence(timeout: 10)
    }

    @MainActor
    private func runActionRow(entry: String) {
        let app = openCabalScreen(entry: entry)
        assertAction(app, entry: entry, actionIdentifier: "group-action-fund", destinationIdentifier: "fund-cabal-view")
        assertAction(app, entry: entry, actionIdentifier: "group-action-sell", destinationIdentifier: "sell-cabal-amount-display")
        assertAction(app, entry: entry, actionIdentifier: "group-action-chat", destinationIdentifier: "group-chat-view")
        attachScreenshot(app, name: "\(entry)-action-row-ok")
    }

    /// Baseline: the cabal screen at the root of the stack. This always worked.
    @MainActor
    func testActionRowFromStackRoot() throws {
        runActionRow(entry: "root")
    }

    /// Home / Cabals list row → cabal screen.
    @MainActor
    func testActionRowWhenOpenedFromAList() throws {
        runActionRow(entry: "list")
    }

    /// Cabals tab → New cabal → Create → cabal screen.
    @MainActor
    func testActionRowWhenOpenedFromCreate() throws {
        runActionRow(entry: "create")
    }
}
