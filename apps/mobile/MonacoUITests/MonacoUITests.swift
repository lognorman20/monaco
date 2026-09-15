//
//  MonacoUITests.swift
//  MonacoUITests
//
//  Created by Logan Norman on 9/15/26.
//

import XCTest

final class MonacoUITests: XCTestCase {

    private func privyLaunchEnvironment() -> [String: String] {
        let keys = [
            "PRIVY_APP_ID",
            "PRIVY_APP_CLIENT_ID",
            "PRIVY_SMS_LOGIN_ENABLED",
            "PRIVY_EMAIL_LOGIN_ENABLED",
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
    func testLaunchPerformance() throws {
        // This measures how long it takes to launch your application.
        measure(metrics: [XCTApplicationLaunchMetric()]) {
            XCUIApplication().launch()
        }
    }
}
