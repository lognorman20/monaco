//
//  MonacoUITests.swift
//  MonacoUITests
//
//  Created by Logan Norman on 9/15/26.
//

import XCTest

private extension XCUIApplication {
    func scrollToElement(_ element: XCUIElement, maxSwipes: Int = 8) {
        var swipes = 0
        while !element.isHittable && swipes < maxSwipes {
            swipeUp()
            swipes += 1
        }
    }
}

final class MonacoUITests: XCTestCase {

    private func authLaunchEnvironment() -> [String: String] {
        let keys = [
            "DYNAMIC_ENVIRONMENT_ID",
            "AUTH_SMS_LOGIN_ENABLED",
            "AUTH_EMAIL_LOGIN_ENABLED",
        ]
        let defaults: [String: String] = [
            "AUTH_SMS_LOGIN_ENABLED": "true",
            "AUTH_EMAIL_LOGIN_ENABLED": "true",
        ]
        let process = ProcessInfo.processInfo.environment
        var env: [String: String] = [:]
        for key in keys {
            let value = process[key] ?? process["SIMCTL_CHILD_\(key)"] ?? defaults[key]
            if let value, !value.isEmpty {
                env[key] = value
            }
        }
        return env
    }

    override func setUpWithError() throws {
        // Put setup code here. This method is called before the invocation of each test method in the class.

        // In UI tests it is usually best to stop immediately when a failure occurs.
        continueAfterFailure = false

        // In UI tests it’s important to set the initial state - such as interface orientation - required for your tests before they run. The setUp method is a good place to do this.
    }

    override func tearDownWithError() throws {
        // Put teardown code here. This method is called after the invocation of each test method in the class.
    }

    @MainActor
    func testM1CreateGroupShowsTreasury() throws {
        let app = XCUIApplication()
        app.launchEnvironment = authLaunchEnvironment()
        app.launch()

        let phoneField = app.textFields["Phone number"]
        XCTAssertTrue(phoneField.waitForExistence(timeout: 15))
        phoneField.tap()
        phoneField.typeText("+15555557177")

        app.buttons["Send code"].tap()

        let codeField = app.textFields["6-digit code"]
        XCTAssertTrue(codeField.waitForExistence(timeout: 20))
        codeField.tap()
        codeField.typeText("465354")

        app.buttons["Verify code"].tap()

        XCTAssertTrue(appLandedInApp(app), "expected Home tab or account after login")

        app.buttons["Create group"].tap()

        let nameField = app.textFields["Group name"]
        XCTAssertTrue(nameField.waitForExistence(timeout: 10))
        nameField.tap()
        nameField.typeText("QA Alpha")

        app.buttons["Create group"].tap()

        XCTAssertTrue(app.staticTexts["Treasury address"].waitForExistence(timeout: 30))
        let attachment = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        attachment.name = "issue14-create-group-treasury"
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    @MainActor
    private func appLandedInApp(_ app: XCUIApplication, timeout: TimeInterval = 30) -> Bool {
        app.tabBars.buttons["Home"].waitForExistence(timeout: timeout)
            || app.staticTexts["Account balance"].waitForExistence(timeout: 2)
            || app.staticTexts["Your account"].waitForExistence(timeout: 2)
            || app.staticTexts["Member wallet"].waitForExistence(timeout: 2)
    }

    @MainActor
    private func loginIfNeeded(_ app: XCUIApplication) {
        if appLandedInApp(app, timeout: 30) {
            return
        }

        let phoneField = app.textFields["Phone number"]
        XCTAssertTrue(phoneField.waitForExistence(timeout: 15))
        phoneField.tap()
        phoneField.typeText("+15555557177")

        app.buttons["Send code"].tap()

        let codeField = app.textFields["6-digit code"]
        XCTAssertTrue(codeField.waitForExistence(timeout: 20))
        codeField.tap()
        codeField.typeText("465354")

        app.buttons["Verify code"].tap()

        XCTAssertTrue(appLandedInApp(app), "expected Home tab or account after login")
    }

    @MainActor
    private func attachScreenshot(_ app: XCUIApplication, name: String) {
        let attachment = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    @MainActor
    func testAssetsTabBrowseSearchDetailAndBuyPicker() throws {
        let app = XCUIApplication()
        app.launchEnvironment = authLaunchEnvironment()
        app.launch()
        loginIfNeeded(app)

        tabButton(app, "Assets").tap()
        XCTAssertTrue(app.staticTexts["Popular"].waitForExistence(timeout: 20), "Assets tab root")

        let popularChip = app.descendants(matching: .any).matching(
            NSPredicate(format: "identifier BEGINSWITH %@", "assets-popular-")
        ).element(boundBy: 0)
        XCTAssertTrue(popularChip.waitForExistence(timeout: 30), "popular strip with prices")

        let search = app.textFields["assets-search-field"].exists
            ? app.textFields["assets-search-field"]
            : app.textFields["monaco-search-field"]
        XCTAssertTrue(search.waitForExistence(timeout: 8), "search field")
        search.tap()
        search.typeText("AAPL")
        let appleRow = app.descendants(matching: .any).matching(
            NSPredicate(format: "identifier BEGINSWITH %@", "assets-row-AAPL")
        ).firstMatch
        XCTAssertTrue(appleRow.waitForExistence(timeout: 25), "AAPL market row")
        appleRow.tap()

        XCTAssertTrue(
            app.buttons["asset-detail-buy"].waitForExistence(timeout: 25)
                || app.staticTexts["Via Kyber"].waitForExistence(timeout: 8)
                || app.otherElements["asset-detail-root"].waitForExistence(timeout: 8)
                || app.staticTexts["No route for this stock right now."].waitForExistence(timeout: 5),
            "asset detail"
        )
        XCTAssertTrue(
            app.otherElements["asset-detail-chart"].waitForExistence(timeout: 12)
                || app.staticTexts["Price history is not available yet."].waitForExistence(timeout: 8)
        )
        XCTAssertTrue(app.otherElements["asset-detail-jupiter"].waitForExistence(timeout: 8)
            || app.staticTexts["Via Kyber"].waitForExistence(timeout: 8))
        attachScreenshot(app, name: "issue-156-asset-detail")

        let buy = app.buttons["asset-detail-buy"]
        XCTAssertTrue(buy.waitForExistence(timeout: 8))
        buy.tap()

        XCTAssertTrue(
            app.navigationBars["Pick a cabal"].waitForExistence(timeout: 10)
                || app.otherElements["group-picker-root"].waitForExistence(timeout: 8)
                || app.staticTexts["Join a cabal first to propose a buy or sell."].waitForExistence(timeout: 8)
        )
        attachScreenshot(app, name: "issue-156-pick-cabal")

        let cabalRow = app.descendants(matching: .any).matching(
            NSPredicate(format: "identifier BEGINSWITH %@", "pick-cabal-")
        ).firstMatch
        if cabalRow.waitForExistence(timeout: 6) {
            cabalRow.tap()
            XCTAssertTrue(
                app.navigationBars["Propose buy"].waitForExistence(timeout: 12)
                    || app.textFields["proposal-search-field"].waitForExistence(timeout: 12)
                    || app.textFields["proposal-amount-field"].waitForExistence(timeout: 8),
                "propose buy with symbol prefilled"
            )
            attachScreenshot(app, name: "issue-156-propose-buy")
            if app.navigationBars.buttons.count > 0 {
                app.navigationBars.buttons.element(boundBy: 0).tap()
            }
        }

        if app.navigationBars.buttons.count > 0 {
            app.navigationBars.buttons.element(boundBy: 0).tap()
        }
        tabButton(app, "Assets").tap()
        let searchField = app.textFields["assets-search-field"].exists
            ? app.textFields["assets-search-field"]
            : app.textFields["monaco-search-field"]
        if searchField.waitForExistence(timeout: 5), let value = searchField.value as? String, !value.isEmpty {
            searchField.tap()
            searchField.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: value.count))
        }
        app.swipeDown()
        XCTAssertTrue(app.staticTexts["Popular"].waitForExistence(timeout: 8))

        tabButton(app, "Home").tap()
        XCTAssertTrue(tabButton(app, "Home").waitForExistence(timeout: 8))
        tabButton(app, "Cabals").tap()
        XCTAssertTrue(app.navigationBars["Cabals"].waitForExistence(timeout: 8))
        attachScreenshot(app, name: "issue-156-cabals-unchanged")
    }

    @MainActor
    func testCreateCabalUsesCanvas() throws {
        let app = XCUIApplication()
        app.launchEnvironment = authLaunchEnvironment()
        app.launch()
        loginIfNeeded(app)

        tabButton(app, "Cabals").tap()
        let menu = app.buttons["cabals-create-join-menu"]
        XCTAssertTrue(menu.waitForExistence(timeout: 8), "create/join menu")
        menu.tap()
        let create = app.buttons["Create cabal"]
        XCTAssertTrue(create.waitForExistence(timeout: 5))
        create.tap()
        XCTAssertTrue(app.navigationBars["Create cabal"].waitForExistence(timeout: 8))
        XCTAssertTrue(app.textFields["create-group-name"].waitForExistence(timeout: 5)
            || app.textFields["Cabal name"].waitForExistence(timeout: 5))
        attachScreenshot(app, name: "create-cabal-canvas")
    }

    @MainActor
    func testFourTabShell() throws {
        let app = XCUIApplication()
        app.launchEnvironment = authLaunchEnvironment()
        app.launch()
        loginIfNeeded(app)

        app.terminate()
        app.launch()
        XCTAssertTrue(tabButton(app, "Home").waitForExistence(timeout: 30), "session restore should land on tabs")

        for name in ["Home", "Profile", "Cabals", "Assets"] {
            let tab = tabButton(app, name)
            XCTAssertTrue(tab.waitForExistence(timeout: 5), "missing tab \(name)")
            tab.tap()
        }
        XCTAssertFalse(app.buttons["tab-settings"].exists, "Settings tab should be removed")

        tabButton(app, "Assets").tap()
        XCTAssertTrue(
            app.staticTexts["Popular"].waitForExistence(timeout: 8)
                || app.otherElements["assets-root"].waitForExistence(timeout: 2),
            "Assets tab should show browse chrome"
        )
        attachScreenshot(app, name: "issue-161-assets")

        tabButton(app, "Cabals").tap()
        XCTAssertTrue(app.navigationBars["Cabals"].waitForExistence(timeout: 8), "Cabals tab root")
        let cabalRow = app.descendants(matching: .any).matching(
            NSPredicate(format: "identifier BEGINSWITH %@", "cabals-row-")
        ).firstMatch
        let emptyCabals = app.staticTexts["No cabals yet. Create or join one to start investing together."]
        if cabalRow.waitForExistence(timeout: 8) {
            cabalRow.tap()
            XCTAssertTrue(
                app.buttons["group-action-fund"].waitForExistence(timeout: 12)
                    || app.navigationBars.element.waitForExistence(timeout: 8),
                "cabal row should push detail or join"
            )
            attachScreenshot(app, name: "issue-161-cabal-detail")
            if app.navigationBars.buttons.count > 0 {
                app.navigationBars.buttons.element(boundBy: 0).tap()
            }
        } else if !emptyCabals.exists, app.cells.count > 0 {
            app.cells.element(boundBy: 0).tap()
            XCTAssertTrue(app.navigationBars.element.waitForExistence(timeout: 8))
            attachScreenshot(app, name: "issue-161-cabal-detail")
            if app.navigationBars.buttons.count > 0 {
                app.navigationBars.buttons.element(boundBy: 0).tap()
            }
        }

        tabButton(app, "Profile").tap()
        attachScreenshot(app, name: "issue-207-profile")
        let signOut = app.buttons["profile-sign-out"].exists ? app.buttons["profile-sign-out"] : app.buttons["Sign out"]
        XCTAssertTrue(signOut.waitForExistence(timeout: 10))
        signOut.tap()
        XCTAssertTrue(app.textFields["Phone number"].waitForExistence(timeout: 20), "sign out should return to login")
        attachScreenshot(app, name: "issue-207-signed-out")
    }

    @MainActor
    private func tabButton(_ app: XCUIApplication, _ name: String) -> XCUIElement {
        let byId = app.tabBars.buttons["tab-\(name.lowercased())"]
        if byId.exists {
            return byId
        }
        return app.tabBars.buttons[name]
    }

    @MainActor
    func testM2DepositLinkReachable() throws {
        let app = XCUIApplication()
        app.launchEnvironment = authLaunchEnvironment()
        app.launch()

        loginIfNeeded(app)

        let homeDeposit = app.buttons["home-deposit-link"]
        XCTAssertTrue(homeDeposit.waitForExistence(timeout: 10))
        homeDeposit.tap()

        XCTAssertTrue(app.navigationBars["Deposit"].waitForExistence(timeout: 10))
        let depositAddressReady = app.otherElements["deposit-address-value"].waitForExistence(timeout: 15)
            || app.otherElements["deposit-address-loading"].waitForExistence(timeout: 5)
        XCTAssertTrue(depositAddressReady)

        app.navigationBars.buttons.element(boundBy: 0).tap()

        app.buttons["Create group"].tap()

        let nameField = app.textFields["Group name"]
        XCTAssertTrue(nameField.waitForExistence(timeout: 10))
        nameField.tap()
        nameField.typeText("M2 Deposit Reach")

        app.buttons["Create group"].tap()

        XCTAssertTrue(app.staticTexts["Treasury address"].waitForExistence(timeout: 30))

        let fundAction = app.buttons["group-action-fund"]
        XCTAssertTrue(fundAction.waitForExistence(timeout: 10))
        app.scrollToElement(fundAction)
        XCTAssertTrue(fundAction.isHittable)
        XCTAssertTrue(app.buttons["group-action-sell"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["deposit-usdc-link"].exists)

        fundAction.tap()

        XCTAssertTrue(app.navigationBars["Fund this cabal"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.textFields["fund-cabal-amount-field"].waitForExistence(timeout: 10))
        attachScreenshot(app, name: "m2-t13-deposit-reachable")
    }

    @MainActor
    func testM2DepositLive() throws {
        let app = XCUIApplication()
        app.launchEnvironment = authLaunchEnvironment()
        app.launch()

        loginIfNeeded(app)
        attachScreenshot(app, name: "m2-t13-xbmcp-01-home")

        let homeDeposit = app.buttons["home-deposit-link"]
        XCTAssertTrue(homeDeposit.waitForExistence(timeout: 10))
        homeDeposit.tap()

        XCTAssertTrue(app.navigationBars["Deposit"].waitForExistence(timeout: 10))
        XCTAssertTrue(
            app.otherElements["deposit-address-value"].waitForExistence(timeout: 30)
                || app.otherElements["deposit-address-loading"].waitForExistence(timeout: 5)
        )
        attachScreenshot(app, name: "m2-t13-xbmcp-02-deposit-address")

        app.navigationBars.buttons.element(boundBy: 0).tap()

        app.buttons["Create group"].tap()

        let nameField = app.textFields["Group name"]
        XCTAssertTrue(nameField.waitForExistence(timeout: 10))
        nameField.tap()
        nameField.typeText("M2 Deposit QA")

        app.buttons["Create group"].tap()

        XCTAssertTrue(app.staticTexts["Treasury address"].waitForExistence(timeout: 30))
        attachScreenshot(app, name: "m2-t13-xbmcp-03-treasury")

        let fundAction = app.buttons["group-action-fund"]
        XCTAssertTrue(fundAction.waitForExistence(timeout: 10))
        app.scrollToElement(fundAction)
        fundAction.tap()

        XCTAssertTrue(app.navigationBars["Fund this cabal"].waitForExistence(timeout: 10))

        let amountField = app.textFields["fund-cabal-amount-field"]
        XCTAssertTrue(amountField.waitForExistence(timeout: 10))
        amountField.tap()
        amountField.typeText("1")

        attachScreenshot(app, name: "m2-t13-xbmcp-04-fund-amount")

        app.buttons["fund-cabal-submit-button"].tap()
        attachScreenshot(app, name: "m2-t13-xbmcp-05-fund-submitted")
    }

    @MainActor
    func testAlfredEmailLoginMigratesSigner() throws {
        let app = XCUIApplication()
        app.launchEnvironment = authLaunchEnvironment()
        app.launch()

        if app.buttons["Sign out"].waitForExistence(timeout: 5) {
            app.buttons["Sign out"].tap()
        }

        if app.buttons["Email"].waitForExistence(timeout: 5) {
            app.buttons["Email"].tap()
        }

        let emailField = app.textFields["Email address"]
        XCTAssertTrue(emailField.waitForExistence(timeout: 15))
        emailField.tap()
        emailField.typeText("test-8081@example.com")

        app.buttons["Send code"].tap()

        let codeField = app.textFields["6-digit code"]
        XCTAssertTrue(codeField.waitForExistence(timeout: 20))
        codeField.tap()
        codeField.typeText("465354")

        app.buttons["Verify code"].tap()

        XCTAssertTrue(appLandedInApp(app, timeout: 60), "expected Home tab or account after email login")
    }

    @MainActor
    func testLaunchPerformance() throws {
        // This measures how long it takes to launch your application.
        measure(metrics: [XCTApplicationLaunchMetric()]) {
            XCUIApplication().launch()
        }
    }
}
