import XCTest
@testable import MonacoCore

final class MatchupCopyTests: XCTestCase {
    private let minus = "\u{2212}"

    func testDaysLeft() {
        XCTAssertEqual(MatchupCopy.daysLeft(7), "7 days left")
        XCTAssertEqual(MatchupCopy.daysLeft(3), "3 days left")
        XCTAssertEqual(MatchupCopy.daysLeft(1), "Last day")
        XCTAssertEqual(MatchupCopy.daysLeft(0), "Week over")
        XCTAssertEqual(MatchupCopy.daysLeft(-2), "Week over")
    }

    func testScore_signsAndRoundsToOneDecimal() {
        XCTAssertEqual(MatchupCopy.score("0.0123"), "+1.2%")
        XCTAssertEqual(MatchupCopy.score("-0.004"), "\(minus)0.4%")
        XCTAssertEqual(MatchupCopy.score("0"), "0.0%")
        XCTAssertEqual(MatchupCopy.score("-0.0001"), "0.0%")
        XCTAssertEqual(MatchupCopy.score(nil), "—")
        XCTAssertEqual(MatchupCopy.score("nan"), "—")
    }

    func testScores_addADecimalWhenAWinnerWouldReadTheSameAsTheLoser() {
        // Arrange / Act
        let close = MatchupCopy.scores("0.01234", "0.01205")
        let apart = MatchupCopy.scores("0.012", "0.008")
        let tied = MatchupCopy.scores("0.01", "0.01")

        // Assert
        XCTAssertEqual(close.a, "+1.23%")
        XCTAssertEqual(close.b, "+1.21%")
        XCTAssertEqual(apart.a, "+1.2%")
        XCTAssertEqual(apart.b, "+0.8%")
        XCTAssertEqual(tied.a, "+1.0%")
        XCTAssertEqual(tied.b, "+1.0%")
    }

    func testLeadFraction_isEvenWhenLevelAndNeverHidesTheTrailingSide() {
        XCTAssertEqual(MatchupCopy.leadFraction(a: "0.01", b: "0.01"), 0.5, accuracy: 1e-9)
        XCTAssertEqual(MatchupCopy.leadFraction(a: "0.015", b: "0.005"), 0.7, accuracy: 1e-9)
        XCTAssertEqual(MatchupCopy.leadFraction(a: "0.30", b: "-0.10"), 0.9, accuracy: 1e-9)
        XCTAssertEqual(MatchupCopy.leadFraction(a: "-0.10", b: "0.30"), 0.1, accuracy: 1e-9)
        XCTAssertEqual(MatchupCopy.leadFraction(a: nil, b: "0.30"), 0.5, accuracy: 1e-9)
    }

    func testRecord() {
        XCTAssertEqual(MatchupCopy.record(MatchupRecordDTO(wins: 4, losses: 1, ties: 0)), "4\u{2013}1")
        XCTAssertEqual(MatchupCopy.record(MatchupRecordDTO(wins: 4, losses: 1, ties: 2)), "4\u{2013}1\u{2013}2")
        XCTAssertEqual(MatchupCopy.recordSpoken(MatchupRecordDTO(wins: 1, losses: 2, ties: 1)), "1 win, 2 losses, 1 tie")
    }

    func testResultLine() {
        // Arrange: Monday 14 September 2026, 00:00 UTC.
        let week = Date(timeIntervalSince1970: 1_789_344_000)
        let opponent = MatchupFaceDTO(groupID: "g2", name: "Semis or bust", memberCount: 5)
        let win = MatchupResultDTO(id: "m1", weekStart: week, result: .win, opponent: opponent, score: "0.012", opponentScore: "0.008")
        let bye = MatchupResultDTO(id: "m2", weekStart: week, result: .bye, opponent: nil, score: nil, opponentScore: nil)

        // Act / Assert
        XCTAssertEqual(MatchupCopy.resultLine(win), "W · vs Semis or bust · +1.2% to +0.8% · Sep 14")
        XCTAssertEqual(MatchupCopy.resultLine(bye), "Bye · Sep 14")
    }

    func testWeekLabel_readsTheMondayInEveryTimeZone() {
        // Monday 21 September 00:00 UTC is still Sunday in California; the week is Monday's.
        XCTAssertEqual(MatchupCopy.weekLabel(Date(timeIntervalSince1970: 1_789_948_800)), "Sep 21")
    }

    func testRefusalCopy_coversEveryReason() {
        let reasons: [MatchupChallengeRefusal?] = [.alreadyMatched, .incomingChallenge, .tooManyChallenges, .challengeClosed, nil]
        let lines = reasons.map(MatchupCopy.refusal)
        XCTAssertEqual(Set(lines).count, reasons.count)
    }

    func testSpoken() {
        let week = Date(timeIntervalSince1970: 1_789_948_800)
        let matchup = MatchupDTO(
            id: "m",
            weekStart: week,
            weekEnd: week.addingTimeInterval(7 * 86_400),
            daysLeft: 3,
            a: MatchupSideDTO(groupID: "g1", name: "Weekend investors", memberCount: 8, score: "0.012"),
            b: MatchupSideDTO(groupID: "g2", name: "Semis or bust", memberCount: 5, score: "-0.004"),
            leading: .a
        )
        XCTAssertEqual(MatchupCopy.spoken(matchup), "Weekend investors, +1.2%, against Semis or bust, \(minus)0.4%. 3 days left.")
    }

    func testCopy_usesTheProductsWords() {
        var strings = MatchupCopy.allFixedStrings
        strings += [
            MatchupCopy.challengeSent(to: "Semis or bust"),
            MatchupCopy.challengeAccepted(opponent: "Semis or bust"),
            MatchupCopy.incomingChallenge(from: "Semis or bust"),
            MatchupCopy.outgoingChallenge(to: "Semis or bust"),
            MatchupCopy.nextOpponent("Semis or bust"),
            MatchupCopy.acceptFailed,
        ]
        strings += [MatchupChallengeRefusal.alreadyMatched, .incomingChallenge, .tooManyChallenges, .challengeClosed, nil].map(MatchupCopy.refusal)
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean(strings), "matchup copy uses a forbidden word")
        for string in strings {
            XCTAssertFalse(string.localizedCaseInsensitiveContains("group"), string)
            XCTAssertFalse(string.localizedCaseInsensitiveContains("P&L"), string)
        }
    }
}
