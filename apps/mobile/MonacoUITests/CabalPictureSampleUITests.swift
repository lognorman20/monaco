//
//  CabalPictureSampleUITests.swift
//  MonacoUITests
//
//  Only a cabal's creator may change its picture, so only the creator is offered the control.
//  The harness behind `-MonacoGroupDetailSample picture | noPicture | pictureNotCreator` puts the
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
}

/// The sample cabal's name, as `GroupDetailSampleData.view` spells it.
private enum GroupPictureSampleCopy {
    static let cabalName = "Weekend investors"
}
