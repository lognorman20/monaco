//
//  ProfileNameSaveSampleUITests.swift
//  MonacoUITests
//
//  QA coverage for Profile → Edit profile against the debug sample harness
//  (launch argument -MonacoProfileSample saveFailure). No sign-in and no backend, so a
//  Save is always rejected — which is exactly the path that used to report nothing at all.
//

import XCTest

final class ProfileNameSaveSampleUITests: XCTestCase {

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    @MainActor
    private func launchApp(_ scenario: String) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-MonacoProfileSample", scenario]
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

    // MARK: - A rejected save says so, inside the sheet

    /// The regression: the result was raised as a toast on the screen *presenting* the
    /// sheet, so it rendered behind the medium detent and vanished after 2.5s. Nobody ever
    /// saw why a rename failed.
    @MainActor
    func testRejectedSaveShowsTheReasonInsideTheSheet() throws {
        let app = launchApp("saveFailure")

        let saveButton = anyElement(app, "profile-name-save")
        XCTAssertTrue(saveButton.waitForExistence(timeout: 10), "the edit sheet should open with Save available")
        XCTAssertTrue(saveButton.isEnabled, "the sample draft is valid and differs from the saved name")

        let error = anyElement(app, "profile-name-error")
        XCTAssertFalse(error.exists, "no error before the save is attempted")

        saveButton.tap()

        XCTAssertTrue(
            error.waitForExistence(timeout: 10),
            "a rejected save must name its reason next to the field, not behind the sheet"
        )
        attachScreenshot(app, name: "01-save-rejected")

        // Still readable a beat later: the old toast cleared itself after 2.5 seconds.
        Thread.sleep(forTimeInterval: 4)
        XCTAssertTrue(error.exists, "the reason must stay while the sheet is open")
    }

    /// Typing again is the retry, so the stale reason has to go.
    @MainActor
    func testEditingTheNameClearsTheRejection() throws {
        let app = launchApp("saveFailure")

        let saveButton = anyElement(app, "profile-name-save")
        XCTAssertTrue(saveButton.waitForExistence(timeout: 10))
        saveButton.tap()

        let error = anyElement(app, "profile-name-error")
        XCTAssertTrue(error.waitForExistence(timeout: 10))

        let field = app.textFields["profile-name-field"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        field.typeText("a")

        let cleared = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: error)
        wait(for: [cleared], timeout: 10)
    }

    // MARK: - Header controls stay individually addressable

    /// The header used to `.combine` its children, which merged the photo picker and the
    /// edit button into one element and hid both identifiers.
    @MainActor
    func testHeaderKeepsItsTwoButtonsSeparate() throws {
        let app = launchApp("placeholder")

        XCTAssertTrue(
            anyElement(app, "profile-edit-button").waitForExistence(timeout: 10),
            "the edit button must be addressable in its own right"
        )
        XCTAssertTrue(
            anyElement(app, "profile-photo-picker").exists,
            "the photo picker must be addressable in its own right"
        )
    }

    // MARK: - Sign out asks first

    @MainActor
    func testSignOutAsksBeforeItSignsOut() throws {
        let app = launchApp("cabals")

        let signOut = app.buttons["profile-sign-out"]
        XCTAssertTrue(signOut.waitForExistence(timeout: 10), "Sign out should be on the profile")
        signOut.tap()

        let confirm = anyElement(app, "profile-sign-out-confirm")
        XCTAssertTrue(
            confirm.waitForExistence(timeout: 5),
            "a stray tap must not sign anyone out; it must ask first"
        )
        attachScreenshot(app, name: "02-sign-out-confirm")

        app.buttons["Cancel"].firstMatch.tap()
        XCTAssertTrue(signOut.waitForExistence(timeout: 5), "cancelling leaves the member on the profile")
    }
}
