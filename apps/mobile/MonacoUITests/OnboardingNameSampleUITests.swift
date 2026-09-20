//
//  OnboardingNameSampleUITests.swift
//  MonacoUITests
//
//  QA coverage for the first-run "What should friends call you?" screen against the
//  debug sample harness (launch argument -MonacoOnboardingSample). No sign-in and no
//  backend: the app boots straight into the screen with a canned save, so this suite
//  and the screenshots it attaches stay reproducible.
//

import XCTest

final class OnboardingNameSampleUITests: XCTestCase {

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    @MainActor
    private func launchApp(_ scenario: String = "fresh") -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-MonacoOnboardingSample", scenario]
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

    /// Identifiers can land on a container rather than the control, so match any type.
    private func anyElement(_ app: XCUIApplication, _ identifier: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: identifier).firstMatch
    }

    @MainActor
    private func nameField(_ app: XCUIApplication) -> XCUIElement {
        let field = app.textFields["onboarding-name-field"]
        if field.waitForExistence(timeout: 5) {
            return field
        }
        return anyElement(app, "onboarding-name-field")
    }

    // MARK: - (a) Typing a name enables Continue

    @MainActor
    func testTypingANameEnablesContinue() throws {
        let app = launchApp()

        let field = nameField(app)
        XCTAssertTrue(field.waitForExistence(timeout: 10), "the name field should exist on launch")

        let continueButton = app.buttons["onboarding-continue"]
        XCTAssertTrue(continueButton.waitForExistence(timeout: 5), "Continue should exist on launch")
        XCTAssertFalse(continueButton.isEnabled, "Continue should start disabled with an empty name")

        attachScreenshot(app, name: "01-empty")

        field.tap()
        field.typeText("Logan Norman")

        XCTAssertTrue(
            continueButton.waitForExistence(timeout: 5) && waitUntilEnabled(continueButton),
            "Continue should enable once the name is valid"
        )

        attachScreenshot(app, name: "02-name-entered")
    }

    // MARK: - (b) Whitespace alone is not a name

    @MainActor
    func testWhitespaceOnlyNameKeepsContinueDisabled() throws {
        let app = launchApp()

        let field = nameField(app)
        XCTAssertTrue(field.waitForExistence(timeout: 10), "the name field should exist on launch")
        field.tap()
        field.typeText("   ")

        let continueButton = app.buttons["onboarding-continue"]
        XCTAssertTrue(continueButton.waitForExistence(timeout: 5), "Continue should exist")
        XCTAssertFalse(continueButton.isEnabled, "spaces alone are not a name")
    }

    // MARK: - (c) The optional photo and the escape hatch are both on screen

    @MainActor
    func testPhotoPickerAndSignOutAreAvailable() throws {
        let app = launchApp()

        XCTAssertTrue(
            anyElement(app, "onboarding-photo").waitForExistence(timeout: 10),
            "the optional photo picker should be on the first-run screen"
        )
        XCTAssertTrue(
            app.buttons["onboarding-sign-out"].waitForExistence(timeout: 5),
            "first run must never trap the user: Sign out should always be reachable"
        )
        XCTAssertFalse(
            app.buttons["Skip"].exists,
            "there is no Skip: a name is required for a social product"
        )
    }

    // MARK: - (d) A rejected save explains itself under the field, and Continue can be retried

    @MainActor
    func testFailedSaveShowsInlineErrorAndAllowsRetry() throws {
        let app = launchApp("failure")

        let field = nameField(app)
        XCTAssertTrue(field.waitForExistence(timeout: 10), "the name field should exist on launch")
        field.tap()
        field.typeText("Logan Norman")

        let continueButton = app.buttons["onboarding-continue"]
        XCTAssertTrue(waitUntilEnabled(continueButton), "Continue should enable for a valid name")
        continueButton.tap()

        let error = app.staticTexts["onboarding-name-error"]
        XCTAssertTrue(error.waitForExistence(timeout: 8), "a rejected save should explain itself under the field")
        XCTAssertEqual(error.label, "Too many changes. Try again in a minute.")
        attachScreenshot(app, name: "03-save-failed")

        XCTAssertTrue(field.exists, "the screen should stay put so the user can retry")
        XCTAssertTrue(waitUntilEnabled(continueButton), "Continue should come back so the save can be retried")
    }

    // MARK: - Helpers

    @MainActor
    private func waitUntilEnabled(_ element: XCUIElement, timeout: TimeInterval = 5) -> Bool {
        let predicate = NSPredicate(format: "isEnabled == true")
        let expectation = XCTNSPredicateExpectation(predicate: predicate, object: element)
        return XCTWaiter().wait(for: [expectation], timeout: timeout) == .completed
    }
}
