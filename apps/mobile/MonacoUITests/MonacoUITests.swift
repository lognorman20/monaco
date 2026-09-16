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

    private func privyLaunchEnvironment() -> [String: String] {
        let keys = [
            "PRIVY_APP_ID",
            "PRIVY_APP_CLIENT_ID",
            "PRIVY_SMS_LOGIN_ENABLED",
            "PRIVY_EMAIL_LOGIN_ENABLED",
            "PRIVY_AUTHORIZATION_KEY_ID",
        ]
        let defaults: [String: String] = [
            "PRIVY_APP_ID": "cmu26uw5s00mp0cl81v6dud1n",
            "PRIVY_APP_CLIENT_ID": "client-WY6dnErYgTBNFxTtjREKwCSYjwu5mecwunVjEndGhLy12",
            "PRIVY_SMS_LOGIN_ENABLED": "true",
            "PRIVY_EMAIL_LOGIN_ENABLED": "true",
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
        app.launchEnvironment = privyLaunchEnvironment()
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

        XCTAssertTrue(
            app.staticTexts["Member wallet"].waitForExistence(timeout: 30)
                || app.staticTexts["Your account"].waitForExistence(timeout: 30)
        )

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
    private func loginIfNeeded(_ app: XCUIApplication) {
        if app.staticTexts["Your account"].waitForExistence(timeout: 5) {
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

        XCTAssertTrue(
            app.staticTexts["Member wallet"].waitForExistence(timeout: 30)
                || app.staticTexts["Your account"].waitForExistence(timeout: 30)
        )
    }

    @MainActor
    private func attachScreenshot(_ app: XCUIApplication, name: String) {
        let attachment = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    @MainActor
    func testM2DepositLinkReachable() throws {
        let app = XCUIApplication()
        app.launchEnvironment = privyLaunchEnvironment()
        app.launch()

        loginIfNeeded(app)

        app.buttons["Create group"].tap()

        let nameField = app.textFields["Group name"]
        XCTAssertTrue(nameField.waitForExistence(timeout: 10))
        nameField.tap()
        nameField.typeText("M2 Deposit Reach")

        app.buttons["Create group"].tap()

        XCTAssertTrue(app.staticTexts["Treasury address"].waitForExistence(timeout: 30))

        let depositLink = app.buttons["deposit-usdc-link"]
        XCTAssertTrue(depositLink.waitForExistence(timeout: 10))
        app.scrollToElement(depositLink)
        XCTAssertTrue(depositLink.isHittable)
        depositLink.tap()

        XCTAssertTrue(app.staticTexts["Member wallet"].waitForExistence(timeout: 15))
        XCTAssertTrue(app.textFields["deposit-amount-field"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["create-deposit-button"].waitForExistence(timeout: 10))
        attachScreenshot(app, name: "m2-t13-deposit-reachable")
    }

    @MainActor
    func testM2DepositLive() throws {
        let app = XCUIApplication()
        app.launchEnvironment = privyLaunchEnvironment()
        app.launch()

        loginIfNeeded(app)
        attachScreenshot(app, name: "m2-t13-xbmcp-01-me")

        app.buttons["Create group"].tap()

        let nameField = app.textFields["Group name"]
        XCTAssertTrue(nameField.waitForExistence(timeout: 10))
        nameField.tap()
        nameField.typeText("M2 Deposit QA")

        app.buttons["Create group"].tap()

        XCTAssertTrue(app.staticTexts["Treasury address"].waitForExistence(timeout: 30))
        attachScreenshot(app, name: "m2-t13-xbmcp-02-treasury")

        let depositLink = app.buttons["deposit-usdc-link"]
        XCTAssertTrue(depositLink.waitForExistence(timeout: 10))
        app.scrollToElement(depositLink)
        depositLink.tap()

        XCTAssertTrue(app.staticTexts["Member wallet"].waitForExistence(timeout: 15))

        let amountField = app.textFields["deposit-amount-field"]
        XCTAssertTrue(amountField.waitForExistence(timeout: 10))
        amountField.tap()
        amountField.typeText("1")

        attachScreenshot(app, name: "m2-t13-xbmcp-03-amount")

        app.buttons["create-deposit-button"].tap()

        let refreshButton = app.buttons["Refresh status"]
        XCTAssertTrue(refreshButton.waitForExistence(timeout: 15))
        attachScreenshot(app, name: "m2-t13-xbmcp-04-created")

        let deadline = Date().addingTimeInterval(120)
        var confirmed = false
        while Date() < deadline {
            refreshButton.tap()
            if app.staticTexts["confirmed"].waitForExistence(timeout: 3) {
                confirmed = true
                break
            }
            RunLoop.current.run(until: Date().addingTimeInterval(5))
        }

        attachScreenshot(app, name: "m2-t13-xbmcp-05-final")
        XCTAssertTrue(confirmed, "Deposit did not reach confirmed within 120s — check SweepPoller and member USDC balance")
    }

    @MainActor
    func testAlfredEmailLoginMigratesSigner() throws {
        let app = XCUIApplication()
        app.launchEnvironment = privyLaunchEnvironment()
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
        emailField.typeText("test-8081@privy.io")

        app.buttons["Send code"].tap()

        let codeField = app.textFields["6-digit code"]
        XCTAssertTrue(codeField.waitForExistence(timeout: 20))
        codeField.tap()
        codeField.typeText("465354")

        app.buttons["Verify code"].tap()

        XCTAssertTrue(
            app.staticTexts["Your account"].waitForExistence(timeout: 60)
                || app.staticTexts["Member wallet"].waitForExistence(timeout: 60)
        )
    }

    @MainActor
    func testLaunchPerformance() throws {
        // This measures how long it takes to launch your application.
        measure(metrics: [XCTApplicationLaunchMetric()]) {
            XCUIApplication().launch()
        }
    }
}
