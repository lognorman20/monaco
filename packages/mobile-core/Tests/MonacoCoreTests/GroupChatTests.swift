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
        XCTAssertEqual(added.map(\.id), ["m2", "m3"])
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
        XCTAssertEqual(added.map(\.id), ["m2"])
        XCTAssertTrue(again.isEmpty)
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

    func testAppendSent_thenPollReturnsSameMessage_noDuplicate() {
        // Arrange
        var timeline = GroupChatTimeline()
        timeline.mergeNewest(GroupMessagesPageDTO(messages: []))
        let sent = message("mine-1", at: "2026-09-18T15:00:01.000000Z", mine: true)

        // Act
        timeline.appendSent(sent)
        let added = timeline.mergeNewest(GroupMessagesPageDTO(messages: [sent]))

        // Assert
        XCTAssertTrue(added.isEmpty)
        XCTAssertEqual(timeline.messages, [sent])
    }

    /// Rows now reuse the `Date` already parsed for a message instead of re-parsing the whole
    /// loaded thread on every merge. The dates, and everything derived from them, have to come
    /// out identical to a thread built in one go.
    func testRows_reusedDatesMatchAThreadBuiltInOnePass() {
        // Arrange: three pages, so most rows are carried across two merges.
        var merged = GroupChatTimeline()
        merged.mergeNewest(GroupMessagesPageDTO(messages: [
            message("m3", at: "2026-09-18T15:00:03.000000Z"),
            message("m2", at: "2026-09-18T15:00:02.000000Z"),
            message("m1", at: "2026-09-18T15:00:01.000000Z"),
        ]))
        merged.mergeNewest(GroupMessagesPageDTO(messages: [
            message("m4", at: "2026-09-18T15:30:04.000000Z"),
        ]))
        merged.mergeNewest(GroupMessagesPageDTO(messages: [
            message("m5", at: "2026-09-18T15:30:05.000000Z"),
        ]))

        var atOnce = GroupChatTimeline()
        atOnce.mergeNewest(GroupMessagesPageDTO(messages: [
            message("m5", at: "2026-09-18T15:30:05.000000Z"),
            message("m4", at: "2026-09-18T15:30:04.000000Z"),
            message("m3", at: "2026-09-18T15:00:03.000000Z"),
            message("m2", at: "2026-09-18T15:00:02.000000Z"),
            message("m1", at: "2026-09-18T15:00:01.000000Z"),
        ]))

        // Assert: dates, separators and run edges all survive being carried.
        XCTAssertEqual(merged.rows, atOnce.rows)
        XCTAssertEqual(merged.rows.map(\.date), atOnce.rows.map(\.date))
        XCTAssertNotNil(merged.rows.first?.date, "the fixture stamps should parse at all")
    }

    /// A stamp that will not parse is carried as nil rather than re-parsed on every merge in
    /// the hope of a different answer.
    func testRows_anUnparseableStampStaysNilAcrossMerges() {
        // Arrange
        var timeline = GroupChatTimeline()
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [message("bad", at: "not a date")]))
        XCTAssertNil(timeline.rows.first?.date)

        // Act
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [
            message("m2", at: "2026-09-18T15:00:02.000000Z"),
            message("bad", at: "not a date"),
        ]))

        // Assert
        XCTAssertEqual(timeline.rows.count, 2)
        XCTAssertNil(timeline.rows.first { $0.id == "bad" }?.date)
        XCTAssertNotNil(timeline.rows.first { $0.id == "m2" }?.date)
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
        XCTAssertEqual(GroupChatCopy.sendFailure(GroupChatDraft.Problem.tooLong(count: 2001)), "Messages can be up to 2000 characters.")
    }

    /// A failure the device can only have seen after the request went out must not promise the
    /// message didn't arrive: sending has no idempotency key, so acting on that promise
    /// double-posts, and chat has no delete.
    func testSendFailure_doesNotClaimNotSentWhenTheRequestMayHaveLanded() {
        for code in [URLError.Code.timedOut, .networkConnectionLost, .cannotParseResponse, .badServerResponse] {
            XCTAssertFalse(
                FlowErrorInput.neverSentURLErrorCodes.contains(code),
                "URLError.\(code) interrupts a request already in flight"
            )
            XCTAssertEqual(GroupChatCopy.sendFailure(URLError(code)), GroupChatCopy.sendUnconfirmed)
        }
        XCTAssertEqual(GroupChatCopy.sendFailure(MonacoAPIError.httpStatus(500)), GroupChatCopy.sendUnconfirmed)
        XCTAssertEqual(GroupChatCopy.sendFailure(MonacoAPIError.httpStatus(504)), GroupChatCopy.sendUnconfirmed)
        XCTAssertEqual(GroupChatCopy.sendFailure(MonacoAPIError.invalidResponse), GroupChatCopy.sendUnconfirmed)
        // A 201 we can't decode is a message the API stored.
        XCTAssertEqual(GroupChatCopy.sendFailure(DecodingError.dataCorrupted(.init(codingPath: [], debugDescription: ""))), GroupChatCopy.sendUnconfirmed)
        XCTAssertFalse(GroupChatCopy.sendUnconfirmed.localizedCaseInsensitiveContains("not sent"))

        // Nothing left the device, so these are promises we can keep.
        XCTAssertEqual(GroupChatCopy.sendFailure(URLError(.notConnectedToInternet)), "You're offline. Message not sent.")
        for code in FlowErrorInput.neverSentURLErrorCodes where code != .notConnectedToInternet {
            XCTAssertEqual(
                GroupChatCopy.sendFailure(URLError(code)),
                "Message not sent. Check your connection and try again.",
                "URLError.\(code) is raised before any byte reaches the API"
            )
        }
        // A rejection is the API declining the message, not losing it.
        XCTAssertEqual(
            GroupChatCopy.sendFailure(MonacoAPIError.httpStatus(400)),
            "That message couldn't be sent. Check the text and try again."
        )
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

    func testListGroupMessages_forbidden_keepsTheStatusAndTheServerReason() async {
        // Arrange
        MockURLProtocol.requestHandler = { request in
            (Self.response(request, status: 403), Data(#"{"error":"not a group member"}"#.utf8))
        }

        // Act + Assert
        do {
            _ = try await makeClient().listGroupMessages(groupId: "g1")
            XCTFail("expected throw")
        } catch {
            XCTAssertEqual(error as? MonacoAPIError, .rejected(status: 403, message: "not a group member"))
            XCTAssertEqual((error as? MonacoAPIError)?.statusCode, 403)
            XCTAssertEqual(GroupChatCopy.loadFailure(error), "You're no longer in this cabal, so its chat is closed to you.")
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

    func testPostGroupMessage_rateLimited_keepsTheServersRetryAfter() async {
        // Arrange
        MockURLProtocol.requestHandler = { request in
            (
                Self.response(request, status: 429, headers: ["Retry-After": "60"]),
                Data(#"{"error":"too many messages, try again shortly"}"#.utf8)
            )
        }

        // Act + Assert
        do {
            _ = try await makeClient().postGroupMessage(groupId: "g1", body: "spam")
            XCTFail("expected throw")
        } catch {
            // Without the header the member only ever got "Wait a moment and try again".
            XCTAssertEqual(error as? MonacoAPIError, .rateLimited(retryAfterSeconds: 60))
            XCTAssertEqual(
                GroupChatCopy.sendFailure(error),
                "You're sending messages fast. Try again in 60 seconds."
            )
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

    private static func response(
        _ request: URLRequest,
        status: Int,
        headers: [String: String] = [:]
    ) -> HTTPURLResponse {
        HTTPURLResponse(
            url: request.url!,
            statusCode: status,
            httpVersion: nil,
            headerFields: ["Content-Type": "application/json"].merging(headers) { _, new in new }
        )!
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

final class GroupChatRowTests: XCTestCase {
    /// The thread used to work all of this out per visible row on every body pass, parsing
    /// each ISO stamp up to four times. It is computed once, at merge, and must still be right.
    func testRows_markSeparatorsAndRunsFromOnePass() {
        // Arrange: Ana twice, then Leo twice with a 19-minute gap before his second.
        var timeline = GroupChatTimeline()

        // Act
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [
            authored("m4", by: "leo", at: "2026-09-18T15:20:00.000000Z"),
            authored("m3", by: "leo", at: "2026-09-18T15:01:00.000000Z"),
            authored("m2", by: "ana", at: "2026-09-18T15:00:30.000000Z"),
            authored("m1", by: "ana", at: "2026-09-18T15:00:00.000000Z"),
        ]))

        // Assert
        XCTAssertEqual(timeline.rows.map(\.id), ["m1", "m2", "m3", "m4"])
        XCTAssertEqual(timeline.rows.map(\.showsTimeSeparator), [true, false, false, true])
        XCTAssertEqual(timeline.rows.map(\.startsRun), [true, false, true, true])
        XCTAssertEqual(timeline.rows.map(\.endsRun), [false, true, true, true])
        XCTAssertEqual(
            timeline.rows.map(\.date),
            timeline.messages.map(\.createdAtDate),
            "each row must carry the stamp the view would otherwise re-parse"
        )
    }

    func testRow_separatorLabelIsOnlyOfferedWhereOneBelongs() {
        // Arrange
        var timeline = GroupChatTimeline()
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [
            authored("m2", by: "ana", at: "2026-09-18T15:00:30.000000Z"),
            authored("m1", by: "ana", at: "2026-09-18T15:00:00.000000Z"),
        ]))
        let now = ISO8601DateFormatter().date(from: "2026-09-18T15:30:00Z")!

        // Act + Assert
        XCTAssertNotNil(timeline.rows[0].timeSeparatorLabel(now: now))
        XCTAssertNil(timeline.rows[1].timeSeparatorLabel(now: now))
    }

    func testRows_unparseableStampGetsNoSeparator() {
        // Arrange
        var timeline = GroupChatTimeline()

        // Act
        timeline.mergeNewest(GroupMessagesPageDTO(messages: [authored("m1", by: "ana", at: "yesterday")]))

        // Assert
        XCTAssertNil(timeline.rows[0].date)
        XCTAssertFalse(timeline.rows[0].showsTimeSeparator)
        XCTAssertNil(timeline.rows[0].timeSeparatorLabel())
    }

    /// A poll that brings nothing new must not re-sort or rebuild anything: on an idle thread
    /// that is a tick every four seconds that would otherwise invalidate the whole list.
    func testQuietTick_changesNothing() {
        // Arrange
        var timeline = GroupChatTimeline()
        let page = GroupMessagesPageDTO(messages: [authored("m1", by: "ana", at: "2026-09-18T15:00:00.000000Z")])
        timeline.mergeNewest(page)
        let before = timeline

        // Act
        let added = timeline.mergeNewest(page)

        // Assert
        XCTAssertTrue(added.isEmpty)
        XCTAssertEqual(timeline, before)
    }

    private func authored(_ id: String, by author: String, at createdAt: String) -> GroupMessageDTO {
        GroupMessageDTO(
            id: id,
            groupId: "g1",
            authorId: author,
            authorName: author.capitalized,
            body: "text \(id)",
            createdAt: createdAt,
            mine: false
        )
    }
}

final class GroupChatAutoScrollTests: XCTestCase {
    func testOwnMessage_alwaysScrolls() {
        XCTAssertTrue(GroupChatTimeline.shouldAutoScroll(added: [mine("m1")], isFollowingThread: false))
        XCTAssertTrue(GroupChatTimeline.shouldAutoScroll(added: [mine("m1")], isFollowingThread: true))
    }

    /// The bug: someone else posting yanked a member reading backlog down to the newest
    /// message, once per arrival, on a four-second poll.
    func testSomeoneElsesMessage_onlyScrollsWhenTheThreadIsFollowing() {
        let arrival = [notMine("m1")]
        XCTAssertFalse(GroupChatTimeline.shouldAutoScroll(added: arrival, isFollowingThread: false))
        XCTAssertTrue(GroupChatTimeline.shouldAutoScroll(added: arrival, isFollowingThread: true))
    }

    func testMixedBatchContainingMine_scrollsEvenWhenScrolledUp() {
        XCTAssertTrue(
            GroupChatTimeline.shouldAutoScroll(added: [notMine("m1"), mine("m2")], isFollowingThread: false)
        )
    }

    func testNothingAdded_neverScrolls() {
        XCTAssertFalse(GroupChatTimeline.shouldAutoScroll(added: [], isFollowingThread: true))
    }

    private func mine(_ id: String) -> GroupMessageDTO { message(id, at: "2026-09-18T15:00:00.000000Z", mine: true) }
    private func notMine(_ id: String) -> GroupMessageDTO { message(id, at: "2026-09-18T15:00:00.000000Z") }
}

final class GroupChatFailureCopyTests: XCTestCase {
    /// The first-load state has no scroll view, so it must not tell anyone to pull it.
    func testLoadFailure_doesNotAskForAGestureThatIsNotThere() {
        let copy = GroupChatCopy.loadFailure(URLError(.notConnectedToInternet))
        XCTAssertEqual(copy, "Couldn't load messages.")
        XCTAssertFalse(copy.lowercased().contains("pull"))
    }

    func testRefreshFailure_canAskForAPullBecauseTheThreadIsOnScreen() {
        XCTAssertEqual(
            GroupChatCopy.refreshFailure(URLError(.timedOut)),
            "Couldn't refresh messages. Pull down to try again."
        )
    }

    func testEarlierFailure_namesTheButtonItCameFrom() {
        XCTAssertEqual(
            GroupChatCopy.earlierFailure(URLError(.timedOut)),
            "Couldn't load earlier messages. Try again."
        )
    }

    func testChatClosed_onlyForRemovedMemberOrMissingCabal() {
        XCTAssertEqual(
            GroupChatCopy.chatClosed(MonacoAPIError.httpStatus(403)),
            "You're no longer in this cabal, so its chat is closed to you."
        )
        XCTAssertEqual(
            GroupChatCopy.chatClosed(MonacoAPIError.rejected(status: 404, message: "group not found")),
            "This cabal no longer exists."
        )
        XCTAssertNil(GroupChatCopy.chatClosed(MonacoAPIError.httpStatus(500)))
        XCTAssertNil(GroupChatCopy.chatClosed(MonacoAPIError.httpStatus(401)))
        XCTAssertNil(GroupChatCopy.chatClosed(URLError(.notConnectedToInternet)))
    }

    /// The composer branches on this, so it has to line up with the sentence exactly: a send
    /// that may have landed must not hand the text back, or the warning is one tap from the
    /// duplicate it exists to prevent.
    func testIsSendUnconfirmed_matchesTheSentenceItIsDerivedFrom() {
        // Could only have failed after the request went out.
        XCTAssertTrue(GroupChatCopy.isSendUnconfirmed(MonacoAPIError.httpStatus(500)))
        XCTAssertTrue(GroupChatCopy.isSendUnconfirmed(MonacoAPIError.invalidResponse))
        XCTAssertTrue(GroupChatCopy.isSendUnconfirmed(URLError(.timedOut)))

        // Raised before a byte left the device, or a refusal the API made on purpose.
        XCTAssertFalse(GroupChatCopy.isSendUnconfirmed(URLError(.notConnectedToInternet)))
        XCTAssertFalse(GroupChatCopy.isSendUnconfirmed(MonacoAPIError.httpStatus(403)))
        XCTAssertFalse(GroupChatCopy.isSendUnconfirmed(MonacoAPIError.httpStatus(400)))
        XCTAssertFalse(GroupChatCopy.isSendUnconfirmed(GroupChatDraft.Problem.empty))
    }

    /// The messages route answers 404 for a missing *user* as well as a missing cabal, and a
    /// membership read served by a lagging replica raises exactly that for a member who is
    /// still in their cabal. Telling them the cabal was deleted is the wrong sentence and,
    /// worse, the one that parks the screen.
    func testChatClosed_aUserNotFound404IsNotADeletedCabal() {
        XCTAssertNil(
            GroupChatCopy.chatClosed(MonacoAPIError.rejected(status: 404, message: "user not found"))
        )
        XCTAssertEqual(
            GroupChatCopy.chatClosed(MonacoAPIError.rejected(status: 404, message: "group not found")),
            "This cabal no longer exists."
        )
    }
}

final class GroupChatClosureTrackerTests: XCTestCase {
    private let removed = MonacoAPIError.httpStatus(403)

    func testStartsOpen() {
        let tracker = GroupChatClosureTracker()
        XCTAssertFalse(tracker.isClosed)
        XCTAssertNil(tracker.message)
    }

    /// The regression: one background tick closed the thread for the life of the view. A blip
    /// on the membership read has to stop being able to do that.
    func testASingleClosedPollDoesNotCloseTheThread() {
        var tracker = GroupChatClosureTracker()
        tracker.pollFailed(removed)
        XCTAssertFalse(tracker.isClosed, "one tick is not evidence that a member was removed")
    }

    func testTheThreadClosesOnceThePollKeepsSayingSo() {
        var tracker = GroupChatClosureTracker()
        for _ in 0..<GroupChatClosureTracker.pollsBeforeClosing {
            tracker.pollFailed(removed)
        }
        XCTAssertEqual(tracker.message, "You're no longer in this cabal, so its chat is closed to you.")
    }

    /// A blip in the middle of a run is the whole point: the server has to hold the story.
    func testOneGoodPollResetsTheRun() {
        var tracker = GroupChatClosureTracker()
        tracker.pollFailed(removed)
        tracker.pollFailed(removed)
        tracker.succeeded()
        tracker.pollFailed(removed)
        tracker.pollFailed(removed)
        XCTAssertFalse(tracker.isClosed)
    }

    /// Failures that are not about being shut out must not accumulate towards closing either.
    func testAnUnrelatedFailureResetsTheRun() {
        var tracker = GroupChatClosureTracker()
        tracker.pollFailed(removed)
        tracker.pollFailed(removed)
        tracker.pollFailed(URLError(.timedOut))
        tracker.pollFailed(removed)
        XCTAssertFalse(tracker.isClosed)
    }

    /// The member asked and is waiting on the answer, so it is not held back for corroboration.
    func testAMemberLoadClosesImmediately() {
        var tracker = GroupChatClosureTracker()
        tracker.memberLoadFailed(removed)
        XCTAssertTrue(tracker.isClosed)
    }

    func testAMemberLoadIgnoresFailuresThatAreNotAClosedThread() {
        var tracker = GroupChatClosureTracker()
        tracker.memberLoadFailed(URLError(.timedOut))
        XCTAssertFalse(tracker.isClosed)
    }

    /// The other half of the one-way street: a closed thread has to be able to reopen, or a
    /// transient 404 is permanent whatever the retry button does.
    func testAPageReopensAClosedThread() {
        var tracker = GroupChatClosureTracker()
        tracker.memberLoadFailed(removed)
        XCTAssertTrue(tracker.isClosed)

        tracker.succeeded()
        XCTAssertFalse(tracker.isClosed)
        XCTAssertNil(tracker.message)
    }

    /// Reopening clears the run too, so a thread that recovered does not close on the next
    /// single blip because of polls that failed before it came back.
    func testReopeningAlsoClearsTheRun() {
        var tracker = GroupChatClosureTracker()
        tracker.pollFailed(removed)
        tracker.pollFailed(removed)
        tracker.memberLoadFailed(removed)
        tracker.succeeded()

        tracker.pollFailed(removed)
        XCTAssertFalse(tracker.isClosed)
    }

    func testClosedCopy_reachesEveryFailureSurface() {
        let closed = "This cabal no longer exists."
        XCTAssertEqual(GroupChatCopy.loadFailure(MonacoAPIError.httpStatus(404)), closed)
        XCTAssertEqual(GroupChatCopy.refreshFailure(MonacoAPIError.httpStatus(404)), closed)
        XCTAssertEqual(GroupChatCopy.earlierFailure(MonacoAPIError.httpStatus(404)), closed)
    }

    func testNewMessagesPill_singularAndPlural() {
        XCTAssertEqual(GroupChatCopy.newMessagesPill(count: 1), "1 new message")
        XCTAssertEqual(GroupChatCopy.newMessagesPill(count: 4), "4 new messages")
    }

    func testFailureCopy_passesMainFlowAudit() {
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean([
            GroupChatCopy.loadFailure(URLError(.timedOut)),
            GroupChatCopy.refreshFailure(URLError(.timedOut)),
            GroupChatCopy.earlierFailure(URLError(.timedOut)),
            GroupChatCopy.chatClosed(MonacoAPIError.httpStatus(403)) ?? "",
            GroupChatCopy.chatClosed(MonacoAPIError.httpStatus(404)) ?? "",
            GroupChatCopy.newMessagesPill(count: 3),
            // The newest member-facing sentence in chat, and the one a member has to act on.
            GroupChatCopy.sendUnconfirmed,
            GroupChatCopy.sendFailure(MonacoAPIError.rateLimited(retryAfterSeconds: 30)),
            GroupChatCopy.sendFailure(MonacoAPIError.httpStatus(401)),
            GroupChatCopy.sendFailure(URLError(.notConnectedToInternet)),
        ]))
    }
}

private func message(_ id: String, at createdAt: String, mine: Bool = false) -> GroupMessageDTO {
    GroupMessageDTO(id: id, groupId: "g1", authorId: "u1", authorName: "Ana", body: "text \(id)", createdAt: createdAt, mine: mine)
}
