import XCTest
@testable import MonacoCore

final class ProposalFeedFormatterTests: XCTestCase {
    private let now = ISO8601DateFormatter().date(from: "2026-09-18T12:00:00Z")!

    func testAmount_formatsMicrosAsDollars() {
        // Arrange
        let micros = "1250000000"

        // Act
        let label = ProposalAmountFormatter.dollars(fromMicros: micros)

        // Assert
        XCTAssertEqual(label, "$1,250.00")
    }

    func testAmount_unparseable_returnsRawValue() {
        // Arrange
        let raw = "not-a-number"

        // Act
        let label = ProposalAmountFormatter.dollars(fromMicros: raw)

        // Assert
        XCTAssertEqual(label, raw)
    }

    func testVoteProgress_majorityCaptionAndDots() {
        // Arrange
        let summary = ProposalVoteSummaryDTO(yesCount: 2, noCount: 1, eligibleCount: 6, threshold: "majority")

        // Act
        let progress = ProposalVoteProgress(summary: summary)

        // Assert
        XCTAssertEqual(progress.caption, "3 of 6 voted · 4 yes to pass")
        XCTAssertEqual(progress.closedCaption, "2 yes · 1 no")
        XCTAssertEqual(progress.dots, [.yes, .yes, .no, .pending, .pending, .pending])
    }

    func testVoteProgress_yesNeeded_matchesBackendPassRule() {
        // Majority passes when yes > no + remaining, i.e. floor(n/2) + 1; unanimous needs everyone.
        func needed(_ n: Int, _ threshold: String) -> Int {
            ProposalVoteProgress(summary: ProposalVoteSummaryDTO(yesCount: 0, noCount: 0, eligibleCount: n, threshold: threshold)).yesNeeded
        }
        XCTAssertEqual(needed(1, "majority"), 1)
        XCTAssertEqual(needed(2, "majority"), 2)
        XCTAssertEqual(needed(5, "majority"), 3)
        XCTAssertEqual(needed(6, "majority"), 4)
        XCTAssertEqual(needed(5, "unanimous"), 5)
        XCTAssertEqual(needed(5, "Unanimous"), 5)
    }

    func testVoteProgress_demoCase_oneOfTwoVoted() {
        // Arrange: golden path. Account B voted yes; the viewer's yes passes it.
        let summary = ProposalVoteSummaryDTO(yesCount: 1, noCount: 0, eligibleCount: 2, threshold: "majority")

        // Act / Assert
        XCTAssertEqual(ProposalVoteProgress(summary: summary).caption, "1 of 2 voted · 2 yes to pass")
    }

    func testVoteProgress_votesExceedEligible_countsBallots() {
        // Arrange: a voter left the cabal after voting.
        let summary = ProposalVoteSummaryDTO(yesCount: 3, noCount: 1, eligibleCount: 3, threshold: "majority")

        // Act
        let progress = ProposalVoteProgress(summary: summary)

        // Assert
        XCTAssertEqual(progress.eligibleCount, 4)
        XCTAssertEqual(progress.pendingCount, 0)
        XCTAssertEqual(progress.dots?.count, 4)
    }

    func testVoteProgress_zeroEligible_hasNoDotsAndPlainCaption() {
        // Arrange
        let summary = ProposalVoteSummaryDTO(yesCount: 0, noCount: 0, eligibleCount: 0, threshold: "majority")

        // Act
        let progress = ProposalVoteProgress(summary: summary)

        // Assert
        XCTAssertNil(progress.dots)
        XCTAssertEqual(progress.caption, "No one can vote on this yet")
    }

    func testVoteProgress_moreThanTwelveVoters_captionOnly() {
        // Arrange
        let summary = ProposalVoteSummaryDTO(yesCount: 4, noCount: 2, eligibleCount: 13, threshold: "majority")

        // Act
        let progress = ProposalVoteProgress(summary: summary)

        // Assert
        XCTAssertNil(progress.dots)
        XCTAssertEqual(progress.caption, "6 of 13 voted · 7 yes to pass")
        XCTAssertEqual(ProposalVoteProgress(summary: ProposalVoteSummaryDTO(yesCount: 0, noCount: 0, eligibleCount: 12, threshold: "majority")).dots?.count, 12)
    }

    func testClosesLabel_hoursLeft() {
        // Act
        let label = ProposalTimeFormatter.closesLabel(expiresAt: "2026-09-18T17:20:00Z", now: now)

        // Assert
        XCTAssertEqual(label, "Closes in 5h")
    }

    func testClosesLabel_buckets() {
        XCTAssertEqual(ProposalTimeFormatter.closesLabel(expiresAt: "2026-09-19T11:59:00Z", now: now), "Closes in 23h")
        XCTAssertEqual(ProposalTimeFormatter.closesLabel(expiresAt: "2026-09-19T20:00:00Z", now: now), "Closes in 32h")
        XCTAssertEqual(ProposalTimeFormatter.closesLabel(expiresAt: "2026-09-21T13:00:00Z", now: now), "Closes in 3d")
        XCTAssertEqual(ProposalTimeFormatter.closesLabel(expiresAt: "2026-09-18T12:12:30Z", now: now), "Closes in 12m")
        XCTAssertEqual(ProposalTimeFormatter.closesLabel(expiresAt: "2026-09-18T12:00:20Z", now: now), "Closes in 1m")
    }

    func testClosesSoon_onlyInTheLastHour() {
        XCTAssertTrue(ProposalTimeFormatter.closesSoon(expiresAt: "2026-09-18T12:40:00Z", now: now))
        XCTAssertFalse(ProposalTimeFormatter.closesSoon(expiresAt: "2026-09-18T13:00:01Z", now: now))
        XCTAssertFalse(ProposalTimeFormatter.closesSoon(expiresAt: "2026-09-18T11:00:00Z", now: now))
        XCTAssertFalse(ProposalTimeFormatter.closesSoon(expiresAt: "garbage", now: now))
    }

    func testClosesLabel_pastExpiry_saysClosed() {
        // Act
        let label = ProposalTimeFormatter.closesLabel(expiresAt: "2026-09-18T11:00:00Z", now: now)

        // Assert
        XCTAssertEqual(label, "Voting closed")
    }

    func testClosesLabel_garbage_isNil() {
        // Act
        let label = ProposalTimeFormatter.closesLabel(expiresAt: "tomorrow", now: now)

        // Assert
        XCTAssertNil(label)
    }

    func testAgeLabel_compactBuckets() {
        // Act
        let labels = [
            ProposalTimeFormatter.ageLabel("2026-09-18T11:59:30Z", now: now),
            ProposalTimeFormatter.ageLabel("2026-09-18T11:48:00Z", now: now),
            ProposalTimeFormatter.ageLabel("2026-09-18T09:00:00Z", now: now),
            ProposalTimeFormatter.ageLabel("2026-09-16T12:00:00Z", now: now),
        ]

        // Assert
        XCTAssertEqual(labels, ["now", "12m", "3h", "2d"])
    }

    func testProposalFeedCopy_passesMainFlowCopyAudit() {
        // Act
        let clean = MainFlowCopyAudit.stringsAreClean(ProposalFeedCopy.auditedStrings)

        // Assert
        XCTAssertTrue(clean)
    }

    func testProposeFlowCopy_passesMainFlowCopyAudit() {
        // Assert: every string passes, and none leaks jargon the plan bans from primary copy.
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean(ProposeFlowCopy.auditedStrings))
        let jargon = ["treasury", "route", "quote", "usdc", "bps", "units", "stake", "http", "api", "!"]
        for string in ProposeFlowCopy.auditedStrings + ProposalFeedCopy.auditedStrings {
            for term in jargon {
                XCTAssertFalse(string.lowercased().contains(term), "\"\(string)\" contains \"\(term)\"")
            }
        }
    }

    func testVoteCopy_plainYesNo() {
        XCTAssertEqual(ProposalFeedCopy.voteYes, "Yes")
        XCTAssertEqual(ProposalFeedCopy.voteNo, "No")
        XCTAssertEqual(ProposalFeedCopy.viewerVoted("yes"), "You voted yes")
        XCTAssertEqual(ProposalFeedCopy.viewerVoted("NO"), "You voted no")
    }

    func testTitle_tradeUsesCompanyNameThenTicker() {
        XCTAssertEqual(ProposalFeedCopy.title(for: ProposalDTO(id: "p", symbol: "AAPLx", status: "open")), "Apple")
        XCTAssertEqual(ProposalFeedCopy.title(for: ProposalDTO(id: "p", symbol: "NVDAx", status: "open", kind: "sell")), "Nvidia")
        XCTAssertEqual(ProposalFeedCopy.title(for: ProposalDTO(id: "p", symbol: "ZZZZx", status: "open")), "ZZZZ")
    }

    func testSubtitle_tradeShowsTickerAndSide() {
        XCTAssertEqual(ProposalFeedCopy.subtitle(for: ProposalDTO(id: "p", symbol: "AAPLx", status: "open")), "AAPL · Buy")
        XCTAssertEqual(ProposalFeedCopy.subtitle(for: ProposalDTO(id: "p", symbol: "AAPLx", status: "open", kind: "sell")), "AAPL · Sell")
        XCTAssertEqual(
            ProposalFeedCopy.subtitle(for: ProposalDTO(id: "p", symbol: "", status: "open", kind: "add_agent", allocationUsdcMicros: "500000000")),
            "New trading bot · Budget from the pot"
        )
    }

    func testClosedLabel_perOutcome() {
        func label(_ status: String, kind: String = "buy", execution: String? = nil) -> String? {
            ProposalFeedCopy.closedLabel(for: ProposalDTO(
                id: "p", symbol: "AAPLx", status: status, kind: kind,
                execution: execution.map { ProposalExecutionDTO(state: $0) }
            ))
        }
        XCTAssertNil(label("open"))
        XCTAssertEqual(label("passed", execution: "confirmed"), "Bought")
        XCTAssertEqual(label("passed", kind: "sell", execution: "confirmed"), "Sold")
        XCTAssertEqual(label("passed", execution: "pending"), "Buying")
        XCTAssertEqual(label("passed", execution: "failed"), "Failed")
        XCTAssertEqual(label("failed"), "Didn't pass")
        XCTAssertEqual(label("expired"), "Expired")
        XCTAssertEqual(label("passed", kind: "add_agent"), "Passed")
    }

    func testClosedLabel_withoutExecution_doesNotClaimTheSwapLanded() {
        // Feed rows carry no execution, so a passed trade may still be swapping — or may
        // have failed. Either way the chip must not read "Bought".
        func label(_ kind: String) -> String? {
            ProposalFeedCopy.closedLabel(for: ProposalDTO(id: "p", symbol: "AAPLc", status: "passed", kind: kind))
        }
        XCTAssertEqual(label("buy"), "Passed")
        XCTAssertEqual(label("sell"), "Passed")
    }

    func testExecutionStage_tracksVoteThenSwap() {
        func stage(_ status: String, kind: String = "buy", execution: String? = nil) -> ProposalExecutionStage? {
            ProposalExecutionStage.of(ProposalDTO(
                id: "p", symbol: "AAPLx", status: status, kind: kind,
                execution: execution.map { ProposalExecutionDTO(state: $0) }
            ))
        }
        XCTAssertEqual(stage("open"), .voting)
        XCTAssertEqual(stage("passed", execution: "pending"), .executing)
        XCTAssertEqual(stage("passed", execution: "confirmed"), .done)
        XCTAssertEqual(stage("passed", execution: "failed"), .failed)
        XCTAssertNil(stage("passed", execution: "not_applicable"), "seeded passed proposals never swap")
        XCTAssertNil(stage("failed"))
        XCTAssertNil(stage("expired"))
        XCTAssertNil(stage("open", kind: "add_agent"))
        XCTAssertEqual(ProposalExecutionStage.failed.stepIndex, 1)
        XCTAssertEqual(ProposalExecutionStage.done.stepIndex, 2)
    }

    func testAwaitingExecution_onlyWhilePending() {
        let pending = ProposalDTO(id: "p", symbol: "AAPLx", status: "passed", execution: ProposalExecutionDTO(state: "pending"))
        let done = ProposalDTO(id: "p", symbol: "AAPLx", status: "passed", execution: ProposalExecutionDTO(state: "confirmed"))
        let failed = ProposalDTO(id: "p", symbol: "AAPLx", status: "passed", execution: ProposalExecutionDTO(state: "failed"))
        let open = ProposalDTO(id: "p", symbol: "AAPLx", status: "open")
        XCTAssertTrue(pending.isAwaitingExecution)
        XCTAssertFalse(done.isAwaitingExecution)
        XCTAssertFalse(failed.isAwaitingExecution)
        XCTAssertFalse(open.isAwaitingExecution)
    }

    func testViewerChoice_matchesViewerBallotOnly() {
        let proposal = ProposalDTO(id: "p", symbol: "AAPLx", status: "open", votes: [
            ProposalVoteDTO(voterId: "b", displayName: "Bea", choice: "yes"),
            ProposalVoteDTO(voterId: "me", displayName: "Logan", choice: "No"),
        ])
        XCTAssertEqual(proposal.viewerChoice(viewerId: "me"), "no")
        XCTAssertNil(proposal.viewerChoice(viewerId: "someone"))
        XCTAssertNil(proposal.viewerChoice(viewerId: nil))
        XCTAssertNil(proposal.viewerChoice(viewerId: ""))
    }

    func testReadOnlyProposal_neverShowsVoteActions() {
        // Faker ghost proposals come back open with canVote false: tally only.
        let ghost = ProposalDTO(id: "p", symbol: "TSLAx", status: "open", canVote: false)
        let unknown = ProposalDTO(id: "p", symbol: "TSLAx", status: "open")
        XCTAssertFalse(ghost.showsVoteActions)
        XCTAssertFalse(unknown.showsVoteActions)
    }

    func testHeadline_sellProposal_usesShareCount() {
        // Arrange
        let proposal = ProposalDTO(id: "p", symbol: "AAPLx", status: "open", kind: "sell", tokenAmount: "50000000")

        // Act
        let headline = ProposalFeedCopy.headline(for: proposal)

        // Assert
        XCTAssertEqual(headline, "Sell 0.5 AAPLx")
    }

    func testHeadline_buyWithoutKind_defaultsToBuy() {
        // Arrange
        let proposal = ProposalDTO(id: "p", symbol: "AAPLx", status: "open", usdcMicros: "25000000")

        // Act
        let headline = ProposalFeedCopy.headline(for: proposal)

        // Assert
        XCTAssertEqual(headline, "Buy $25.00 of AAPLx")
    }

    func testHeadline_addAgentProposal_namesAgentAndBudget() {
        // Arrange
        let proposal = ProposalDTO(id: "p", symbol: "", status: "open", kind: "add_agent", agentDisplayName: "Scout", allocationUsdcMicros: "500000000")

        // Act
        let headline = ProposalFeedCopy.headline(for: proposal)
        let title = ProposalFeedCopy.title(for: proposal)

        // Assert
        XCTAssertEqual(headline, "Add agent Scout with $500.00 budget")
        XCTAssertEqual(title, "Scout")
        XCTAssertFalse(proposal.isTrade)
    }

    func testHeadline_pauseAgentWithoutName_usesGenericTitle() {
        // Arrange
        let proposal = ProposalDTO(id: "p", symbol: "", status: "open", kind: "pause_agent")

        // Act
        let headline = ProposalFeedCopy.headline(for: proposal)
        let title = ProposalFeedCopy.title(for: proposal)

        // Assert
        XCTAssertEqual(headline, "Pause the cabal trading agent")
        XCTAssertEqual(title, ProposalFeedCopy.agentTitle)
    }

    func testShares_wholeAndFractional() {
        // Act / Assert
        XCTAssertEqual(ProposalShareFormatter.shares(fromAtomics: "300000000"), "3")
        XCTAssertEqual(ProposalShareFormatter.shares(fromAtomics: "12345678"), "0.12345678")
        XCTAssertEqual(ProposalShareFormatter.shares(fromAtomics: "abc"), "abc")
    }

    func testReasonLength_matchesBackendThesisRule() {
        XCTAssertEqual(ProposeFlowCopy.reasonMax, 500)
        XCTAssertEqual(ProposeFlowCopy.reasonLength("  Earnings beat.  \n"), 14)
        XCTAssertEqual(ProposeFlowCopy.reasonLength("   "), 0)
        // The backend counts UTF-8 bytes, so accented text uses more of the limit.
        XCTAssertEqual(ProposeFlowCopy.reasonLength("é"), 2)
        XCTAssertEqual(ProposeFlowCopy.reasonCounter(480), "480 of 500")
    }
}
