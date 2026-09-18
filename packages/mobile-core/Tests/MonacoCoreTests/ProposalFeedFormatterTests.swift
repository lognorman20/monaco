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

    func testVoteProgress_fractionsAndCaption() {
        // Arrange
        let summary = ProposalVoteSummaryDTO(yesCount: 2, noCount: 1, eligibleCount: 6, threshold: "majority")

        // Act
        let progress = ProposalVoteProgress(summary: summary)

        // Assert
        XCTAssertEqual(progress.yesFraction, 2.0 / 6.0, accuracy: 0.0001)
        XCTAssertEqual(progress.noFraction, 1.0 / 6.0, accuracy: 0.0001)
        XCTAssertEqual(progress.caption, "2 yes · 1 no · 3 still to vote")
    }

    func testVoteProgress_votesExceedEligible_neverOverfillsBar() {
        // Arrange: a voter left the cabal after voting.
        let summary = ProposalVoteSummaryDTO(yesCount: 3, noCount: 1, eligibleCount: 3, threshold: "majority")

        // Act
        let progress = ProposalVoteProgress(summary: summary)

        // Assert
        XCTAssertLessThanOrEqual(progress.yesFraction + progress.noFraction, 1.0)
        XCTAssertEqual(progress.caption, "3 yes · 1 no")
    }

    func testVoteProgress_zeroEligible_isZeroNotNaN() {
        // Arrange
        let summary = ProposalVoteSummaryDTO(yesCount: 0, noCount: 0, eligibleCount: 0, threshold: "majority")

        // Act
        let progress = ProposalVoteProgress(summary: summary)

        // Assert
        XCTAssertEqual(progress.yesFraction, 0)
        XCTAssertEqual(progress.noFraction, 0)
    }

    func testClosesLabel_hoursAndMinutesLeft() {
        // Act
        let label = ProposalTimeFormatter.closesLabel(expiresAt: "2026-09-18T17:20:00Z", now: now)

        // Assert
        XCTAssertEqual(label, "Closes in 5h 20m")
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

    func testShares_wholeAndFractional() {
        // Act / Assert
        XCTAssertEqual(ProposalShareFormatter.shares(fromAtomics: "300000000"), "3")
        XCTAssertEqual(ProposalShareFormatter.shares(fromAtomics: "12345678"), "0.12345678")
        XCTAssertEqual(ProposalShareFormatter.shares(fromAtomics: "abc"), "abc")
    }
}
