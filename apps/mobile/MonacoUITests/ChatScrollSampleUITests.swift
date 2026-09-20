//
//  ChatScrollSampleUITests.swift
//  MonacoUITests
//
//  QA coverage for cabal chat's reading position against the debug sample harness
//  (-MonacoChatSampleQA -MonacoChatSampleBusy). No sign-in and no backend: the sample
//  service holds a long backlog and posts a new message from another member on every poll.
//

import XCTest

final class ChatScrollSampleUITests: XCTestCase {

    /// The newest message in the sample backlog, so "are we at the bottom?" has an answer.
    private let newestSampleMessage = "group-chat-message-s8"

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    @MainActor
    private func launchBusyChat() -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-MonacoChatSampleQA", "-MonacoChatSampleBusy"]
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

    /// Message bubbles and the pill are static text and buttons, so ask for those types
    /// rather than walking a whole thread's worth of elements.
    private func anyElement(_ app: XCUIApplication, _ identifier: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: identifier).firstMatch
    }

    private func newMessagesPill(_ app: XCUIApplication) -> XCUIElement {
        app.buttons["group-chat-new-messages"]
    }

    /// Scrolls up into the backlog until the newest message is no longer realised.
    @MainActor
    private func scrollIntoHistory(_ app: XCUIApplication) {
        let newest = anyElement(app, newestSampleMessage)
        for _ in 0..<4 where newest.exists {
            app.swipeDown()
        }
    }

    /// The regression: any arriving message scrolled the thread to the bottom, so a member
    /// reading backlog in a lively cabal was thrown to the newest message every few seconds.
    @MainActor
    func testAMessageArrivingWhileReadingHistoryDoesNotYankTheThread() throws {
        let app = launchBusyChat()

        let newest = anyElement(app, newestSampleMessage)
        if !newest.waitForExistence(timeout: 30) {
            XCTFail("the sample thread should open at its newest message. On screen:\n\(app.debugDescription.suffix(9000))")
            return
        }

        scrollIntoHistory(app)
        XCTAssertFalse(newest.exists, "the viewer should now be reading backlog, not the newest message")
        attachScreenshot(app, name: "01-reading-history")

        // The poll runs every 4s and the sample posts a message from Ana on each tick.
        let pill = newMessagesPill(app)
        if !pill.waitForExistence(timeout: 40) {
            XCTFail("an arrival while scrolled up should be offered as a pill, not forced on the reader. On screen:\n\(app.debugDescription.suffix(9000))")
            return
        }
        attachScreenshot(app, name: "02-new-messages-pill")

        XCTAssertFalse(
            newest.exists,
            "the thread must not have scrolled out from under the reader"
        )
    }

    /// The pill is the way back down, and taking it clears the count.
    @MainActor
    func testTappingThePillReturnsToTheNewestMessage() throws {
        let app = launchBusyChat()

        let newest = anyElement(app, newestSampleMessage)
        XCTAssertTrue(newest.waitForExistence(timeout: 40))
        scrollIntoHistory(app)

        let pill = newMessagesPill(app)
        XCTAssertTrue(pill.waitForExistence(timeout: 60))
        pill.tap()

        XCTAssertTrue(
            newest.waitForExistence(timeout: 10),
            "tapping the pill should take the reader back to the end of the thread"
        )
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: pill)
        wait(for: [gone], timeout: 10)
        attachScreenshot(app, name: "03-back-at-the-bottom")
    }

    /// Sending still takes the sender to their own message, and the composer is cleared
    /// rather than having the submitted text subtracted from it afterwards.
    @MainActor
    func testSendingClearsTheComposerAndShowsTheMessage() throws {
        let app = launchBusyChat()

        // The one text field on this screen; cheaper than walking a thread-sized tree.
        let composer = app.textFields.firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 40), "the composer should be on screen")
        composer.tap()
        composer.typeText("Counting me in for Thursday")

        let send = app.buttons["group-chat-send"]
        XCTAssertTrue(send.waitForExistence(timeout: 10))
        // It only enables once the draft validates, so do not race it.
        let enabled = expectation(for: NSPredicate(format: "isEnabled == true"), evaluatedWith: send)
        wait(for: [enabled], timeout: 10)
        send.tap()

        // A bubble is one combined element, so its label is "You, 9:41 AM: <body>".
        let sent = app.staticTexts
            .matching(NSPredicate(format: "label CONTAINS %@", "Counting me in for Thursday"))
            .firstMatch
        XCTAssertTrue(sent.waitForExistence(timeout: 20), "the sent message should appear in the thread")
        XCTAssertEqual(
            composer.value as? String,
            "Message your cabal",
            "the composer must be empty after a send, showing its placeholder"
        )
        attachScreenshot(app, name: "04-sent")
    }
}
