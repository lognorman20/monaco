import XCTest
@testable import MonacoCore

final class CabalPositionRowFiguresTests: XCTestCase {
    func testFlatPnl_showsTheMembersMoney_notZeroDollars() {
        // The live confusion: a $2.00 slice with flat P&L rendered "$0.00" as the row's figure.
        let figures = CabalPositionRowFigures(equityUsd: "2.00", dollarPnl: "+0.00", percentReturn: nil)

        XCTAssertEqual(figures.equityUsd, "2.00")
        XCTAssertEqual(figures.change, .percent("0"))
        XCTAssertEqual(PercentReturnFormatter.format("0"), "0.0%")
    }

    func testPercentIsPreferredWhenTheServerHasOne() {
        let up = CabalPositionRowFigures(equityUsd: "311.50", dollarPnl: "+27.40", percentReturn: "0.096")
        let down = CabalPositionRowFigures(equityUsd: "400.05", dollarPnl: "-15.00", percentReturn: "-0.036")

        XCTAssertEqual(up.change, .percent("0.096"))
        XCTAssertEqual(down.change, .percent("-0.036"))
    }

    func testMissingPercent_fallsBackToSignedDollarsWhenThePnlMoved() {
        let figures = CabalPositionRowFigures(equityUsd: "120.00", dollarPnl: "-7.60", percentReturn: nil)

        XCTAssertEqual(figures.change, .dollars("-7.60"))
    }

    func testMalformedServerValues_neverBecomeADollarFigure() {
        let blankPercent = CabalPositionRowFigures(equityUsd: "5.00", dollarPnl: "+0.00", percentReturn: "  ")
        let garbagePnl = CabalPositionRowFigures(equityUsd: "5.00", dollarPnl: "n/a", percentReturn: nil)

        XCTAssertEqual(blankPercent.change, .percent("0"))
        XCTAssertEqual(garbagePnl.change, .unavailable, "unknown is not the same as flat")
    }

    func testPotSubtitle() {
        XCTAssertEqual(CabalPositionRowFigures.potSubtitle(potValueUsd: "1900"), "Pot $1,900.00")
        XCTAssertNil(CabalPositionRowFigures.potSubtitle(potValueUsd: nil))
    }
}
