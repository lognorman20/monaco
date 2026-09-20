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

    private func anyElement(_ app: XCUIApplication, _ identifier: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: identifier).firstMatch
    }

    /// The regression: any arriving message scrolled the thread to the bottom, so a member
    /// reading backlog in a lively cabal was thrown to the newest message every few seconds.
    @MainActor
    func testAMessageArrivingWhileReadingHistoryDoesNotYankTheThread() throws {
        let app = launchBusyChat()

        let thread = anyElement(app, "group-chat-thread")
        XCTAssertTrue(thread.waitForExistence(timeout: 20), "the sample thread should load")

        // Scroll up into the backlog and pick a line to keep an eye on.
        thread.swipeDown()
        thread.swipeDown()
        let anchor = anyElement(app, "group-chat-message-b8")
        XCTAssertTrue(anchor.waitForExistence(timeout: 10), "a backlog line should be on screen after scrolling up")
        let anchorFrame = anchor.frame
        attachScreenshot(app, name: "01-reading-history")

        // The poll runs every 4s and the sample posts a message from Ana on each tick.
        let pill = anyElement(app, "group-chat-new-messages")
        XCTAssertTrue(
            pill.waitForExistence(timeout: 30),
            "an arrival while scrolled up should be offered as a pill, not forced on the reader"
        )
        attachScreenshot(app, name: "02-new-messages-pill")

        XCTAssertTrue(anchor.exists, "the line being read must still be on screen")
        XCTAssertEqual(
            anchor.frame.minY,
            anchorFrame.minY,
            accuracy: 24,
            "the thread must not have scrolled under the reader"
        )
    }

    /// The pill is the way back down, and taking it clears the count.
    @MainActor
    func testTappingThePillReturnsToTheNewestMessage() throws {
        let app = launchBusyChat()

        let thread = anyElement(app, "group-chat-thread")
        XCTAssertTrue(thread.waitForExistence(timeout: 20))
        thread.swipeDown()
        thread.swipeDown()

        let pill = anyElement(app, "group-chat-new-messages")
        XCTAssertTrue(pill.waitForExistence(timeout: 30))
        pill.tap()

        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: pill)
        wait(for: [gone], timeout: 10)
        attachScreenshot(app, name: "03-back-at-the-bottom")
    }

    /// Sending is still meant to take the sender to their own message.
    @MainActor
    func testSendingScrollsTheSenderToTheirOwnMessage() throws {
        let app = launchBusyChat()

        let composer = anyElement(app, "group-chat-composer")
        XCTAssertTrue(composer.waitForExistence(timeout: 20), "the composer should be on screen")
        composer.tap()
        composer.typeText("Counting me in for Thursday")

        let send = anyElement(app, "group-chat-send")
        XCTAssertTrue(send.waitForExistence(timeout: 5))
        send.tap()

        let sent = app.staticTexts["Counting me in for Thursday"]
        XCTAssertTrue(sent.waitForExistence(timeout: 15), "the sent message should be on screen")
        // Snapshot-and-clear: the field must not still hold what was just posted.
        XCTAssertEqual(composer.value as? String, "Message your cabal")
    }
}
