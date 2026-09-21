import XCTest
@testable import MonacoCore

/// The arithmetic behind the scrubbing chart: which sample a finger lands on, what
/// the change is measured from, and what the time under the price reads.
///
/// All of it is host-side and deterministic, so the drag behaviour is pinned here
/// rather than in a simulator test that can only assert that *something* moved.
final class AssetChartSeriesTests: XCTestCase {
    /// 2026-09-22 14:00:00 UTC, a Tuesday — the same instant `MarketSampleData` uses.
    private let tuesday = Date(timeIntervalSince1970: 1_790_085_600)

    private func series(
        range: AssetChartRange = .oneDay,
        prices: [Int64],
        stepSeconds: Int64 = 300,
        previousClose: Int64? = nil
    ) -> AssetChartSeries {
        let start = Int64(tuesday.timeIntervalSince1970)
        let points = prices.enumerated().map { index, price in
            AssetChartPointDTO(timestamp: start + Int64(index) * stepSeconds, priceUsdcMicros: price)
        }
        return AssetChartSeries(range: range, points: points, previousCloseUsdcMicros: previousClose)
    }

    // MARK: - Nearest point

    func testNearestIndex_takesTheCloserNeighbourNotTheEarlierOne() {
        let chart = series(prices: [100_000_000, 101_000_000, 102_000_000])

        // 60s past the first sample of a 300s grid: still nearest the first.
        XCTAssertEqual(chart.nearestIndex(to: tuesday.addingTimeInterval(60)), 0)
        // 240s past it: past the midpoint, so the second sample is nearer.
        XCTAssertEqual(chart.nearestIndex(to: tuesday.addingTimeInterval(240)), 1)
        XCTAssertEqual(chart.nearestIndex(to: tuesday.addingTimeInterval(320)), 1)
    }

    func testNearestIndex_clampsOutsideTheWindow() {
        let chart = series(prices: [100_000_000, 101_000_000, 102_000_000])

        XCTAssertEqual(chart.nearestIndex(to: tuesday.addingTimeInterval(-86_400)), 0)
        XCTAssertEqual(chart.nearestIndex(to: tuesday.addingTimeInterval(86_400)), 2)
    }

    func testNearestIndex_onAnExactSampleIsThatSample() {
        let chart = series(prices: Array(repeating: 100_000_000, count: 40))

        for index in [0, 7, 21, 39] {
            let sample = chart.points[index].date
            XCTAssertEqual(chart.nearestIndex(to: sample), index, "sample \(index)")
        }
    }

    /// A tie must resolve the same way every time, or a finger held still on the
    /// midpoint flickers between two prices.
    func testNearestIndex_breaksATieTowardsTheEarlierSample() {
        let chart = series(prices: [100_000_000, 101_000_000])

        XCTAssertEqual(chart.nearestIndex(to: tuesday.addingTimeInterval(150)), 0)
    }

    func testNearestIndex_onAnEmptySeriesIsNil() {
        XCTAssertNil(series(prices: []).nearestIndex(to: tuesday))
    }

    /// The search is a binary search, so an out-of-order payload would quietly
    /// return the wrong sample. The series sorts instead.
    func testPointsAreSortedSoTheSearchStaysCorrect() {
        let unsorted = [
            AssetChartPointDTO(timestamp: 300, priceUsdcMicros: 3),
            AssetChartPointDTO(timestamp: 100, priceUsdcMicros: 1),
            AssetChartPointDTO(timestamp: 200, priceUsdcMicros: 2),
        ]
        let chart = AssetChartSeries(range: .oneDay, points: unsorted)

        XCTAssertEqual(chart.points.map(\.timestamp), [100, 200, 300])
        XCTAssertEqual(chart.nearestIndex(to: Date(timeIntervalSince1970: 190)), 1)
    }

    // MARK: - Baseline

    func testDayChart_measuresFromThePreviousClose() throws {
        let chart = series(
            range: .oneDay,
            prices: [100_000_000, 110_000_000],
            previousClose: 200_000_000
        )

        XCTAssertEqual(chart.baselineUsdcMicros, 200_000_000)
        XCTAssertTrue(chart.drawsBaselineRule)
        // 110 against a 200 close is a 45% fall, not the 10% rise inside the window.
        XCTAssertEqual(PercentReturnFormatter.format(chart.changeRatio()), "−45.0%")
    }

    /// The sampled fallback ships no previous close. Measuring from the window's own
    /// first point is right; drawing a dashed rule through that same point is not.
    func testWithoutAPreviousClose_theWindowMeasuresFromItsFirstPointAndDrawsNoRule() throws {
        let chart = series(range: .oneDay, prices: [100_000_000, 110_000_000])

        XCTAssertEqual(chart.baselineUsdcMicros, 100_000_000)
        XCTAssertFalse(chart.drawsBaselineRule)
        XCTAssertEqual(PercentReturnFormatter.format(chart.changeRatio()), "+10.0%")
    }

    /// Over a year, "previous close" is the close before the window — not a number
    /// anyone reads a year chart against.
    func testLongerRanges_ignoreThePreviousCloseEntirely() {
        for range in [AssetChartRange.oneWeek, .oneMonth, .threeMonths, .oneYear, .all] {
            let chart = series(range: range, prices: [100_000_000, 110_000_000], previousClose: 200_000_000)
            XCTAssertEqual(chart.baselineUsdcMicros, 100_000_000, "\(range.rawValue)")
            XCTAssertFalse(chart.drawsBaselineRule, "\(range.rawValue)")
        }
    }

    func testAZeroPreviousCloseIsNotABaseline() {
        let chart = series(range: .oneDay, prices: [100_000_000, 110_000_000], previousClose: 0)

        XCTAssertEqual(chart.baselineUsdcMicros, 100_000_000)
        XCTAssertFalse(chart.drawsBaselineRule)
    }

    // MARK: - Change

    func testChangeAtAScrubbedIndexIsMeasuredFromTheSameBaseline() throws {
        let chart = series(
            range: .oneDay,
            prices: [100_000_000, 105_000_000, 120_000_000],
            previousClose: 100_000_000
        )

        XCTAssertEqual(PercentReturnFormatter.format(chart.changeRatio(toIndex: 0)), "0.0%")
        XCTAssertEqual(PercentReturnFormatter.format(chart.changeRatio(toIndex: 1)), "+5.0%")
        XCTAssertEqual(PercentReturnFormatter.format(chart.changeRatio(toIndex: 2)), "+20.0%")
        // No index means the end of the curve.
        XCTAssertEqual(chart.changeRatio(), chart.changeRatio(toIndex: 2))
    }

    /// `String(someDouble)` goes exponential under 1e-4, and `MonacoTheme.signed`
    /// reads the digits of "5e-05" as "505" — a flat move tinted as a gain.
    func testATinyMoveIsWrittenInFixedPoint() throws {
        let chart = series(prices: [100_000_000, 100_005_000])
        let ratio = try XCTUnwrap(chart.changeRatio())

        XCTAssertFalse(ratio.lowercased().contains("e"), ratio)
        XCTAssertEqual(PercentReturnFormatter.format(ratio), "0.0%")
    }

    func testAnEmptySeriesHasNoChangeToShow() {
        XCTAssertNil(series(prices: []).changeRatio())
        XCTAssertNil(series(prices: []).changeDollars())
    }

    func testTheDollarMoveIsMeasuredFromTheSameBaselineAsThePercent() throws {
        let chart = series(
            range: .oneDay,
            prices: [100_000_000, 105_500_000],
            previousClose: 100_000_000
        )

        XCTAssertEqual(chart.changeDollars(), "5.50")
        XCTAssertEqual(chart.changeDollars(toIndex: 0), "0.00")
        XCTAssertEqual(SignedUsdFormatter.format(try XCTUnwrap(chart.changeDollars())), "+$5.50")
    }

    func testAFallingWindowGivesASignedDollarMove() throws {
        let chart = series(range: .oneDay, prices: [100_000_000, 98_750_000], previousClose: 100_000_000)

        XCTAssertEqual(chart.changeDollars(), "-1.25")
        XCTAssertEqual(SignedUsdFormatter.format(try XCTUnwrap(chart.changeDollars())), "−$1.25")
    }

    func testAnOutOfBoundsScrubIndexFallsBackToTheEndOfTheCurve() {
        let chart = series(prices: [100_000_000, 110_000_000])

        XCTAssertEqual(chart.changeRatio(toIndex: 99), chart.changeRatio())
    }

    // MARK: - Range echo

    func testASeriesBuiltForAnotherRangeIsRefused() {
        let dto = AssetChartDTO(
            points: [AssetChartPointDTO(timestamp: 1, priceUsdcMicros: 1)],
            range: .oneYear
        )

        XCTAssertNil(AssetChartSeries(dto, requested: .oneDay))
        XCTAssertNotNil(AssetChartSeries(dto, requested: .oneYear))
    }

    /// A backend that does not echo the range is older, not wrong: it answered the
    /// only range it was asked for.
    func testAResponseWithoutARangeIsTakenAtItsWord() throws {
        let dto = AssetChartDTO(points: [AssetChartPointDTO(timestamp: 1, priceUsdcMicros: 1)])
        let chart = try XCTUnwrap(AssetChartSeries(dto, requested: .oneMonth))

        XCTAssertEqual(chart.range, .oneMonth)
    }

    func testTheSeriesCarriesWhatTheChartNeedsToLabelItself() throws {
        let dto = MarketSampleData.chart(range: .oneDay)
        let chart = try XCTUnwrap(AssetChartSeries(dto, requested: .oneDay))

        XCTAssertEqual(chart.source, .benchmarks)
        XCTAssertEqual(chart.basis, .underlying)
        XCTAssertEqual(chart.basisSymbol, "AAPL")
        XCTAssertTrue(chart.isDrawable)
        XCTAssertTrue(chart.drawsBaselineRule)
    }

    func testTheSampleFallbackSeriesDrawsNoBaseline() throws {
        let dto = MarketSampleData.chartFromFallback(range: .oneDay)
        let chart = try XCTUnwrap(AssetChartSeries(dto, requested: .oneDay))

        XCTAssertEqual(chart.source, .hermes)
        XCTAssertFalse(chart.drawsBaselineRule)
        XCTAssertFalse(chart.points.contains { $0.hasCandle })
    }

    // MARK: - Scrub label

    func testDayScrubReadsAsAWallClockInTheReadersOwnLocale() {
        let newYork = TimeZone(identifier: "America/New_York")!
        let caption = ChartScrubLabel.caption(
            for: tuesday,
            range: .oneDay,
            locale: Locale(identifier: "en_US"),
            timeZone: newYork
        )

        // 14:00 UTC is 10:00 ET on 22 September.
        XCTAssertTrue(caption.contains("10:00"), caption)
        XCTAssertTrue(caption.contains("Tue"), caption)
        XCTAssertTrue(caption.uppercased().contains("AM"), caption)
    }

    func testA24HourLocaleGetsA24HourClock() {
        let caption = ChartScrubLabel.caption(
            for: tuesday,
            range: .oneDay,
            locale: Locale(identifier: "en_GB"),
            timeZone: TimeZone(identifier: "Europe/London")!
        )

        // 14:00 UTC is 15:00 London in September, written without a meridiem.
        XCTAssertTrue(caption.contains("15:00"), caption)
        XCTAssertFalse(caption.uppercased().contains("PM"), caption)
    }

    func testWiderRangesDropTheClockAndThenTheWeekday() {
        let utc = TimeZone(identifier: "UTC")!
        let locale = Locale(identifier: "en_US")

        let week = ChartScrubLabel.caption(for: tuesday, range: .oneWeek, locale: locale, timeZone: utc)
        XCTAssertTrue(week.contains("Tue"), week)
        XCTAssertTrue(week.contains("Sep"), week)
        XCTAssertFalse(week.contains(":"), week)

        let year = ChartScrubLabel.caption(for: tuesday, range: .oneYear, locale: locale, timeZone: utc)
        XCTAssertTrue(year.contains("2026"), year)
        XCTAssertFalse(year.contains("Tue"), year)
    }

    func testEveryRangeHasACaption() {
        for range in AssetChartRange.allCases {
            let caption = ChartScrubLabel.caption(
                for: tuesday,
                range: range,
                locale: Locale(identifier: "en_US"),
                timeZone: TimeZone(identifier: "UTC")!
            )
            XCTAssertFalse(caption.isEmpty, range.rawValue)
        }
    }
}
