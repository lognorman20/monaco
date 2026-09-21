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

        // Hittable, not merely present: a row stranded just above the fold is still in the
        // hierarchy, and that is exactly the failure this guards — the thread coming to rest
        // short of the end, where the reader never counts as being at the bottom.
        let atTheEnd = expectation(for: NSPredicate(format: "isHittable == true"), evaluatedWith: newest)
        wait(for: [atTheEnd], timeout: 10)

        // And it stays cleared: messages keep arriving in this sample, and following the thread
        // means they land without the count starting up again behind the reader.
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: pill)
        wait(for: [gone], timeout: 10)
        attachScreenshot(app, name: "03-back-at-the-bottom")
    }

    /// A drag that never leaves the end of the thread is not the reader leaving it.
    ///
    /// The regression: `readerControlsScroll` latched on *any* drag and was only cleared by the
    /// bottom-ness of the thread changing from false to true. A drag that stayed inside the
    /// 40pt pinned window — swiping down to dismiss the keyboard, a flick to check for new
    /// messages — never produces that transition, so the latch stayed set for the life of the
    /// view: every arrival was counted unread, the pill was drawn over the message that had
    /// just landed, and the correction that keeps a LazyVStack at its end was switched off.
    @MainActor
    func testASmallDragAtTheEndLeavesTheThreadFollowing() throws {
        let app = launchBusyChat()

        let newest = anyElement(app, newestSampleMessage)
        if !newest.waitForExistence(timeout: 40) {
            XCTFail("the sample thread should open at its newest message. On screen:\n\(app.debugDescription.suffix(9000))")
            return
        }

        // Twenty points, inside the 40pt window that counts as the end of the thread. Dragged
        // slowly and *held* before lifting, because a flick carries momentum and would coast
        // right out of the window — which would be the reader genuinely leaving, not the case
        // under test.
        let thread = anyElement(app, "group-chat-thread")
        let start = thread.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        start.press(
            forDuration: 0.1,
            thenDragTo: start.withOffset(CGVector(dx: 0, dy: 20)),
            withVelocity: .slow,
            thenHoldForDuration: 0.5
        )

        // The busy sample posts one of these on every poll.
        let arrivals = app.staticTexts.matching(
            NSPredicate(format: "label CONTAINS %@", "Still thinking about Thursday")
        )
        let firstArrival = arrivals.element(boundBy: 0)
        if !firstArrival.waitForExistence(timeout: 40) {
            XCTFail("the busy sample should post a message on each poll. On screen:\n\(app.debugDescription.suffix(9000))")
            return
        }

        // The reader never went anywhere, so the arrival belongs on screen, not behind a pill.
        XCTAssertFalse(
            newMessagesPill(app).exists,
            "a drag inside the pinned window is not the reader taking the thread over. On screen:\n\(app.debugDescription.suffix(6000))"
        )
        let latest = try XCTUnwrap(arrivals.allElementsBoundByIndex.last)
        XCTAssertTrue(
            latest.isHittable,
            "the newest message should be on screen, not stranded above the fold"
        )
        attachScreenshot(app, name: "05-small-drag-still-following")
    }

    /// "Load earlier" prepends a page above the reader and has to leave them where they were.
    ///
    /// Until the sample harness paged, it always answered with `nextCursor` nil, so `hasOlder`
    /// was never true, the button never appeared and none of the place-keeping had any
    /// coverage at all. The failure it guards: the content-size correction that keeps a
    /// LazyVStack at its end fires on the prepend and races the place-keeping scroll, dropping
    /// the reader at the newest message — the opposite of what they asked for.
    @MainActor
    func testLoadingEarlierMessagesKeepsTheReadersRow() throws {
        let app = launchBusyChat()

        let newest = anyElement(app, newestSampleMessage)
        XCTAssertTrue(newest.waitForExistence(timeout: 40), "the sample thread should open at its newest message")

        // Walk up to the top of the loaded page, where the button lives.
        let loadEarlier = app.buttons["group-chat-load-earlier"]
        for _ in 0..<12 where !loadEarlier.exists {
            app.swipeDown()
        }
        if !loadEarlier.waitForExistence(timeout: 10) {
            XCTFail("a paged sample should offer earlier messages. On screen:\n\(app.debugDescription.suffix(9000))")
            return
        }
        attachScreenshot(app, name: "06-at-the-top-of-the-page")

        // Whichever row is topmost right now is the one the reader is sitting on; ask the
        // thread rather than working it out from the sample's arithmetic.
        let messageRows = app.descendants(matching: .any).matching(
            NSPredicate(format: "identifier BEGINSWITH %@", "group-chat-message-")
        )
        let readersRowID = try XCTUnwrap(
            messageRows.allElementsBoundByIndex.first?.identifier,
            "the thread should have rows on screen"
        )

        loadEarlier.tap()

        // The older page landed, so there is now a row above the one they were on…
        let prepended = expectation(
            for: NSPredicate(format: "identifier != %@", readersRowID),
            evaluatedWith: messageRows.element(boundBy: 0)
        )
        wait(for: [prepended], timeout: 20)

        // …and they are still looking at their row, not at the bottom of the thread.
        let readersRow = anyElement(app, readersRowID)
        XCTAssertTrue(readersRow.isHittable, "the row the reader was on should still be on screen")
        XCTAssertFalse(
            newest.isHittable,
            "asking for history must not drop the reader at the newest message"
        )
        attachScreenshot(app, name: "07-history-loaded-in-place")
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
