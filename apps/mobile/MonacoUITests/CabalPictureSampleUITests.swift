//
//  CabalPictureSampleUITests.swift
//  MonacoUITests
//
//  Only a cabal's creator may change its picture, so only the creator is offered the control.
//  The harness behind `-MonacoGroupDetailSample picture | noPicture | pictureNotCreator |
//  pictureUploadFailure` puts the
//  real cabal screen up on canned data with a generated picture on a file URL, no network.
//

import XCTest

final class CabalPictureSampleUITests: XCTestCase {

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
    func testCreatorIsOfferedThePictureControl() {
        let app = launchApp(scenario: "picture")

        let picker = anyElement(app, "cabal-picture-picker")
        XCTAssertTrue(picker.waitForExistence(timeout: 20), "the creator should get the picture control")
        XCTAssertEqual(picker.label, "Change cabal picture")
    }

    @MainActor
    func testCreatorWithoutAPictureIsAskedToAddOne() {
        let app = launchApp(scenario: "noPicture")

        let picker = anyElement(app, "cabal-picture-picker")
        XCTAssertTrue(picker.waitForExistence(timeout: 20))
        XCTAssertEqual(picker.label, "Add cabal picture")
    }

    /// A member who did not create the cabal gets the plain mark: nothing to tap that would
    /// only answer 403. The header's action row proves the screen is up, so the absence means
    /// something.
    @MainActor
    func testMemberWhoIsNotTheCreatorGetsNoControl() {
        let app = launchApp(scenario: "pictureNotCreator")

        XCTAssertTrue(
            app.staticTexts[GroupPictureSampleCopy.cabalName].waitForExistence(timeout: 20),
            "the cabal screen should be up"
        )
        XCTAssertFalse(anyElement(app, "cabal-picture-picker").exists)
    }

    /// The picture is drawn, not just promised: the mark's value reports what it is showing,
    /// and it only says "Picture" once the image has replaced the initials.
    @MainActor
    func testThePictureReplacesTheInitials() {
        let app = launchApp(scenario: "pictureNotCreator")

        let mark = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label == %@", "\(GroupPictureSampleCopy.cabalName) picture"))
            .firstMatch
        XCTAssertTrue(mark.waitForExistence(timeout: 20), "the read-only mark should carry its own label")
        let drawn = expectation(for: NSPredicate(format: "value == %@", "Picture"), evaluatedWith: mark)
        wait(for: [drawn], timeout: 10)
    }

    /// Every write is refused in this scenario. The member is told why in the server's terms,
    /// and the picture the cabal still has stays on screen.
    @MainActor
    func testARefusedRemovalSaysWhyAndKeepsThePicture() {
        let app = launchApp(scenario: "pictureUploadFailure")

        let picker = anyElement(app, "cabal-picture-picker")
        XCTAssertTrue(picker.waitForExistence(timeout: 20))
        XCTAssertEqual(picker.label, "Change cabal picture")

        picker.press(forDuration: 1.0)
        let remove = app.buttons["Remove picture"]
        XCTAssertTrue(remove.waitForExistence(timeout: 5), "a creator with a picture can remove it")
        remove.tap()

        let failure = app.staticTexts["Cabal pictures are not set up on this server."]
        XCTAssertTrue(failure.waitForExistence(timeout: 10), "the refusal should be toasted")
        XCTAssertEqual(picker.label, "Change cabal picture", "the refused removal must not clear the picture")
    }
}

/// The sample cabal's name, as `GroupDetailSampleData.view` spells it.
private enum GroupPictureSampleCopy {
    static let cabalName = "Weekend investors"
}
