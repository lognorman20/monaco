import XCTest
@testable import MonacoCore

final class NotificationDTOTests: XCTestCase {
    private let pageJSON = """
    {
      "notifications": [
        {
          "id": "n1", "kind": "proposal_created", "category": "proposals",
          "title": "Jordan proposed $250 of Alphabet in Sunday Investors", "body": "Voting closes in 1 day.",
          "groupId": "g1", "groupName": "Sunday Investors", "groupPictureUrl": null,
          "proposalId": "p1", "transactionId": null, "symbol": "GOOGLx",
          "readAt": null, "createdAt": "2026-09-25T14:03:00Z"
        },
        {
          "id": "n2", "kind": "funds_arrived", "category": "money",
          "title": "$500 arrived in your balance", "body": "Put it into a cabal when you're ready.",
          "groupId": null, "groupName": null, "groupPictureUrl": null,
          "proposalId": null, "transactionId": null, "symbol": null,
          "readAt": "2026-09-25T15:00:00Z", "createdAt": "2026-09-24T09:00:00.123456Z"
        }
      ],
      "unreadCount": 1,
      "nextCursor": "MjAyNi0w"
    }
    """

    func testPage_decodesRowsUnreadCountAndCursor() throws {
        // Act
        let page = try monacoISO8601JSONDecoder().decode(NotificationsPageDTO.self, from: Data(pageJSON.utf8))

        // Assert
        XCTAssertEqual(page.unreadCount, 1)
        XCTAssertEqual(page.nextCursor, "MjAyNi0w")
        XCTAssertEqual(page.notifications.count, 2)
        let first = page.notifications[0]
        XCTAssertEqual(first.kind, NotificationKind.proposalCreated)
        XCTAssertEqual(first.proposalId, "p1")
        XCTAssertEqual(first.symbol, "GOOGLx")
        XCTAssertTrue(first.isUnread)
        XCTAssertEqual(first.createdAt, ISO8601DateFormatter().date(from: "2026-09-25T14:03:00Z"))
        XCTAssertFalse(page.notifications[1].isUnread)
        XCTAssertNil(page.notifications[1].groupId)
    }

    func testPage_lastPageHasNoCursor() throws {
        let json = #"{"notifications":[],"unreadCount":0}"#
        let page = try monacoISO8601JSONDecoder().decode(NotificationsPageDTO.self, from: Data(json.utf8))
        XCTAssertNil(page.nextCursor)
        XCTAssertTrue(page.notifications.isEmpty)
    }

    func testNudgeResult_decodes() throws {
        let result = try JSONDecoder().decode(NudgeResultDTO.self, from: Data(#"{"reminded":2,"waitingOn":3}"#.utf8))
        XCTAssertEqual(result, NudgeResultDTO(reminded: 2, waitingOn: 3))
    }

    // MARK: - Destination

    func testDestination_prefersProposalThenTransactionThenCabal() {
        let at = Date()
        let cases: [(NotificationDTO, NotificationDestination)] = [
            (NotificationDTO(id: "1", kind: "trade_bought", title: "t", groupId: "g", proposalId: "p", transactionId: "x", createdAt: at), .proposal(id: "p")),
            (NotificationDTO(id: "2", kind: "bot_trade", title: "t", groupId: "g", transactionId: "x", createdAt: at), .transaction(id: "x", isSell: false)),
            (NotificationDTO(id: "3", kind: "trade_sold", title: "t", groupId: "g", transactionId: "x", createdAt: at), .transaction(id: "x", isSell: true)),
            (NotificationDTO(id: "4", kind: "chat_message", title: "t", groupId: "g", groupName: "Sunday", createdAt: at), .cabal(id: "g", name: "Sunday")),
            (NotificationDTO(id: "5", kind: "funds_arrived", title: "t", createdAt: at), .none),
            (NotificationDTO(id: "6", kind: "member_joined", title: "t", groupId: "  ", proposalId: "", createdAt: at), .none),
            (NotificationDTO(id: "7", kind: "price_alert", title: "t", symbol: "GOOGLx", createdAt: at), .stock(symbol: "GOOGLx")),
            (NotificationDTO(id: "8", kind: "price_alert", title: "t", createdAt: at), .none),
        ]
        for (notification, want) in cases {
            XCTAssertEqual(NotificationDestination.of(notification), want, notification.kind)
        }
    }

    func testDestination_fromPushPayload() {
        XCTAssertEqual(NotificationDestination.fromPush(groupId: "g", proposalId: "p"), .proposal(id: "p"))
        XCTAssertEqual(NotificationDestination.fromPush(groupId: "g", proposalId: nil), .cabal(id: "g", name: nil))
        XCTAssertEqual(NotificationDestination.fromPush(groupId: nil, proposalId: nil), .none)
    }

    // MARK: - Mark

    func testMark_stockForTradesCoinForMoneyCabalOtherwise() {
        let at = Date()
        XCTAssertEqual(NotificationMark.of(NotificationDTO(id: "1", kind: "trade_bought", title: "t", groupId: "g", symbol: "AAPLx", createdAt: at)), .stock(symbol: "AAPLx"))
        XCTAssertEqual(NotificationMark.of(NotificationDTO(id: "2", kind: "fund_credited", title: "t", groupId: "g", createdAt: at)), .money)
        XCTAssertEqual(NotificationMark.of(NotificationDTO(id: "3", kind: "proposal_created", title: "t", groupId: "g", groupName: "Sunday", symbol: "AAPLx", createdAt: at)), .cabal(groupId: "g", name: "Sunday", pictureUrl: nil))
        XCTAssertEqual(NotificationMark.of(NotificationDTO(id: "4", kind: "trade_bought", title: "t", groupId: "g", groupName: "S", createdAt: at)), .cabal(groupId: "g", name: "S", pictureUrl: nil), "a trade without a symbol falls back to the cabal")
        XCTAssertEqual(NotificationMark.of(NotificationDTO(id: "5", kind: "something_new", title: "t", createdAt: at)), .bell)
        XCTAssertEqual(NotificationMark.of(NotificationDTO(id: "6", kind: "price_alert", title: "t", symbol: "GOOGLx", createdAt: at)), .stock(symbol: "GOOGLx"), "an alert wears the stock it is about")
    }

    // MARK: - Grouping

    func testSections_todayThenEarlierNewestFirst() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "America/New_York")!
        let now = ISO8601DateFormatter().date(from: "2026-09-25T18:00:00Z")! // 2pm in New York
        let items = [
            NotificationDTO(id: "a", kind: "k", title: "this morning", createdAt: now.addingTimeInterval(-6 * 3600)),
            NotificationDTO(id: "b", kind: "k", title: "last night", createdAt: now.addingTimeInterval(-16 * 3600)),
            NotificationDTO(id: "c", kind: "k", title: "just now", createdAt: now.addingTimeInterval(-60)),
        ]

        let sections = InboxGrouping.sections(items, now: now, calendar: calendar)

        XCTAssertEqual(sections.map(\.title), ["Today", "Earlier"])
        XCTAssertEqual(sections[0].items.map(\.title), ["just now", "this morning"])
        XCTAssertEqual(sections[1].items.map(\.title), ["last night"])
    }

    func testSections_emptyGroupsAreLeftOut() {
        let now = Date()
        XCTAssertEqual(InboxGrouping.sections([], now: now), [])
        let old = [NotificationDTO(id: "a", kind: "k", title: "t", createdAt: now.addingTimeInterval(-10 * 86_400))]
        XCTAssertEqual(InboxGrouping.sections(old, now: now).map(\.title), ["Earlier"])
    }

    func testMerge_freshFirstPageKeepsOlderPagesAndDropsDuplicates() {
        let now = Date()
        let current = (0..<5).map { i in
            NotificationDTO(id: "old\(i)", kind: "k", title: "\(i)", createdAt: now.addingTimeInterval(-Double(i) * 60))
        }
        let fresh = [
            NotificationDTO(id: "new", kind: "k", title: "new", createdAt: now.addingTimeInterval(30)),
            current[0].markedRead(at: now),
            current[1],
        ]

        let merged = InboxGrouping.merge(fresh: fresh, over: current)

        XCTAssertEqual(merged.map(\.id), ["new", "old0", "old1", "old2", "old3", "old4"])
        XCTAssertFalse(merged[1].isUnread, "the fresh copy wins")
    }

    // MARK: - Age, badge

    func testAgeLabel() {
        let now = ISO8601DateFormatter().date(from: "2026-09-25T18:00:00Z")!
        XCTAssertEqual(NotificationAge.label(for: now.addingTimeInterval(-20), now: now), "now")
        XCTAssertEqual(NotificationAge.label(for: now.addingTimeInterval(-12 * 60), now: now), "12m")
        XCTAssertEqual(NotificationAge.label(for: now.addingTimeInterval(-3 * 3600 - 5), now: now), "3h")
        XCTAssertEqual(NotificationAge.label(for: now.addingTimeInterval(-2 * 86_400), now: now), "2d")
        XCTAssertEqual(NotificationAge.label(for: now.addingTimeInterval(30), now: now), "now", "clock skew never reads as the future")
        XCTAssertEqual(NotificationAge.accessibilityLabel(for: now.addingTimeInterval(-60), now: now), "1 minute ago")
        XCTAssertEqual(NotificationAge.accessibilityLabel(for: now.addingTimeInterval(-7200), now: now), "2 hours ago")
    }

    func testBadge() {
        XCTAssertNil(InboxBadge.text(unread: 0))
        XCTAssertEqual(InboxBadge.text(unread: 7), "7")
        XCTAssertEqual(InboxBadge.text(unread: 99), "99")
        XCTAssertEqual(InboxBadge.text(unread: 140), "99+")
    }

    func testDeviceTokenHex() {
        XCTAssertEqual(DeviceTokenFormatter.hex(Data([0x00, 0xab, 0x10, 0xff])), "00ab10ff")
    }

    // MARK: - Remind rule

    private func proposal(status: String = "open", proposerId: String = "jordan", yes: Int, no: Int, eligible: Int, votes: [ProposalVoteDTO] = [], expiresIn: TimeInterval = 3600) -> ProposalDTO {
        ProposalDTO(
            id: "p", symbol: "AAPLx", status: status, proposerId: proposerId,
            expiresAt: ISO8601DateFormatter().string(from: Date().addingTimeInterval(expiresIn)),
            votes: votes,
            voteSummary: ProposalVoteSummaryDTO(yesCount: yes, noCount: no, eligibleCount: eligible, threshold: "majority")
        )
    }

    func testRemindRule() {
        let priyaVoted = [ProposalVoteDTO(voterId: "priya", displayName: "Priya", choice: "yes")]
        XCTAssertTrue(ProposalNudgeRule.showsRemind(proposal: proposal(yes: 1, no: 0, eligible: 4, votes: priyaVoted), viewerId: "priya", viewerChoice: nil),
                      "voted, three others waiting")
        XCTAssertTrue(ProposalNudgeRule.showsRemind(proposal: proposal(yes: 0, no: 0, eligible: 3), viewerId: "jordan", viewerChoice: nil),
                      "the proposer may remind before voting")
        XCTAssertFalse(ProposalNudgeRule.showsRemind(proposal: proposal(yes: 0, no: 0, eligible: 3), viewerId: "sam", viewerChoice: nil),
                       "a member who has not voted votes first")
        XCTAssertFalse(ProposalNudgeRule.showsRemind(proposal: proposal(yes: 0, no: 0, eligible: 1), viewerId: "jordan", viewerChoice: nil),
                       "only the proposer is left to vote")
        XCTAssertFalse(ProposalNudgeRule.showsRemind(proposal: proposal(yes: 2, no: 1, eligible: 3, votes: priyaVoted), viewerId: "priya", viewerChoice: "yes"),
                       "everyone voted")
        XCTAssertTrue(ProposalNudgeRule.showsRemind(proposal: proposal(yes: 1, no: 0, eligible: 3), viewerId: "sam", viewerChoice: "yes"),
                      "the ledger's record of the viewer's ballot counts")
        XCTAssertFalse(ProposalNudgeRule.showsRemind(proposal: proposal(status: "passed", yes: 1, no: 0, eligible: 3, votes: priyaVoted), viewerId: "priya", viewerChoice: nil),
                       "closed")
        XCTAssertFalse(ProposalNudgeRule.showsRemind(proposal: proposal(yes: 1, no: 0, eligible: 3, votes: priyaVoted, expiresIn: -60), viewerId: "priya", viewerChoice: nil),
                       "past its deadline")
    }

    // MARK: - Copy

    func testReminderToast() {
        XCTAssertEqual(InboxCopy.reminded(NudgeResultDTO(reminded: 2, waitingOn: 2)), "Reminded 2 members")
        XCTAssertEqual(InboxCopy.reminded(NudgeResultDTO(reminded: 1, waitingOn: 3)), "Reminded 1 member")
        XCTAssertEqual(InboxCopy.reminded(NudgeResultDTO(reminded: 0, waitingOn: 0)), "Everyone has voted")
        XCTAssertEqual(InboxCopy.reminded(NudgeResultDTO(reminded: 0, waitingOn: 2)), "They've switched reminders off")
        XCTAssertEqual(InboxCopy.remindError(status: 429), InboxCopy.remindTooSoon)
        XCTAssertEqual(InboxCopy.remindError(status: 403), InboxCopy.remindVoteFirst)
        XCTAssertEqual(InboxCopy.remindError(status: 409), InboxCopy.remindClosed)
        XCTAssertEqual(InboxCopy.remindError(status: nil), InboxCopy.remindFailed)
    }

    func testInboxCopy_passesTheMainFlowAudit() {
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean(InboxCopy.all), "\(InboxCopy.all)")
    }
}
