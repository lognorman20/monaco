import XCTest
@testable import MonacoCore

final class GroupChatDTOTests: XCTestCase {
    func testDecodePage_withCursor_mapsFields() throws {
        // Arrange
        let json = """
        {"messages":[{"id":"m2","groupId":"g1","authorId":"u2","authorName":"Ana","body":"buy apple?","createdAt":"2026-09-18T15:04:05.123456Z","mine":false}],
         "nextCursor":"abc_-1"}
        """

        // Act
        let page = try JSONDecoder().decode(GroupMessagesPageDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertEqual(page.nextCursor, "abc_-1")
        XCTAssertEqual(page.messages.count, 1)
        let message = page.messages[0]
        XCTAssertEqual(message.authorName, "Ana")
        XCTAssertEqual(message.body, "buy apple?")
        XCTAssertFalse(message.mine)
        let expected = ISO8601DateFormatter().date(from: "2026-09-18T15:04:05Z")!
        XCTAssertEqual(message.createdAtDate!.timeIntervalSince1970, expected.timeIntervalSince1970 + 0.123, accuracy: 0.001)
    }

    func testDecodePage_lastPage_omitsCursor() throws {
        // Arrange
        let json = #"{"messages":[]}"#

        // Act
        let page = try JSONDecoder().decode(GroupMessagesPageDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertTrue(page.messages.isEmpty)
        XCTAssertNil(page.nextCursor)
    }

    func testDecodePage_missingRequiredField_throws() {
        // Arrange: no "mine".
        let json = """
        {"messages":[{"id":"m1","groupId":"g1","authorId":"u1","authorName":"Ana","body":"hi","createdAt":"2026-09-18T15:04:05.000000Z"}]}
        """

        // Act + Assert
        XCTAssertThrowsError(try JSONDecoder().decode(GroupMessagesPageDTO.self, from: Data(json.utf8)))
    }

    func testCreatedAtDate_plainSecondsAndGarbage() {
        XCTAssertNotNil(message("a", at: "2026-09-18T15:04:05Z").createdAtDate)
        XCTAssertNil(message("a", at: "yesterday").createdAtDate)
    }
}

final class GroupChatDatesTests: XCTestCase {
    private let wholeSecond = ISO8601DateFormatter().date(from: "2026-09-18T15:04:05Z")!.timeIntervalSince1970

    func testParse_microseconds_isTheFormatTheAPISends() throws {
        let date = try XCTUnwrap(GroupChatDates.parse("2026-09-18T15:04:05.123456Z"))
        XCTAssertEqual(date.timeIntervalSince1970, wholeSecond + 0.123, accuracy: 0.001)
    }

    func testParse_milliseconds() throws {
        let date = try XCTUnwrap(GroupChatDates.parse("2026-09-18T15:04:05.123Z"))
        XCTAssertEqual(date.timeIntervalSince1970, wholeSecond + 0.123, accuracy: 0.001)
    }

    func testParse_noFraction() throws {
        let date = try XCTUnwrap(GroupChatDates.parse("2026-09-18T15:04:05Z"))
        XCTAssertEqual(date.timeIntervalSince1970, wholeSecond, accuracy: 0.0001)
    }

    func testParse_offsetIsConvertedToUTC() throws {
        let date = try XCTUnwrap(GroupChatDates.parse("2026-09-18T17:04:05.123456+02:00"))
        XCTAssertEqual(date.timeIntervalSince1970, wholeSecond + 0.123, accuracy: 0.001)
    }

    func testParse_invalid_isNil() {
        for raw in ["", "yesterday", "2026-09-18", "2026-09-18T15:04:05.Z", "2026-13-40T99:99:99.000000Z", "1758207845"] {
            XCTAssertNil(GroupChatDates.parse(raw), "parsed \(raw)")
        }
    }

    func testDecodedMessage_carriesTheParsedDate_andReencodesWithoutIt() throws {
        // Arrange
        let json = #"{"id":"m1","groupId":"g1","authorId":"u1","authorName":"Ana","body":"hi","createdAt":"2026-09-18T15:04:05.123456Z","mine":false}"#

        // Act
        let decoded = try JSONDecoder().decode(GroupMessageDTO.self, from: Data(json.utf8))
        let reencoded = try JSONSerialization.jsonObject(with: JSONEncoder().encode(decoded)) as? [String: Any]

        // Assert: the wire shape is unchanged; the Date is derived, not sent.
        XCTAssertEqual(decoded.createdAtDate, GroupChatDates.parse("2026-09-18T15:04:05.123456Z"))
        XCTAssertEqual(reencoded?["createdAt"] as? String, "2026-09-18T15:04:05.123456Z")
        XCTAssertNil(reencoded?["createdAtDate"])
    }

    func testDecodedMessage_unparseableTimestamp_stillDecodesWithNilDate() throws {
        let json = #"{"id":"m1","groupId":"g1","authorId":"u1","authorName":"Ana","body":"hi","createdAt":"soon","mine":false}"#
        let decoded = try JSONDecoder().decode(GroupMessageDTO.self, from: Data(json.utf8))
        XCTAssertNil(decoded.createdAtDate)
    }
}

/// What the thread costs to draw. SwiftUI re-runs this for every visible bubble on each pass.
final class GroupChatHotPathTests: XCTestCase {
    func testOnePassOverFiftyMessages_doesNoDateParsing() {
        // Arrange: a page and a half of history, stamped the way the API stamps it.
        let messages = (0..<50).map { index in
            message("m\(index)", at: String(format: "2026-09-18T15:%02d:05.123456Z", index))
        }

        // Act: the four date reads the thread used to make per bubble (its own separator, the
        // next bubble's separator, the run check, the VoiceOver label). Fastest of several
        // passes: a shared CI runner can stall any single pass, but it cannot make one faster.
        let clock = ContinuousClock()
        let fastest = (0..<10).map { _ in
            clock.measure {
                for message in messages {
                    for _ in 0..<4 { _ = message.createdAtDate }
                }
            }
        }.min()!
        let fastestMilliseconds = Double(fastest.components.attoseconds) / 1e15
            + Double(fastest.components.seconds) * 1000

        // Assert: parsing on every read took ~15ms here on a fast Mac, most of a 60Hz frame.
        XCTAssertLessThan(fastestMilliseconds, 1)
    }

    func testRows_carryEverythingTheThreadDraws() {
        // Arrange
        var timeline = GroupChatTimeline()

        // Act: Ana twice within a minute, then Ben twenty minutes later.
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [
            message("m3", at: "2026-09-18T15:21:00.000000Z", author: "ben"),
            message("m2", at: "2026-09-18T15:00:30.000000Z", author: "ana"),
            message("m1", at: "2026-09-18T15:00:00.000000Z", author: "ana"),
        ]))

        // Assert
        let rows = timeline.rows
        XCTAssertEqual(rows.map(\.id), ["m1", "m2", "m3"])
        XCTAssertEqual(rows.map { $0.separatorDate != nil }, [true, false, true])
        XCTAssertEqual(rows.map(\.startsRun), [true, false, true])
        XCTAssertEqual(rows.map(\.endsRun), [false, true, true])
        XCTAssertEqual(rows[0].separatorDate, rows[0].date)
        XCTAssertTrue(rows.allSatisfy { $0.delivery == .delivered })
    }

    func testRows_sameAuthorAcrossATimeSeparator_startsANewRun() {
        var timeline = GroupChatTimeline()
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [
            message("m2", at: "2026-09-18T15:30:00.000000Z", author: "ana"),
            message("m1", at: "2026-09-18T15:00:00.000000Z", author: "ana"),
        ]))

        XCTAssertEqual(timeline.rows.map(\.endsRun), [true, true])
        XCTAssertEqual(timeline.rows.map(\.startsRun), [true, true])
    }
}

final class GroupChatDraftTests: XCTestCase {
    func testValidate_trimsWhitespace() {
        XCTAssertEqual(try GroupChatDraft.validate("  hi cabal \n").get(), "hi cabal")
    }

    func testValidate_blank_isEmpty() {
        XCTAssertEqual(GroupChatDraft.validate(" \n\t "), .failure(.empty))
    }

    func testValidate_countsCharactersNotBytes() {
        // 2000 two-byte characters is allowed; 2001 is not.
        XCTAssertNoThrow(try GroupChatDraft.validate(String(repeating: "é", count: 2000)).get())
        XCTAssertEqual(
            GroupChatDraft.validate(String(repeating: "a", count: 2001)),
            .failure(.tooLong(count: 2001))
        )
    }
}

final class GroupChatTimelineTests: XCTestCase {
    func testMergeNewest_firstLoad_ordersOldestFirstAndKeepsCursor() {
        // Arrange
        var timeline = GroupChatTimeline()
        let page = GroupMessagesPageDTO(
            messages: [
                message("m3", at: "2026-09-18T15:00:03.000000Z"),
                message("m2", at: "2026-09-18T15:00:02.000000Z"),
            ],
            nextCursor: "older"
        )

        // Act
        let added = timeline.mergeNewest(page)

        // Assert
        XCTAssertTrue(timeline.hasLoadedNewest)
        XCTAssertEqual(timeline.messages.map(\.id), ["m2", "m3"])
        XCTAssertEqual(Set(added), ["m2", "m3"])
        XCTAssertEqual(timeline.olderCursor, "older")
    }

    func testMergeNewest_poll_dedupesAndReportsOnlyNewIDs() {
        // Arrange
        var timeline = GroupChatTimeline()
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [message("m1", at: "2026-09-18T15:00:01.000000Z")]))

        // Act
        let added = timeline.mergeNewest(GroupMessagesPageDTO(messages: [
            message("m2", at: "2026-09-18T15:00:02.000000Z"),
            message("m1", at: "2026-09-18T15:00:01.000000Z"),
        ]))
        let again = timeline.mergeNewest(GroupMessagesPageDTO(messages: [
            message("m2", at: "2026-09-18T15:00:02.000000Z"),
        ]))

        // Assert
        XCTAssertEqual(added, ["m2"])
        XCTAssertEqual(again, [])
        XCTAssertEqual(timeline.messages.map(\.id), ["m1", "m2"])
    }

    func testOrdering_usesMicrosecondsWithinSameSecond() {
        // Arrange: ids deliberately sort opposite to time.
        var timeline = GroupChatTimeline()

        // Act
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [
            message("a-later", at: "2026-09-18T15:00:01.000200Z"),
            message("z-earlier", at: "2026-09-18T15:00:01.000100Z"),
        ]))

        // Assert
        XCTAssertEqual(timeline.messages.map(\.id), ["z-earlier", "a-later"])
    }

    func testMergeOlder_prependsAndAdvancesCursorToNil() {
        // Arrange
        var timeline = GroupChatTimeline()
        timeline.mergeNewest(GroupMessagesPageDTO(
            messages: [message("m3", at: "2026-09-18T15:00:03.000000Z")],
            nextCursor: "c1"
        ))

        // Act
        timeline.mergeOlder(GroupMessagesPageDTO(messages: [
            message("m2", at: "2026-09-18T15:00:02.000000Z"),
            message("m1", at: "2026-09-18T15:00:01.000000Z"),
        ]))

        // Assert
        XCTAssertEqual(timeline.messages.map(\.id), ["m1", "m2", "m3"])
        XCTAssertFalse(timeline.hasOlder)
    }

    func testMergeNewest_pollWithGap_pointsOlderCursorAtGap() {
        // Arrange: on screen m1 only, with no older history.
        var timeline = GroupChatTimeline()
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [message("m1", at: "2026-09-18T15:00:01.000000Z")]))
        XCTAssertFalse(timeline.hasOlder)

        // Act: a poll returns a full page that does not reach back to m1.
        timeline.mergeNewest(GroupMessagesPageDTO(
            messages: [
                message("m9", at: "2026-09-18T15:00:09.000000Z"),
                message("m8", at: "2026-09-18T15:00:08.000000Z"),
            ],
            nextCursor: "gap"
        ))

        // Assert
        XCTAssertEqual(timeline.olderCursor, "gap")
        XCTAssertEqual(timeline.messages.map(\.id), ["m1", "m8", "m9"])
    }

    func testMergeNewest_pollOverlapping_keepsExistingOlderCursor() {
        // Arrange
        var timeline = GroupChatTimeline()
        timeline.mergeNewest(GroupMessagesPageDTO(
            messages: [message("m5", at: "2026-09-18T15:00:05.000000Z")],
            nextCursor: "before-m5"
        ))

        // Act
        timeline.mergeNewest(GroupMessagesPageDTO(
            messages: [
                message("m6", at: "2026-09-18T15:00:06.000000Z"),
                message("m5", at: "2026-09-18T15:00:05.000000Z"),
            ],
            nextCursor: "before-m5-again"
        ))

        // Assert
        XCTAssertEqual(timeline.olderCursor, "before-m5")
    }

}

final class GroupChatOptimisticSendTests: XCTestCase {
    private let now = ISO8601DateFormatter().date(from: "2026-09-18T15:00:10Z")!

    private func loadedTimeline() -> GroupChatTimeline {
        var timeline = GroupChatTimeline()
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [message("m1", at: "2026-09-18T15:00:01.000000Z")]))
        return timeline
    }

    func testBeginSend_showsTheMessageStraightAwayAsSending() {
        // Arrange
        var timeline = loadedTimeline()

        // Act
        timeline.beginSend(clientId: "c1", body: "buy apple?", at: now)

        // Assert
        XCTAssertEqual(timeline.rows.map(\.id), ["m1", "c1"])
        let row = timeline.rows[1]
        XCTAssertEqual(row.body, "buy apple?")
        XCTAssertTrue(row.mine)
        XCTAssertEqual(row.delivery, .sending)
        XCTAssertNil(row.serverId)
        XCTAssertEqual(row.date, now)
        XCTAssertEqual(timeline.messages.map(\.id), ["m1"], "nothing is a server message until the server says so")
    }

    func testBeginSend_sameClientIdTwice_addsOneRow() {
        var timeline = loadedTimeline()
        timeline.beginSend(clientId: "c1", body: "hi", at: now)
        timeline.beginSend(clientId: "c1", body: "hi", at: now)
        XCTAssertEqual(timeline.rows.map(\.id), ["m1", "c1"])
    }

    func testConfirmSent_swapsInTheServerRow_keepingTheBubbleIdentity() {
        // Arrange
        var timeline = loadedTimeline()
        timeline.beginSend(clientId: "c1", body: "hi", at: now)
        let server = message("m2", at: "2026-09-18T15:00:10.500000Z", mine: true, body: "hi")

        // Act
        timeline.confirmSent(clientId: "c1", message: server)

        // Assert
        XCTAssertTrue(timeline.pending.isEmpty)
        XCTAssertEqual(timeline.messages.map(\.id), ["m1", "m2"])
        XCTAssertEqual(timeline.rows.map(\.id), ["m1", "c1"], "same id, so SwiftUI updates the bubble instead of replacing it")
        XCTAssertEqual(timeline.rows[1].serverId, "m2")
        XCTAssertEqual(timeline.rows[1].delivery, .delivered)
        XCTAssertEqual(timeline.rows[1].date, server.createdAtDate)
    }

    func testConfirmSent_thenPollReturnsSameMessage_noDuplicate() {
        // Arrange
        var timeline = loadedTimeline()
        timeline.beginSend(clientId: "c1", body: "hi", at: now)
        let server = message("m2", at: "2026-09-18T15:00:10.500000Z", mine: true, body: "hi")
        timeline.confirmSent(clientId: "c1", message: server)

        // Act
        let added = timeline.mergeNewest(GroupMessagesPageDTO(messages: [server, message("m1", at: "2026-09-18T15:00:01.000000Z")]))

        // Assert
        XCTAssertEqual(added, [])
        XCTAssertEqual(timeline.rows.map(\.id), ["m1", "c1"])
    }

    func testPollArrivesBeforePostResponse_showsTheMessageOnce_thenConfirmChangesNothing() {
        // Arrange
        var timeline = loadedTimeline()
        timeline.beginSend(clientId: "c1", body: "hi", at: now)
        let server = message("m2", at: "2026-09-18T15:00:10.500000Z", mine: true, body: "hi")

        // Act: the poll wins the race.
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [server]))

        // Assert: one bubble, already delivered, under the identity it had while sending.
        XCTAssertEqual(timeline.rows.map(\.id), ["m1", "c1"])
        XCTAssertEqual(timeline.rows[1].delivery, .delivered)
        XCTAssertEqual(timeline.rows[1].serverId, "m2")

        // Act: then the POST answers.
        let rowsBefore = timeline.rows
        timeline.confirmSent(clientId: "c1", message: server)

        // Assert
        XCTAssertEqual(timeline.rows, rowsBefore)
        XCTAssertTrue(timeline.pending.isEmpty)
    }

    func testPollArrivesBeforePostResponse_thenPostFails_countsAsDelivered() {
        // Arrange: the message landed but the response was lost (timeout).
        var timeline = loadedTimeline()
        timeline.beginSend(clientId: "c1", body: "hi", at: now)
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [message("m2", at: "2026-09-18T15:00:10.500000Z", mine: true, body: "hi")]))

        // Act
        let outcome = timeline.failSend(clientId: "c1")

        // Assert: nothing to retry, so a retry cannot post it twice.
        XCTAssertEqual(outcome, .alreadyDelivered)
        XCTAssertTrue(timeline.pending.isEmpty)
        XCTAssertEqual(timeline.rows.map(\.delivery), [.delivered, .delivered])
    }

    func testPostFailsFirst_thenPollDeliversIt_clearsTheFailedBubble() {
        // Arrange
        var timeline = loadedTimeline()
        timeline.beginSend(clientId: "c1", body: "hi", at: now)
        XCTAssertEqual(timeline.failSend(clientId: "c1"), .markedFailed)

        // Act
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [message("m2", at: "2026-09-18T15:00:10.500000Z", mine: true, body: "hi")]))

        // Assert
        XCTAssertTrue(timeline.pending.isEmpty)
        XCTAssertEqual(timeline.rows.map(\.id), ["m1", "c1"])
        XCTAssertNil(timeline.retrySend(clientId: "c1"))
    }

    func testPollMatch_wasAnotherDevicesMessage_confirmShowsBoth() {
        // Arrange: the same text sent from a second device lands while this send is in flight.
        var timeline = loadedTimeline()
        timeline.beginSend(clientId: "c1", body: "hi", at: now)
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [message("other", at: "2026-09-18T15:00:10.400000Z", mine: true, body: "hi")]))

        // Act: the POST answers with its own, different row.
        timeline.confirmSent(clientId: "c1", message: message("m2", at: "2026-09-18T15:00:10.500000Z", mine: true, body: "hi"))

        // Assert: both messages exist on the server, so both show, with distinct ids.
        XCTAssertEqual(timeline.rows.map(\.id), ["m1", "other", "c1"])
        XCTAssertEqual(timeline.rows.map(\.serverId), ["m1", "other", "m2"])
    }

    func testPoll_doesNotMatchOtherPeoplesOrOlderOrDifferentMessages() {
        // Arrange
        var timeline = loadedTimeline()
        timeline.beginSend(clientId: "c1", body: "hi", at: now)

        // Act: same text from someone else, different text from me, and my same text from before the send.
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [
            message("theirs", at: "2026-09-18T15:00:11.000000Z", mine: false, body: "hi"),
            message("mine-other", at: "2026-09-18T15:00:11.500000Z", mine: true, body: "hello"),
        ]))
        timeline.mergeOlder(GroupMessagesPageDTO(messages: [
            message("mine-old", at: "2026-09-18T14:00:00.000000Z", mine: true, body: "hi"),
        ]))

        // Assert: still sending.
        XCTAssertEqual(timeline.rows.map(\.id), ["mine-old", "m1", "theirs", "mine-other", "c1"])
        XCTAssertEqual(timeline.rows.last?.delivery, .sending)
    }

    func testSendBeforeFirstPageLoads_firstPageNeverSwallowsIt() {
        // Arrange: the viewer types "gm" and sends before history arrives.
        var timeline = GroupChatTimeline()
        timeline.beginSend(clientId: "c1", body: "gm", at: now)

        // Act: history holds yesterday's "gm".
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [message("old", at: "2026-09-17T09:00:00.000000Z", mine: true, body: "gm")]))

        // Assert
        XCTAssertEqual(timeline.rows.map(\.id), ["old", "c1"])
        XCTAssertEqual(timeline.rows[1].delivery, .sending)
    }

    func testTwoIdenticalSends_pollMatchesOneEach() {
        // Arrange
        var timeline = loadedTimeline()
        timeline.beginSend(clientId: "c1", body: "+1", at: now)
        timeline.beginSend(clientId: "c2", body: "+1", at: now.addingTimeInterval(1))

        // Act
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [
            message("m3", at: "2026-09-18T15:00:11.600000Z", mine: true, body: "+1"),
            message("m2", at: "2026-09-18T15:00:10.500000Z", mine: true, body: "+1"),
        ]))

        // Assert: two bubbles, not four, oldest send matched to the oldest row.
        XCTAssertEqual(timeline.rows.map(\.id), ["m1", "c1", "c2"])
        XCTAssertEqual(timeline.rows.map(\.serverId), ["m1", "m2", "m3"])
    }

    func testFailSend_marksFailed_retrySendsTheSameBody_discardRemovesIt() {
        // Arrange
        var timeline = loadedTimeline()
        timeline.beginSend(clientId: "c1", body: "hi", at: now)

        // Act + Assert: failure keeps the text on screen.
        XCTAssertEqual(timeline.failSend(clientId: "c1"), .markedFailed)
        XCTAssertEqual(timeline.rows.last?.delivery, .failed)
        XCTAssertEqual(timeline.rows.last?.body, "hi")

        // Retry puts the same bubble back to sending.
        XCTAssertEqual(timeline.retrySend(clientId: "c1"), "hi")
        XCTAssertEqual(timeline.rows.map(\.id), ["m1", "c1"])
        XCTAssertEqual(timeline.rows.last?.delivery, .sending)
        XCTAssertNil(timeline.retrySend(clientId: "c1"), "a message already sending is not sent again")

        // A sending message cannot be discarded from under its request; a failed one can.
        timeline.discardFailed(clientId: "c1")
        XCTAssertEqual(timeline.rows.count, 2)
        timeline.failSend(clientId: "c1")
        timeline.discardFailed(clientId: "c1")
        XCTAssertEqual(timeline.rows.map(\.id), ["m1"])
    }

    func testFailSend_unknownClientId_isIgnored() {
        var timeline = loadedTimeline()
        XCTAssertEqual(timeline.failSend(clientId: "nope"), .unknown)
        XCTAssertEqual(timeline.rows.map(\.id), ["m1"])
    }

    func testPendingRows_joinTheViewersRun_andStayBelowServerMessages() {
        // Arrange
        var timeline = GroupChatTimeline()
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [message("m1", at: "2026-09-18T15:00:01.000000Z", mine: true)]))
        timeline.beginSend(clientId: "c1", body: "and another thing", at: now)
        XCTAssertEqual(timeline.rows.map(\.startsRun), [true, false])

        // Act: someone else's message arrives while mine is still sending.
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [message("m2", at: "2026-09-18T15:00:12.000000Z")]))

        // Assert
        XCTAssertEqual(timeline.rows.map(\.id), ["m1", "m2", "c1"])
        XCTAssertEqual(timeline.rows.map(\.startsRun), [true, true, true])
    }
}

final class GroupChatCopyTests: XCTestCase {
    func testSendFailure_mapsStatusesAndNetworkErrors() {
        XCTAssertEqual(GroupChatCopy.sendFailure(MonacoAPIError.httpStatus(403)), "Only members of this cabal can chat here.")
        XCTAssertEqual(GroupChatCopy.sendFailure(MonacoAPIError.httpStatus(429)), "You're sending messages fast. Wait a moment and try again.")
        XCTAssertEqual(GroupChatCopy.sendFailure(MonacoAPIError.rateLimited(retryAfterSeconds: 1)), "You're sending messages fast. Try again in 1 second.")
        XCTAssertEqual(GroupChatCopy.sendFailure(MonacoAPIError.rateLimited(retryAfterSeconds: nil)), "You're sending messages fast. Wait a moment and try again.")
        XCTAssertEqual(GroupChatCopy.sendFailure(MonacoAPIError.rejected(status: 403, message: "not a group member")), "Only members of this cabal can chat here.")
        XCTAssertEqual(GroupChatCopy.sendFailure(URLError(.notConnectedToInternet)), "You're offline. Message not sent.")
        XCTAssertEqual(GroupChatCopy.sendFailure(URLError(.timedOut)), "Message not sent. Check your connection and try again.")
        XCTAssertEqual(GroupChatCopy.sendFailure(GroupChatDraft.Problem.tooLong(count: 2001)), "Messages can be up to 2000 characters.")
    }

    func testTitle_usesCabalNameWithFallback() {
        XCTAssertEqual(GroupChatCopy.title(groupName: "Weekend investors"), "Weekend investors")
        XCTAssertEqual(GroupChatCopy.title(groupName: "  "), "Cabal chat")
        XCTAssertEqual(GroupChatCopy.title(groupName: nil), "Cabal chat")
    }

    func testShowsTimeSeparator_onlyForFirstMessageAndGapsOverTenMinutes() {
        let start = Date(timeIntervalSince1970: 1_800_000_000)
        XCTAssertTrue(GroupChatCopy.showsTimeSeparator(previous: nil, current: start))
        XCTAssertFalse(GroupChatCopy.showsTimeSeparator(previous: start, current: start.addingTimeInterval(600)))
        XCTAssertTrue(GroupChatCopy.showsTimeSeparator(previous: start, current: start.addingTimeInterval(601)))
    }

    func testTimeSeparatorLabel_todayYesterdayAndOlder() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let locale = Locale(identifier: "en_US_POSIX")
        let now = ISO8601DateFormatter().date(from: "2026-09-18T15:00:00Z")!
        let today = ISO8601DateFormatter().date(from: "2026-09-18T12:40:00Z")!
        let yesterday = ISO8601DateFormatter().date(from: "2026-09-17T09:02:00Z")!
        let older = ISO8601DateFormatter().date(from: "2026-09-14T09:02:00Z")!
        let lastYear = ISO8601DateFormatter().date(from: "2025-12-30T21:15:00Z")!

        XCTAssertEqual(plain(GroupChatCopy.timeSeparatorLabel(today, now: now, calendar: calendar, locale: locale)), "Today 12:40 PM")
        XCTAssertEqual(plain(GroupChatCopy.timeSeparatorLabel(yesterday, now: now, calendar: calendar, locale: locale)), "Yesterday 9:02 AM")
        XCTAssertEqual(plain(GroupChatCopy.timeSeparatorLabel(older, now: now, calendar: calendar, locale: locale)), "Sep 14, 9:02 AM")
        XCTAssertEqual(plain(GroupChatCopy.timeSeparatorLabel(lastYear, now: now, calendar: calendar, locale: locale)), "Dec 30, 2025, 9:15 PM")
    }

    func testTimeSeparatorLabel_usesViewerTimeZone() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "America/Los_Angeles")!
        let locale = Locale(identifier: "en_US_POSIX")
        let now = ISO8601DateFormatter().date(from: "2026-09-18T20:00:00Z")!
        let stamp = ISO8601DateFormatter().date(from: "2026-09-18T16:30:00Z")!

        XCTAssertEqual(plain(GroupChatCopy.timeSeparatorLabel(stamp, now: now, calendar: calendar, locale: locale)), "Today 9:30 AM")
    }

    /// DateFormatter puts a narrow no-break space before AM/PM; compare with plain spaces.
    private func plain(_ label: String) -> String {
        label.replacingOccurrences(of: "\u{202F}", with: " ")
    }

    func testChatCopy_passesMainFlowAudit() {
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean([
            GroupChatCopy.title,
            GroupChatCopy.title(groupName: "Weekend investors"),
            GroupChatCopy.emptyState,
            GroupChatCopy.composerPlaceholder,
            GroupChatCopy.loadEarlier,
            GroupChatCopy.sending,
            GroupChatCopy.notSent,
            GroupChatCopy.tryAgain,
            GroupChatCopy.deleteUnsent,
            GroupChatCopy.sendFailure(MonacoAPIError.httpStatus(403)),
            GroupChatCopy.loadFailure(MonacoAPIError.httpStatus(403)),
        ]))
    }
}

final class GroupChatAPITests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    func testListGroupMessages_sendsLimitCursorAndBearer() async throws {
        // Arrange
        var captured: URLRequest?
        MockURLProtocol.requestHandler = { request in
            captured = request
            let body = #"{"messages":[{"id":"m1","groupId":"g1","authorId":"u1","authorName":"Ana","body":"hi","createdAt":"2026-09-18T15:04:05.000000Z","mine":true}],"nextCursor":"next"}"#
            return (Self.response(request, status: 200), Data(body.utf8))
        }
        let client = makeClient()

        // Act
        let page = try await client.listGroupMessages(groupId: "g1", before: "cur+/=", limit: 20)

        // Assert
        let url = try XCTUnwrap(captured?.url)
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        XCTAssertEqual(captured?.httpMethod, "GET")
        XCTAssertEqual(url.path, "/v1/groups/g1/messages")
        XCTAssertEqual(query.first { $0.name == "limit" }?.value, "20")
        XCTAssertEqual(query.first { $0.name == "before" }?.value, "cur+/=")
        XCTAssertEqual(captured?.value(forHTTPHeaderField: "Authorization"), "Bearer \(TestFixtures.fixtureSessionToken)")
        XCTAssertEqual(page.messages.map(\.id), ["m1"])
        XCTAssertEqual(page.nextCursor, "next")
    }

    func testListGroupMessages_withoutCursor_omitsBefore() async throws {
        // Arrange
        var captured: URLRequest?
        MockURLProtocol.requestHandler = { request in
            captured = request
            return (Self.response(request, status: 200), Data(#"{"messages":[]}"#.utf8))
        }

        // Act
        _ = try await makeClient().listGroupMessages(groupId: "g1")

        // Assert
        let query = URLComponents(url: try XCTUnwrap(captured?.url), resolvingAgainstBaseURL: false)?.queryItems ?? []
        XCTAssertNil(query.first { $0.name == "before" })
    }

    func testListGroupMessages_forbidden_throwsHTTPStatus() async {
        // Arrange
        MockURLProtocol.requestHandler = { request in
            (Self.response(request, status: 403), Data(#"{"error":"not a group member"}"#.utf8))
        }

        // Act + Assert
        do {
            _ = try await makeClient().listGroupMessages(groupId: "g1")
            XCTFail("expected throw")
        } catch {
            XCTAssertEqual(error as? MonacoAPIError, .httpStatus(403))
        }
    }

    func testListGroupMessages_malformedJSON_throwsDecodingError() async {
        // Arrange
        MockURLProtocol.requestHandler = { request in
            (Self.response(request, status: 200), Data(#"{"messages":"nope"}"#.utf8))
        }

        // Act + Assert
        do {
            _ = try await makeClient().listGroupMessages(groupId: "g1")
            XCTFail("expected throw")
        } catch {
            XCTAssertTrue(error is DecodingError, "got \(error)")
        }
    }

    func testPostGroupMessage_sendsBodyAndDecodes201() async throws {
        // Arrange
        var capturedBody: Data?
        var capturedMethod: String?
        MockURLProtocol.requestHandler = { request in
            capturedBody = Self.httpBody(from: request)
            capturedMethod = request.httpMethod
            let body = #"{"id":"m9","groupId":"g1","authorId":"u1","authorName":"Me","body":"hello","createdAt":"2026-09-18T15:04:05.000000Z","mine":true}"#
            return (Self.response(request, status: 201), Data(body.utf8))
        }

        // Act
        let message = try await makeClient().postGroupMessage(groupId: "g1", body: "hello")

        // Assert
        XCTAssertEqual(capturedMethod, "POST")
        let sent = try JSONSerialization.jsonObject(with: try XCTUnwrap(capturedBody)) as? [String: String]
        XCTAssertEqual(sent, ["body": "hello"])
        XCTAssertEqual(message.id, "m9")
        XCTAssertTrue(message.mine)
    }

    func testPostGroupMessage_rateLimited_throws429() async {
        // Arrange
        MockURLProtocol.requestHandler = { request in
            (Self.response(request, status: 429), Data(#"{"error":"too many messages, try again shortly"}"#.utf8))
        }

        // Act + Assert
        do {
            _ = try await makeClient().postGroupMessage(groupId: "g1", body: "spam")
            XCTFail("expected throw")
        } catch {
            XCTAssertEqual(error as? MonacoAPIError, .httpStatus(429))
        }
    }

    func testPostGroupMessage_networkFailure_propagatesURLError() async {
        // Arrange
        MockURLProtocol.requestHandler = { _ in throw URLError(.notConnectedToInternet) }

        // Act + Assert
        do {
            _ = try await makeClient().postGroupMessage(groupId: "g1", body: "hi")
            XCTFail("expected throw")
        } catch {
            XCTAssertEqual((error as? URLError)?.code, .notConnectedToInternet)
            XCTAssertEqual(GroupChatCopy.sendFailure(error), "You're offline. Message not sent.")
        }
    }

    // MARK: - Helpers

    private func makeClient() -> MonacoAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        let token = TestFixtures.fixtureSessionToken
        return MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: URLSession(configuration: configuration),
            accessTokenProvider: { token }
        )
    }

    private static func response(_ request: URLRequest, status: Int) -> HTTPURLResponse {
        HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
    }

    private static func httpBody(from request: URLRequest) -> Data? {
        if let body = request.httpBody { return body }
        guard let stream = request.httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        let bufferSize = 1024
        let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: bufferSize)
        defer { buffer.deallocate() }
        while stream.hasBytesAvailable {
            let read = stream.read(buffer, maxLength: bufferSize)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return data
    }
}

private func message(
    _ id: String,
    at createdAt: String,
    mine: Bool = false,
    body: String? = nil,
    author: String = "u1"
) -> GroupMessageDTO {
    GroupMessageDTO(id: id, groupId: "g1", authorId: author, authorName: "Ana", body: body ?? "text \(id)", createdAt: createdAt, mine: mine)
}
