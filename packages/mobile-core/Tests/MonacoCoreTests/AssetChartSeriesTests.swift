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

    /// An index is a claim about a sample. One the series does not have gets nothing
    /// back — a fallback to the end of the curve would hand a caller a real-looking
    /// number for a point that is not there. "No index" still means the end.
    func testAnOutOfBoundsScrubIndexMeasuresNothing() {
        let chart = series(prices: [100_000_000, 110_000_000])

        XCTAssertNil(chart.changeRatio(toIndex: 99))
        XCTAssertNil(chart.changeDollars(toIndex: 99))
        XCTAssertNil(chart.changeRatio(toIndex: -1))
        XCTAssertNotNil(chart.changeRatio())
        XCTAssertEqual(chart.changeRatio(toIndex: 1), chart.changeRatio())
    }

    // MARK: - One point per instant

    /// The drawing side keys a mark by its timestamp, so two samples at one instant
    /// are two marks with one identity — SwiftUI warns and then renders it wrong.
    /// Nothing upstream promises distinct instants, so the series is what enforces
    /// it: the later read of an instant wins.
    func testTwoSamplesAtOneInstantCollapseToTheLaterRead() {
        let start = Int64(tuesday.timeIntervalSince1970)
        let chart = AssetChartSeries(range: .oneDay, points: [
            AssetChartPointDTO(timestamp: start, priceUsdcMicros: 100_000_000),
            AssetChartPointDTO(timestamp: start + 300, priceUsdcMicros: 101_000_000),
            AssetChartPointDTO(timestamp: start + 300, priceUsdcMicros: 102_000_000),
            AssetChartPointDTO(timestamp: start + 600, priceUsdcMicros: 103_000_000),
        ])

        XCTAssertEqual(chart.points.map(\.timestamp), [start, start + 300, start + 600])
        XCTAssertEqual(chart.points[1].priceUsdcMicros, 102_000_000)
        XCTAssertEqual(Set(chart.points.map(\.timestamp)).count, chart.points.count)
    }

    /// Out of order *and* repeated: sorting happens first, so the winner is the last
    /// one for that instant in time order, not in arrival order.
    func testRepeatsAreCollapsedAfterSorting() {
        let start = Int64(tuesday.timeIntervalSince1970)
        let chart = AssetChartSeries(range: .oneWeek, points: [
            AssetChartPointDTO(timestamp: start + 600, priceUsdcMicros: 103_000_000),
            AssetChartPointDTO(timestamp: start, priceUsdcMicros: 100_000_000),
            AssetChartPointDTO(timestamp: start + 600, priceUsdcMicros: 104_000_000),
        ])

        XCTAssertEqual(chart.points.map(\.timestamp), [start, start + 600])
        XCTAssertEqual(chart.points.last?.priceUsdcMicros, 104_000_000)
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

    // MARK: - Whose curve this is

    func testAnUnderlyingCurveIsCaptionedAsTheShareOnItsExchange() throws {
        let chart = try XCTUnwrap(AssetChartSeries(MarketSampleData.chart(range: .oneDay), requested: .oneDay))

        XCTAssertEqual(chart.basisCaption, "AAPL on its home exchange")
        XCTAssertEqual(chart.underlyingDisplaySymbol, "AAPL")
    }

    /// The last fallback is the token's own Chainlink rounds: the same instrument as
    /// the hero price, on Base, and never tagged as the share.
    func testTheTokensOwnRoundsAreCaptionedAsTheTokenOnBase() throws {
        let chart = try XCTUnwrap(AssetChartSeries(MarketSampleData.chartFromChainlink(range: .oneDay), requested: .oneDay))

        XCTAssertEqual(chart.basisCaption, "AAPL token on Base")
        XCTAssertNil(chart.underlyingDisplaySymbol)
        XCTAssertFalse(chart.basisCaption?.contains("Solana") ?? true)
    }

    func testASeriesNobodyNamedGetsNoCaption() {
        let chart = series(prices: [100_000_000, 110_000_000])

        XCTAssertNil(chart.basisCaption)
        XCTAssertNil(chart.underlyingDisplaySymbol)
    }

    // MARK: - The session a day chart draws

    /// Friday 2026-09-25, 15:55 ET: a bar near the end of Friday's regular session.
    private let fridayAfternoon = Date(timeIntervalSince1970: 1_790_366_100)

    private func fridaySession(source: AssetChartSource? = .benchmarks, range: AssetChartRange = .oneDay) -> AssetChartSeries {
        let end = Int64(fridayAfternoon.timeIntervalSince1970)
        return AssetChartSeries(
            range: range,
            points: [
                AssetChartPointDTO(timestamp: end - 600, priceUsdcMicros: 230_000_000),
                AssetChartPointDTO(timestamp: end, priceUsdcMicros: 231_000_000),
            ],
            previousCloseUsdcMicros: 229_000_000,
            source: source,
            basis: .underlying,
            basisSymbol: "AAPL"
        )
    }

    /// The gap the data stage left: the backend's 1D window on a Saturday is
    /// Friday's session, and the header called its move "Past day".
    func testOnASaturdayTheDayChartIsNamedForFridaysSession() throws {
        let session = try XCTUnwrap(fridaySession().session(now: MarketSampleData.saturdayNoon, locale: Locale(identifier: "en_US")))

        XCTAssertFalse(session.isToday)
        XCTAssertTrue(session.caption.contains("Fri"), session.caption)
        XCTAssertTrue(session.caption.contains("25"), session.caption)
        XCTAssertEqual(session.spokenPhrase, "on \(session.date)")
    }

    func testDuringTheSessionItIsToday() throws {
        let session = try XCTUnwrap(fridaySession().session(now: fridayAfternoon.addingTimeInterval(60), locale: Locale(identifier: "en_US")))

        XCTAssertTrue(session.isToday)
        XCTAssertEqual(session.caption, "Today")
        XCTAssertEqual(session.spokenPhrase, "today")
    }

    /// The session's day is New York's. At 16:00 ET on Friday it is already 05:00
    /// on Saturday in Tokyo, but on the exchange it is still Friday, so Friday's
    /// session is today's, and its date is written as Friday's.
    func testTheSessionDayIsTheExchangesNotTheReaders() throws {
        let tokyoSaturdayMorning = fridayAfternoon.addingTimeInterval(5 * 60)
        let tokyo = TimeZone(identifier: "Asia/Tokyo")!
        var tokyoCalendar = Calendar(identifier: .gregorian)
        tokyoCalendar.timeZone = tokyo
        XCTAssertEqual(tokyoCalendar.component(.weekday, from: tokyoSaturdayMorning), 7, "the fixture is Saturday in Tokyo")

        let session = try XCTUnwrap(fridaySession().session(now: tokyoSaturdayMorning, locale: Locale(identifier: "en_JP")))

        XCTAssertTrue(session.isToday)
        XCTAssertTrue(session.date.contains("25"), session.date)
    }

    /// The case above cannot tell the two calendars apart on "today": Fri 15:55 ET and
    /// Fri 16:00 ET are the same day in Tokyo too. These two can. In each, New York and
    /// Tokyo disagree about whether the session and "now" share a day, so a reader-calendar
    /// implementation gets `isToday` wrong.
    func testTodayIsDecidedOnTheExchangesCalendarWhereTheTwoDisagree() throws {
        var newYork = Calendar(identifier: .gregorian)
        newYork.timeZone = ChartSessionDay.exchangeTimeZone
        var tokyo = Calendar(identifier: .gregorian)
        tokyo.timeZone = TimeZone(identifier: "Asia/Tokyo")!
        let locale = Locale(identifier: "en_JP")

        // Friday's session, read at Sat 00:30 ET (Sat 13:30 in Tokyo). Tokyo calls the
        // session Saturday morning and so "today"; on the exchange it was yesterday.
        let saturdayJustAfterMidnightET = fridayAfternoon.addingTimeInterval(8 * 3600 + 35 * 60)
        XCTAssertTrue(tokyo.isDate(fridayAfternoon, inSameDayAs: saturdayJustAfterMidnightET), "the fixture is one day in Tokyo")
        XCTAssertFalse(newYork.isDate(fridayAfternoon, inSameDayAs: saturdayJustAfterMidnightET), "the fixture is two days in New York")

        let yesterday = try XCTUnwrap(fridaySession().session(now: saturdayJustAfterMidnightET, locale: locale))
        XCTAssertFalse(yesterday.isToday)
        XCTAssertNotEqual(yesterday.caption, "Today")
        XCTAssertTrue(yesterday.date.contains("25"), yesterday.date)

        // A bar at Fri 09:35 ET (Fri 22:35 in Tokyo), read at Fri 15:55 ET (Sat 04:55 in
        // Tokyo). Tokyo says two different days; on the exchange it is still today's session.
        let fridayOpen = fridayAfternoon.addingTimeInterval(-(6 * 3600 + 20 * 60))
        XCTAssertFalse(tokyo.isDate(fridayOpen, inSameDayAs: fridayAfternoon), "the fixture is two days in Tokyo")
        XCTAssertTrue(newYork.isDate(fridayOpen, inSameDayAs: fridayAfternoon), "the fixture is one day in New York")

        let today = ChartSessionDay(sessionInstant: fridayOpen, now: fridayAfternoon, locale: locale)
        XCTAssertTrue(today.isToday)
        XCTAssertEqual(today.caption, "Today")
    }

    /// The sampler and the Chainlink rounds are a rolling 24 hours, which "Past day"
    /// describes; only a longer range or another source is never named for a session.
    func testOnlyABenchmarksDayChartIsOneSession() {
        XCTAssertNil(fridaySession(source: .hermes).session(now: MarketSampleData.saturdayNoon))
        XCTAssertNil(fridaySession(source: .chainlink).session(now: MarketSampleData.saturdayNoon))
        XCTAssertNil(fridaySession(source: nil).session(now: MarketSampleData.saturdayNoon))
        XCTAssertNil(fridaySession(range: .oneWeek).session(now: MarketSampleData.saturdayNoon))
        XCTAssertNil(AssetChartSeries(range: .oneDay, points: [], source: .benchmarks).session(now: MarketSampleData.saturdayNoon))
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
