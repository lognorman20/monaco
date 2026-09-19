import XCTest

/// Opt-in smoke against local API and a configured Privy test account.
/// Supply TEST_RUNNER_MONACO_TEST_EMAIL and TEST_RUNNER_MONACO_TEST_OTP.
/// Forward Privy launch settings with the same TEST_RUNNER_ prefix.
@MainActor
final class LiveShellTests: XCTestCase {
    func testLoginTabsAndSignOut() throws {
        let environment = ProcessInfo.processInfo.environment
        guard let email = environment["MONACO_TEST_EMAIL"],
              let code = environment["MONACO_TEST_OTP"] else {
            throw XCTSkip("Requires a local API and Privy test-account credentials")
        }
        continueAfterFailure = false
        let app = XCUIApplication()
        for key in ["PRIVY_APP_ID", "PRIVY_APP_CLIENT_ID", "PRIVY_AUTHORIZATION_KEY_ID"] {
            if let value = environment[key] { app.launchEnvironment[key] = value }
        }
        app.launchEnvironment["PRIVY_EMAIL_LOGIN_ENABLED"] = "true"
        app.launch()
        defer {
            let screenshot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
            screenshot.name = "live-shell"
            screenshot.lifetime = .keepAlways
            add(screenshot)
        }

        completeOnboardingIfNeeded(app)
        if app.tabBars.buttons["Settings"].waitForExistence(timeout: 5) {
            app.tabBars.buttons["Settings"].tap()
            app.buttons["Sign out"].tap()
        }
        let emailOption = app.buttons["Email"]
        if emailOption.waitForExistence(timeout: 10) { emailOption.tap() }
        let emailField = app.textFields["emailAddressField"]
        XCTAssertTrue(emailField.waitForExistence(timeout: 15))
        emailField.tap()
        emailField.typeText(email)
        app.buttons["emailSendCodeButton"].tap()
        let codeField = app.textFields["emailCodeField"]
        XCTAssertTrue(codeField.waitForExistence(timeout: 25))
        codeField.tap()
        codeField.typeText(code)
        app.buttons["emailVerifyButton"].tap()

        completeOnboardingIfNeeded(app)
        XCTAssertTrue(app.staticTexts["Your portfolio"].waitForExistence(timeout: 40))
        capture("live-home")
        app.tabBars.buttons["Profile"].tap()
        XCTAssertTrue(app.navigationBars["Profile"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["profile-your-cabals-link"].waitForExistence(timeout: 10))
        app.tabBars.buttons["Cabals"].tap()
        XCTAssertTrue(app.staticTexts["Invest together."].waitForExistence(timeout: 10))
        app.tabBars.buttons["Assets"].tap()
        XCTAssertTrue(app.textFields["assets-search-field"].waitForExistence(timeout: 15))
        let stock = app.buttons["assets-popular-AAPLx"]
        XCTAssertTrue(stock.waitForExistence(timeout: 60), "Live stock catalog must finish loading")
        capture("live-assets")
        stock.tap()
        let buy = app.buttons["asset-detail-buy"]
        XCTAssertTrue(buy.waitForExistence(timeout: 30), "Stock detail must finish loading")
        capture("live-asset-detail")
        app.tabBars.buttons["Settings"].tap()
        XCTAssertTrue(app.buttons["Sign out"].waitForExistence(timeout: 10))
        app.buttons["Sign out"].tap()
        XCTAssertTrue(app.buttons["Send code"].waitForExistence(timeout: 15))
        XCTAssertFalse(app.tabBars.buttons["Home"].exists)
    }

    private func capture(_ name: String) {
        let screenshot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        screenshot.name = name
        screenshot.lifetime = .keepAlways
        add(screenshot)
    }

    private func completeOnboardingIfNeeded(_ app: XCUIApplication) {
        let username = app.textFields["onboarding-username-field"]
        if username.waitForExistence(timeout: 5) {
            username.tap()
            username.typeText("Alfred")
            app.buttons["onboarding-username-continue-button"].tap()
        }
        let skip = app.buttons["onboarding-skip-button"]
        if skip.waitForExistence(timeout: 5) { skip.tap() }
    }
}
