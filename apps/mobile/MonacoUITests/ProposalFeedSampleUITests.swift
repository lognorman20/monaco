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
        XCTAssertTrue(element("proposal-card-comment-count-sample-0").exists)
        // The proposer's thesis shows as a two-line excerpt on the card.
        XCTAssertTrue(element("proposal-card-reason-sample-0").exists)
        capture("01-feed")

        // Vote yes from the card: buttons disappear and the tally updates.
        yes.tap()
        XCTAssertTrue(app.staticTexts["Vote in"].waitForExistence(timeout: 5))
        let tally = element("proposal-card-votes-sample-0")
        let updated = NSPredicate(format: "label CONTAINS %@", "1 of 5 voted · 3 yes to pass")
        wait(for: [expectation(for: updated, evaluatedWith: tally)], timeout: 5)
        XCTAssertFalse(element("proposal-card-vote-yes-sample-0").exists)
        XCTAssertTrue(element("proposal-card-voted-sample-0").exists)
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
        // Detail quotes the full thesis once, in its own block.
        XCTAssertTrue(element("proposal-detail-thesis").exists)
        XCTAssertEqual(app.descendants(matching: .any).matching(identifier: "proposal-card-reason-sample-0").count, 0)
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

    func testReadOnlyProposal_showsTallyWithoutButtons() throws {
        let card = element("proposal-card-sample-3")
        var swipes = 0
        while !card.exists && swipes < 6 {
            app.swipeUp()
            swipes += 1
        }
        XCTAssertTrue(card.waitForExistence(timeout: 5))
        XCTAssertTrue(element("proposal-card-votes-sample-3").exists)
        XCTAssertFalse(element("proposal-card-vote-yes-sample-3").exists)
        XCTAssertFalse(element("proposal-card-vote-no-sample-3").exists)
    }
}

/// Drives the propose sheet on sample data (`-MonacoProposalFeedSample -MonacoProposeSample`):
/// chooser → pick a stock → amount preset → review → send, then the toast on the cabal screen.
final class ProposeFlowSampleUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-MonacoProposalFeedSample", "-MonacoProposeSample"]
        app.launch()
    }

    private func element(_ id: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: id).firstMatch
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

    /// With the keyboard up, the reason field sits fully above the pinned Review button.
    private func assertAboveReview(_ field: XCUIElement, file: StaticString = #filePath, line: UInt = #line) {
        let review = app.buttons["Review"]
        XCTAssertTrue(field.isHittable, "reason field is covered", file: file, line: line)
        XCTAssertLessThanOrEqual(field.frame.maxY, review.frame.minY, "reason field runs under Review", file: file, line: line)
    }

    func testBuy_threeSteps_sendsToCabal() throws {
        let propose = element("group-action-propose")
        XCTAssertTrue(propose.waitForExistence(timeout: 10))
        propose.tap()

        let buy = element("propose-kind-buy")
        XCTAssertTrue(buy.waitForExistence(timeout: 5))
        sleep(1)
        capture("10-chooser")
        buy.tap()

        let apple = element("proposal-asset-AAPLx")
        XCTAssertTrue(apple.waitForExistence(timeout: 5))
        sleep(1)
        capture("11-pick-stock")
        apple.tap()

        let preset = app.buttons["$50"]
        XCTAssertTrue(preset.waitForExistence(timeout: 5))
        sleep(1)
        capture("12a-amount-empty")
        preset.tap()
        sleep(1)
        capture("12-amount")

        element("amount-entry-field").typeText("00")
        XCTAssertTrue(app.staticTexts["More than the pot has"].waitForExistence(timeout: 3))
        XCTAssertFalse(app.buttons["Review"].isEnabled)
        capture("13-amount-over")
        preset.tap()

        // Optional thesis: sent with the proposal and repeated on the review receipt.
        element("proposal-add-reason").tap()
        let thesis = element("proposal-thesis-field")
        XCTAssertTrue(thesis.waitForExistence(timeout: 3))
        thesis.typeText("Earnings Thursday.")
        sleep(1)
        assertAboveReview(thesis)
        capture("13b-amount-reason")

        app.buttons["Review"].tap()
        let send = app.buttons["Send to cabal"]
        XCTAssertTrue(send.waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["Earnings Thursday."].exists)
        sleep(1)
        capture("14-review")
        send.tap()

        XCTAssertTrue(app.staticTexts["Proposal sent to Weekend investors"].waitForExistence(timeout: 5))
        capture("15-sent")
    }

    func testSell_dollarsToShares_reviewShowsEstimate() throws {
        let propose = element("group-action-propose")
        XCTAssertTrue(propose.waitForExistence(timeout: 10))
        propose.tap()
        let sell = element("propose-kind-sell")
        XCTAssertTrue(sell.waitForExistence(timeout: 5))
        sell.tap()

        let apple = element("proposal-sell-AAPLx")
        XCTAssertTrue(apple.waitForExistence(timeout: 5))
        sleep(1)
        capture("20-sell-pick")
        apple.tap()

        let half = app.buttons["50%"]
        XCTAssertTrue(half.waitForExistence(timeout: 5))
        half.tap()
        let thesis = element("proposal-sell-thesis-field")
        XCTAssertTrue(thesis.waitForExistence(timeout: 3))
        thesis.tap()
        thesis.typeText("Take some profit before earnings.")
        sleep(1)
        assertAboveReview(thesis)
        capture("21-sell-amount")

        app.buttons["Review"].tap()
        let send = app.buttons["Send to cabal"]
        XCTAssertTrue(send.waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["Take some profit before earnings."].exists)
        sleep(1)
        capture("22-sell-review")
        send.tap()
        XCTAssertTrue(app.staticTexts["Proposal sent to Weekend investors"].waitForExistence(timeout: 5))
    }
}
