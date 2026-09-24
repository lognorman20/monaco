//
//  ChatClosedSampleUITests.swift
//  MonacoUITests
//
//  QA coverage for the state cabal chat enters when the API says the thread is shut, against
//  the debug sample harness. No sign-in and no backend: the sample service answers 403 for a
//  set number of polls and is readable again afterwards.
//
//  Closing is the heaviest thing this screen does — it parks the poll, takes the composer away
//  and tells a paying member they are out of their cabal — and it used to be one-way and
//  reachable from a single background tick.
//

import XCTest

final class ChatClosedSampleUITests: XCTestCase {

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    @MainActor
    private func launchChat(_ scenario: String) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-MonacoChatSampleQA", scenario]
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

    private func closedBanner(_ app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: "group-chat-closed").firstMatch
    }

    private func composer(_ app: XCUIApplication) -> XCUIElement {
        app.textFields["group-chat-composer"]
    }

    /// The regression: the first 403 a *background* poll saw closed the thread for good. A
    /// membership read served by a replica that had not caught up, or one of the messages
    /// route's non-cabal 404s, was enough to tell a member in good standing that they had been
    /// thrown out of their cabal — and leave them nothing to tap.
    @MainActor
    func testABlipDoesNotCloseTheThread() throws {
        let app = launchChat("-MonacoChatSampleClosedBlip")

        XCTAssertTrue(
            composer(app).waitForExistence(timeout: 40),
            "the sample thread should load before the blip starts"
        )

        // Long enough for the whole run of 403s and the polls that follow them.
        let banner = closedBanner(app)
        XCTAssertFalse(
            banner.waitForExistence(timeout: 30),
            "a short run of closed answers is a blip, not a member being removed from a cabal"
        )
        XCTAssertTrue(composer(app).exists, "the composer should never have been taken away")
        attachScreenshot(app, name: "01-blip-rode-out")
    }

    /// And when a load really does come back closed, it is still not a dead end.
    ///
    /// The regression, both halves of it: the closed state hid the "Try again" button, so a
    /// member told they were no longer in their cabal had nothing on screen to tap; and it was
    /// one-way, so even a 403 the API never meant — a membership read on a lagging replica —
    /// ended the screen for the life of the view. Here the first load is refused and every
    /// call after it succeeds, which is exactly that blip.
    @MainActor
    func testAThreadThatOpensClosedStillOffersAWayBackIn() throws {
        let app = launchChat("-MonacoChatSampleClosedFirstLoad")

        let retry = app.buttons["group-chat-retry"]
        if !retry.waitForExistence(timeout: 40) {
            XCTFail("a closed thread must leave the member something to tap. On screen:\n\(app.debugDescription.suffix(9000))")
            return
        }
        // The sentence itself is asserted in GroupChatTests; here it only has to be on screen.
        XCTAssertTrue(
            app.descendants(matching: .any).matching(identifier: "group-chat-error").firstMatch.exists,
            "the screen should say why, next to the button"
        )
        attachScreenshot(app, name: "02-opened-closed")

        retry.tap()

        // Asking again reaches a cabal that is perfectly readable, so the thread comes back —
        // composer and all, rather than staying parked on the first answer it ever got.
        XCTAssertTrue(
            composer(app).waitForExistence(timeout: 30),
            "asking again should reopen the thread, not stay shut on one refused load"
        )
        XCTAssertFalse(closedBanner(app).exists, "nothing should still be saying the thread is shut")
        attachScreenshot(app, name: "03-reopened")
    }
}
