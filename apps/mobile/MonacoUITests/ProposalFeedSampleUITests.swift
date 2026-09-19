import XCTest

/// Drives the proposal feed on in-memory sample data (`-MonacoProposalFeedSample`, Debug only):
/// vote from a card, open the thread, post a comment, reply. No Privy session or backend needed.
/// Set `MONACO_QA_SCREENSHOT_DIR` (via `TEST_RUNNER_MONACO_QA_SCREENSHOT_DIR`) to save PNGs for docs/qa.
final class ProposalFeedSampleUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-MonacoProposalFeedSample"]
        app.launch()
    }

    private func capture(_ name: String) {
        let shot = XCUIScreen.main.screenshot()
        let attachment = XCTAttachment(screenshot: shot)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = ProcessInfo.processInfo.environment["MONACO_QA_SCREENSHOT_DIR"], !dir.isEmpty {
            try? shot.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
        }
    }

    private func element(_ id: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: id).firstMatch
    }

    func testFeed_voteFromCard_thenCommentAndReplyInThread() throws {
        // Feed renders cards with vote summary and inline voting.
        let yes = element("proposal-card-vote-yes-sample-0")
        XCTAssertTrue(yes.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["1 comment"].exists || app.staticTexts["2 comments"].exists)
        capture("01-feed")

        // Vote yes from the card: buttons disappear and the tally updates.
        yes.tap()
        XCTAssertTrue(app.staticTexts["Vote recorded"].waitForExistence(timeout: 5))
        let tally = element("proposal-card-votes-sample-0")
        let updated = NSPredicate(format: "label CONTAINS %@", "1 yes · 0 no · 4 still to vote")
        wait(for: [expectation(for: updated, evaluatedWith: tally)], timeout: 5)
        XCTAssertFalse(element("proposal-card-vote-yes-sample-0").exists)
        capture("02-after-card-vote")

        // Scroll performance smoke: the feed holds 20 open cards; reach the last one.
        let last = element("proposal-card-sample-19")
        var swipes = 0
        while !last.exists && swipes < 30 {
            app.swipeUp()
            swipes += 1
        }
        XCTAssertTrue(last.exists, "last of 20 open cards never rendered")
        capture("03-feed-scrolled")
        while !element("proposal-card-open-sample-0").isHittable && swipes > 0 {
            app.swipeDown()
            swipes -= 1
        }

        // Open detail: existing thread shows the nested reply.
        element("proposal-card-open-sample-0").tap()
        XCTAssertTrue(element("comment-thread").waitForExistence(timeout: 5))
        XCTAssertTrue(element("comment-row-c-2").exists)
        capture("04-detail-thread")

        // Post a top-level comment.
        let field = element("comment-composer-field")
        field.tap()
        field.typeText("Count me in if we cap it at $25.")
        element("comment-composer-send").tap()
        XCTAssertTrue(app.staticTexts["Comment posted"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["Count me in if we cap it at $25."].waitForExistence(timeout: 5))
        capture("05-comment-posted")

        // Reply to Ben's comment.
        element("comment-reply-c-1").tap()
        XCTAssertTrue(app.staticTexts["Replying to Ben Ortiz"].waitForExistence(timeout: 5))
        field.tap()
        field.typeText("Agreed, Nvidia next.")
        element("comment-composer-send").tap()
        XCTAssertTrue(app.staticTexts["Reply posted"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["Agreed, Nvidia next."].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["4 comments"].waitForExistence(timeout: 5))
        var threadSwipes = 0
        while !app.staticTexts["Agreed, Nvidia next."].isHittable && threadSwipes < 6 {
            app.swipeUp()
            threadSwipes += 1
        }
        capture("06-reply-posted")
    }

    func testComposer_whitespaceOnly_keepsPostDisabled() throws {
        let open = element("proposal-card-open-sample-1")
        XCTAssertTrue(open.waitForExistence(timeout: 10))
        open.tap()

        let field = element("comment-composer-field")
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        field.typeText("   ")

        XCTAssertFalse(element("comment-composer-send").isEnabled)
        XCTAssertTrue(element("comment-thread-empty").exists)
    }
}
