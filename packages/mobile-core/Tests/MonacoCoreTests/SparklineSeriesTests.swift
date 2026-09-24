import Foundation
import XCTest
@testable import MonacoCore

final class SparklineSeriesTests: XCTestCase {
    func testEmptySeriesHasNothingToDraw() {
        XCTAssertNil(SparklineSeries(usdcMicros: []))
    }

    func testSinglePointHasNothingToDraw() {
        XCTAssertNil(SparklineSeries(usdcMicros: [1_000_000]))
    }

    func testZeroMicrosAreDroppedRatherThanDrawnAsAFloor() {
        // A zero is a gap the upstream could not fill. Left in, it would anchor the
        // low of the window and flatten every real point against the top.
        let series = SparklineSeries(usdcMicros: [0, 100_000, 0, 200_000, 0])
        XCTAssertEqual(series?.heights, [0, 1])
        XCTAssertEqual(series?.firstUsdcMicros, 100_000)
        XCTAssertEqual(series?.lastUsdcMicros, 200_000)
    }

    func testOnlyOneUsablePointAfterFilteringHasNothingToDraw() {
        XCTAssertNil(SparklineSeries(usdcMicros: [0, 0, 500_000]))
    }

    func testHeightsNormaliseToTheWindow() {
        let series = SparklineSeries(usdcMicros: [100, 150, 200])
        XCTAssertEqual(series?.heights, [0, 0.5, 1])
    }

    func testFlatSeriesDrawsThroughTheMiddleNotTheFloor() {
        let series = SparklineSeries(usdcMicros: [200_000, 200_000, 200_000])
        XCTAssertEqual(series?.heights, [0.5, 0.5, 0.5])
        XCTAssertTrue(series?.isRising ?? false)
    }

    func testFallingSeriesIsNotRising() {
        let series = SparklineSeries(usdcMicros: [300, 250, 200])
        XCTAssertEqual(series?.isRising, false)
    }

    func testDownsampleKeepsBothEnds() {
        let values = (1...500).map(Int64.init)
        let sampled = SparklineSeries.downsample(values, to: 24)
        XCTAssertEqual(sampled.count, 24)
        XCTAssertEqual(sampled.first, 1)
        XCTAssertEqual(sampled.last, 500)
    }

    func testDownsampleLeavesAShortSeriesAlone() {
        let values: [Int64] = [1, 2, 3]
        XCTAssertEqual(SparklineSeries.downsample(values, to: 24), values)
    }

    func testDenseSeriesIsCappedForTheRow() {
        let series = SparklineSeries(usdcMicros: (1...5_000).map(Int64.init))
        XCTAssertEqual(series?.heights.count, SparklineSeries.maximumPoints)
        XCTAssertEqual(series?.firstUsdcMicros, 1)
        XCTAssertEqual(series?.lastUsdcMicros, 5_000)
    }
}

final class DayChangeFiguresTests: XCTestCase {
    func testRatioReadsBothSpellings() {
        XCTAssertEqual(DayChangeFigures.ratio(from: "0.0124"), Decimal(string: "0.0124"))
        XCTAssertEqual(DayChangeFigures.ratio(from: "+1.24%"), Decimal(string: "0.0124"))
        XCTAssertEqual(DayChangeFigures.ratio(from: "\u{2212}1.24%"), Decimal(string: "-0.0124"))
    }

    func testRatioRefusesNonNumbers() {
        XCTAssertNil(DayChangeFigures.ratio(from: nil))
        XCTAssertNil(DayChangeFigures.ratio(from: ""))
        XCTAssertNil(DayChangeFigures.ratio(from: "—"))
        XCTAssertNil(DayChangeFigures.ratio(from: "nan"))
        XCTAssertNil(DayChangeFigures.ratio(from: "inf"))
        XCTAssertNil(DayChangeFigures.ratio(from: "up a bit"))
    }

    func testDollarMoveIsMeasuredFromThePreviousClose() {
        // $110 now after +10% means a $100 close and a $10 move — not $11, which is
        // what taking the percentage of the current price would give.
        let text = DayChangeFigures.dollarText(change24h: "0.1", priceUsdcMicros: 110_000_000)
        XCTAssertEqual(text, "+$10.00")
    }

    func testDollarMoveOnALoss() {
        let text = DayChangeFigures.dollarText(change24h: "-0.02", priceUsdcMicros: 98_000_000)
        XCTAssertEqual(text, "\u{2212}$2.00")
    }

    func testDollarMoveIsNilWithoutAPrice() {
        XCTAssertNil(DayChangeFigures.dollarText(change24h: "0.1", priceUsdcMicros: nil))
        XCTAssertNil(DayChangeFigures.dollarText(change24h: "0.1", priceUsdcMicros: 0))
    }

    func testDollarMoveIsNilWithoutAChange() {
        XCTAssertNil(DayChangeFigures.dollarText(change24h: nil, priceUsdcMicros: 110_000_000))
    }

    func testDollarMoveRefusesAnImpossiblePreviousClose() {
        // A ratio of −100% or worse implies a close at or below zero.
        XCTAssertNil(DayChangeFigures.dollarText(change24h: "-1.0", priceUsdcMicros: 110_000_000))
        XCTAssertNil(DayChangeFigures.dollarText(change24h: "-1.5", priceUsdcMicros: 110_000_000))
    }

    func testFlatDayShowsAnUnsignedZero() {
        XCTAssertEqual(DayChangeFigures.dollarText(change24h: "0.0", priceUsdcMicros: 110_000_000), "$0.00")
    }

    func testPercentText() {
        XCTAssertEqual(DayChangeFigures.percentText(change24h: "0.0124"), "+1.2%")
        XCTAssertEqual(DayChangeFigures.percentText(change24h: nil), "—")
    }
}

final class TopMoversTests: XCTestCase {
    private func asset(_ symbol: String, change: String?) -> MarketAssetDTO {
        MarketAssetDTO(symbol: symbol, name: symbol, solanaMint: "m", routable: true, priceUsdcMicros: 1_000_000, change24h: change)
    }

    func testBiggestAbsoluteMoveFirst() {
        let ranked = TopMovers.rank([
            asset("A", change: "0.01"),
            asset("B", change: "-0.09"),
            asset("C", change: "0.04"),
        ])
        XCTAssertEqual(ranked.map(\.symbol), ["B", "C", "A"])
    }

    func testRowsWithoutAReadableChangeAreLeftOut() {
        let ranked = TopMovers.rank([
            asset("A", change: nil),
            asset("B", change: "nan"),
            asset("C", change: "0.04"),
        ])
        XCTAssertEqual(ranked.map(\.symbol), ["C"])
    }

    func testTiesKeepCatalogueOrderSoTheStripDoesNotReshuffle() {
        let ranked = TopMovers.rank([
            asset("A", change: "0.02"),
            asset("B", change: "-0.02"),
            asset("C", change: "0.02"),
        ])
        XCTAssertEqual(ranked.map(\.symbol), ["A", "B", "C"])
    }

    func testLimitIsRespected() {
        let ranked = TopMovers.rank([
            asset("A", change: "0.01"),
            asset("B", change: "0.09"),
            asset("C", change: "0.04"),
        ], limit: 2)
        XCTAssertEqual(ranked.map(\.symbol), ["B", "C"])
        XCTAssertTrue(TopMovers.rank([asset("A", change: "0.01")], limit: 0).isEmpty)
    }
}
