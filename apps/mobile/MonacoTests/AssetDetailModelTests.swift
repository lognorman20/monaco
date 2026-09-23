import Foundation
import MonacoCore
import Testing
@testable import Monaco

// The app target shadows these MonacoCore DTOs; pin the tests to the ones the views use.
private typealias AssetChartRange = Monaco.AssetChartRange
private typealias AssetChartDTO = Monaco.AssetChartDTO
private typealias AssetChartPointDTO = Monaco.AssetChartPointDTO
private typealias AssetDetailDTO = Monaco.AssetDetailDTO
private typealias AssetLiquidityDTO = Monaco.AssetLiquidityDTO
private typealias MarketStatusDTO = Monaco.MarketStatusDTO
private typealias MarketSession = Monaco.MarketSession

@MainActor
private final class StubAssetDetailDataSource: AssetDetailDataSource {
    var detailCalls = 0
    var chartCalls: [AssetChartRange] = []
    /// Per-range latency, so a slow range can answer after a faster one.
    var delays: [AssetChartRange: Duration] = [:]
    var detailError: Error?
    var chartError: Error?
    var routable = true
    /// The buy probe's own verdict for this one response. A probe that errored reports `false`
    /// here for that response (the backend does not cache it), so it can disagree with `routable`.
    var liquidityRoutable = true
    /// The sell probe's USDC out, or nil when the sell side found no route.
    var sellProbeOutAmount: String? = "185000000"
    /// One scripted detail answer: how long it takes, the price it reports, and the error
    /// it throws instead, if any.
    struct ScriptedDetail {
        var delay: Duration
        var price: Int64?
        var error: Error?
    }

    /// Scripted answers for successive detail calls, consumed in order. Lets a slow first
    /// load answer after a faster poll.
    var scriptedDetail: [ScriptedDetail] = []
    var change24h: String? = "0.05"
    /// The backend labels `change24h` as the underlying's; nil plays an older backend.
    var change24hBasis: MarketPriceBasis? = .underlying
    var priceUsdcMicros: Int64? = 185_000_000
    var marketSession: MarketSession?
    var market: MarketStatusDTO?
    /// The stock-vs-token card. This branch does not draw it, but the hero reads the
    /// mark's own freshness off it.
    var stockVsToken: StockVsTokenDTO?
    var points: [AssetChartRange: [AssetChartPointDTO]] = [:]
    var previousCloseUsdcMicros: Int64?
    /// What the server says about a window it could not fill, per range. On a B20
    /// token the chart comes off a Chainlink feed that is only weeks old, so 3M and
    /// 1Y really do come back empty with "Only on-chain since 5 Aug 2026".
    var emptyReason: [AssetChartRange: String] = [:]
    /// The range the server claims each series was built for. Nil echoes the request.
    var echoedRange: AssetChartRange?
    var source: AssetChartSource?
    /// Which instrument the series is about. Every Pyth history source serves the
    /// underlying equity, so this is what the shipping backend sends.
    var basis: MarketPriceBasis?
    var basisSymbol: String?
    /// Scripted answers for successive calls to one range, consumed in order. Lets a
    /// test make the *first* request return after the second, which is how a tap and
    /// a background poll overlap in life.
    var scripted: [AssetChartRange: [(delay: Duration, points: [AssetChartPointDTO])]] = [:]

    func detail(symbol: String) async throws -> AssetDetailDTO {
        detailCalls += 1
        var priceUsdcMicros = self.priceUsdcMicros
        if !scriptedDetail.isEmpty {
            let next = scriptedDetail.removeFirst()
            priceUsdcMicros = next.price
            try? await Task.sleep(for: next.delay)
            if let error = next.error { throw error }
        } else if let detailError {
            throw detailError
        }
        return AssetDetailDTO(
            symbol: symbol,
            name: "Apple",
            tokenAddress: "0xb200000000000000000000000000000000000001",
            routable: routable,
            priceUsdcMicros: priceUsdcMicros,
            change24h: change24h,
            change24hBasis: change24hBasis,
            change24hBasisSymbol: change24hBasis == nil ? nil : "AAPL",
            liquidity: AssetLiquidityDTO(
                label: "Via DEX",
                routable: liquidityRoutable,
                buyProbeUsdcMicros: 1_000_000,
                buyProbeOutAmount: "100000000",
                sellProbeInAmount: sellProbeOutAmount == nil ? nil : "100000000",
                sellProbeOutAmount: sellProbeOutAmount,
                spreadBps: 12
            ),
            marketSession: marketSession,
            afterHours: marketSession.map { !$0.isRegularSession } ?? false,
            market: market,
            stockVsToken: stockVsToken
        )
    }

    func chart(symbol: String, range: AssetChartRange) async throws -> AssetChartDTO {
        chartCalls.append(range)
        var answer = points[range] ?? Self.series(from: 100, to: 110)
        if var queued = scripted[range], !queued.isEmpty {
            let next = queued.removeFirst()
            scripted[range] = queued
            answer = next.points
            try? await Task.sleep(for: next.delay)
        } else if let delay = delays[range] {
            try? await Task.sleep(for: delay)
        }
        if let chartError { throw chartError }
        return AssetChartDTO(
            points: answer,
            emptyReason: emptyReason[range],
            previousCloseUsdcMicros: previousCloseUsdcMicros,
            range: echoedRange ?? range,
            source: source,
            basis: basis,
            basisSymbol: basisSymbol
        )
    }

    static func series(from first: Int64, to last: Int64) -> [AssetChartPointDTO] {
        [
            AssetChartPointDTO(timestamp: 1_000, priceUsdcMicros: first * 1_000_000),
            AssetChartPointDTO(timestamp: 2_000, priceUsdcMicros: last * 1_000_000),
        ]
    }

    /// What the model should end up holding for a stub answer.
    static func expected(
        _ range: AssetChartRange,
        from first: Int64,
        to last: Int64,
        previousClose: Int64? = nil
    ) -> AssetChartSeries {
        AssetChartSeries(
            range: range,
            points: series(from: first, to: last),
            previousCloseUsdcMicros: previousClose
        )
    }
}

@MainActor
struct AssetDetailModelTests {
    /// The bug: with no chart state the screen showed "Price history is not available yet."
    /// while the first request was still in flight.
    @Test func chartStartsLoadingRatherThanEmpty() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        #expect(model.chartState == .loading)

        await model.loadChart(range: .oneDay)
        #expect(model.chartState == .series(StubAssetDetailDataSource.expected(.oneDay, from: 100, to: 110)))
    }

    @Test func aFailedChartIsRetryableInsteadOfLookingEmpty() async throws {
        let source = StubAssetDetailDataSource()
        source.chartError = Monaco.MonacoAPIError.httpStatus(500)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)

        #expect(model.chartState == .failed)
    }

    @Test func aShortSeriesReadsAsEmptyNotAsAChart() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneWeek] = [AssetChartPointDTO(timestamp: 1_000, priceUsdcMicros: 1)]
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        model.range = .oneWeek

        await model.loadChart(range: .oneWeek)

        #expect(model.chartState == .empty(reason: nil))
    }

    /// The reason is the whole answer for 3M and 1Y on a B20 token: the Chainlink feed
    /// the chart is drawn from is weeks old, so those windows have no history and never
    /// will until it ages. An unexplained blank box reads as a broken app.
    ///
    /// It travels on the series rather than beside it, so it goes through the same
    /// per-range sequence check the curve does — see
    /// `aLateEmptyReasonCannotCaptionANewerWindow`.
    @Test func anEmptyWindowSurfacesTheServersReason() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneYear] = []
        source.emptyReason[.oneYear] = "Only on-chain since 5 Aug 2026"
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        model.range = .oneYear

        await model.loadChart(range: .oneYear)

        #expect(model.chartState == .empty(reason: "Only on-chain since 5 Aug 2026"))
    }

    /// The catch-all only repeats what an empty chart already says, so it is dropped
    /// before it ever reaches a slot. `AssetChartDTO.emptyMessage` is where that is
    /// decided; this pins that the model reads it and not the raw field.
    @Test func theCatchAllReasonIsNotWorthShowing() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.threeMonths] = []
        source.emptyReason[.threeMonths] = AssetChartDTO.genericEmptyReason
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        model.range = .threeMonths

        await model.loadChart(range: .threeMonths)

        #expect(model.chartState == .empty(reason: nil))
    }

    /// A range that has data still draws a real curve, reason or no reason: the field
    /// is about a window the server could not fill, and a filled one must ignore it.
    @Test func aRangeWithDataStillDrawsItsCurve() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        source.emptyReason[.oneDay] = "Only on-chain since 5 Aug 2026"
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)

        let series = try #require(model.series)
        #expect(series.points.count == 2)
        #expect(series.isDrawable)
        #expect(model.chartState != .empty(reason: "Only on-chain since 5 Aug 2026"))
    }

    /// The reason is carried by the series, so the per-range sequence guards it too: a
    /// slow 1Y read must not caption the 1D window the member has since tapped to.
    @Test func aLateEmptyReasonCannotCaptionANewerWindow() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneYear] = []
        source.emptyReason[.oneYear] = "Only on-chain since 5 Aug 2026"
        source.delays[.oneYear] = .milliseconds(400)
        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        model.range = .oneYear
        let slow = Task { await model.loadChart(range: .oneYear) }
        try await Task.sleep(for: .milliseconds(50))
        model.range = .oneDay
        await model.loadChart(range: .oneDay)
        _ = await slow.value

        #expect(model.chartState == .series(StubAssetDetailDataSource.expected(.oneDay, from: 100, to: 110)))
        // The year's own slot still keeps its reason, so tapping back is instant.
        #expect(model.charts[.oneYear] == .empty(reason: "Only on-chain since 5 Aug 2026"))
    }

    /// The bug: a slow 1M response overwrote the 1D curve the user had already switched to.
    @Test func aSlowRangeCannotRepaintTheRangeOnScreen() async throws {
        let source = StubAssetDetailDataSource()
        source.delays[.oneMonth] = .milliseconds(400)
        source.points[.oneMonth] = StubAssetDetailDataSource.series(from: 50, to: 60)
        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        model.range = .oneMonth
        let slow = Task { await model.loadChart(range: .oneMonth) }
        try await Task.sleep(for: .milliseconds(50))
        model.range = .oneDay
        await model.loadChart(range: .oneDay)
        _ = await slow.value

        #expect(model.range == .oneDay)
        #expect(model.chartState == .series(StubAssetDetailDataSource.expected(.oneDay, from: 100, to: 110)))
        // The late month series is kept, in its own slot, so switching back is instant.
        #expect(model.charts[.oneMonth] == .series(StubAssetDetailDataSource.expected(.oneMonth, from: 50, to: 60)))
    }

    /// The bug: the header always showed the 24h move, unlabelled, under a 1W or 1M curve.
    ///
    /// The curve is the token's here so the dollar leg is a figure this screen is allowed
    /// to show: the hero is the token's mark, and dollars are only shown when the curve is
    /// in that same unit. The window and its label are what this test is about, and they
    /// do not depend on the basis.
    @Test func theHeaderFigureMeasuresTheWindowTheCurveDraws() async throws {
        let source = StubAssetDetailDataSource()
        source.basis = .token
        source.basisSymbol = "AAPLc"
        source.points[.oneWeek] = StubAssetDetailDataSource.series(from: 100, to: 110)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        model.range = .oneWeek

        await model.loadDetail()
        await model.loadChart(range: .oneWeek)

        let move = try #require(model.move)
        #expect(move.label == "Past week")
        #expect(PercentReturnFormatter.format(move.ratio) == "+10.0%")
        #expect(move.dollars == "10.00")
    }

    @Test func withoutACurveTheFigureSaysWhichPeriodItMeasured() async throws {
        let source = StubAssetDetailDataSource()
        source.chartError = Monaco.MonacoAPIError.httpStatus(500)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        model.range = .oneMonth

        await model.loadDetail()
        await model.loadChart(range: .oneMonth)

        let move = try #require(model.move)
        // The share's day move, said to be the share's: beside the token's price an unlabelled
        // "Past day" read as the token's move.
        #expect(move.label == "AAPL day move")
        #expect(PercentReturnFormatter.format(move.ratio) == "+5.0%")
        // Nothing measured a dollar move, so the pill shows a percent alone.
        #expect(move.dollars == nil)
    }

    @Test func aDayMoveWithoutItsBasisIsNotShownUnderTheTokenPrice() async throws {
        let source = StubAssetDetailDataSource()
        source.chartError = Monaco.MonacoAPIError.httpStatus(500)
        source.change24hBasis = nil
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()
        await model.loadChart(range: .oneDay)

        #expect(model.move == nil)
    }

    /// The gap the data stage recorded: the 1D move measured from the first pre-market
    /// bar. The day change every broker shows is measured against the previous
    /// session's close.
    @Test func theDayChangeIsMeasuredAgainstThePreviousClose() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        source.previousCloseUsdcMicros = 200_000_000
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)

        let move = try #require(model.move)
        #expect(PercentReturnFormatter.format(move.ratio) == "−45.0%")
        #expect(model.curveDirection == .down)
        #expect(try #require(model.series).drawsBaselineRule)
    }

    /// The same number under a week chip would be meaningless, so it is not used there.
    @Test func aWeekChartIgnoresThePreviousClose() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneWeek] = StubAssetDetailDataSource.series(from: 100, to: 110)
        source.previousCloseUsdcMicros = 200_000_000
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        model.range = .oneWeek

        await model.loadChart(range: .oneWeek)

        #expect(PercentReturnFormatter.format(try #require(model.move).ratio) == "+10.0%")
        #expect(try #require(model.series).drawsBaselineRule == false)
    }

    // MARK: - Which day the day chart is

    /// Friday 2026-09-25 at 15:50 and 15:55 ET: two bars of Friday's session.
    private static let fridaySession = [
        AssetChartPointDTO(timestamp: 1_790_365_800, priceUsdcMicros: 230_000_000),
        AssetChartPointDTO(timestamp: 1_790_366_100, priceUsdcMicros: 231_000_000),
    ]

    /// The other gap the data stage recorded: on a weekend the backend's 1D window is
    /// Friday's session, and the header still said "Past day".
    @Test func onASaturdayTheDayMoveIsNamedForFridaysSession() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneDay] = Self.fridaySession
        source.previousCloseUsdcMicros = 229_000_000
        source.source = .benchmarks
        source.basis = .underlying
        source.basisSymbol = "AAPL"
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        model.now = { MarketSampleData.saturdayNoon }

        await model.loadChart(range: .oneDay)

        let move = try #require(model.move)
        #expect(move.label != "Past day")
        #expect(move.label.contains("Fri"), "label = \(move.label)")
        #expect(move.basisSymbol == "AAPL")
        #expect(model.chartAccessibilitySummary.contains("on Fri"), "summary = \(model.chartAccessibilitySummary)")
    }

    @Test func duringTheSessionTheDayMoveIsToday() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneDay] = Self.fridaySession
        source.source = .benchmarks
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        model.now = { Date(timeIntervalSince1970: 1_790_366_160) }

        await model.loadChart(range: .oneDay)

        #expect(model.move?.label == "Today")
    }

    /// The Hermes sampler and the Chainlink rounds are a rolling 24 hours, which is
    /// what "Past day" says.
    @Test func aRollingDayIsStillThePastDay() async throws {
        for fallback in [AssetChartSource.hermes, .chainlink] {
            let source = StubAssetDetailDataSource()
            source.points[.oneDay] = Self.fridaySession
            source.source = fallback
            let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
            model.now = { MarketSampleData.saturdayNoon }

            await model.loadChart(range: .oneDay)

            #expect(model.move?.label == "Past day", "\(fallback)")
        }
    }

    // MARK: - Failure and sign-out

    /// The bug: a missing token returned before the loading flag was cleared, leaving the
    /// screen on a bare spinner with no retry.
    @Test func aMissingTokenEndsInAFailedState() async throws {
        let source = StubAssetDetailDataSource()
        source.detailError = Monaco.MonacoAPIError.missingAccessToken
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()

        #expect(model.detailState == .failed)
    }

    @Test func rejectedSessionAsksTheViewToSignOut() async throws {
        let source = StubAssetDetailDataSource()
        source.detailError = RejectedSession(token: "token-1")
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()

        #expect(model.sessionExpired)
        // The view signs out through the token that read carried, not whatever is current.
        #expect(model.rejectedSession == RejectedSession(token: "token-1"))
        #expect(model.detailState == .loading)
    }

    /// A background poll that is rejected ends the session the same way, with the token
    /// that poll carried.
    @Test func aRejectedPollAlsoNamesItsToken() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        await model.loadDetail()

        source.detailError = RejectedSession(token: "token-2")
        await #expect(throws: RejectedSession.self) { try await model.refreshDetail() }

        #expect(model.rejectedSession == RejectedSession(token: "token-2"))
    }

    @Test func buyIsBlockedOnlyWhenTheStockIsKnownToBeUnbuyable() async throws {
        let source = StubAssetDetailDataSource()
        source.routable = false
        source.liquidityRoutable = false
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        #expect(model.canBuy)

        await model.loadDetail()
        #expect(!model.canBuy)
    }

    /// A quote probe that errored (a rate limit, a timeout) reports `liquidity.routable = false`
    /// for that one response, though it says nothing about the pool. So the probe snippet
    /// cannot decide the button: the stock-level verdict does.
    @Test func aFailedQuoteProbeDoesNotBlockTheBuy() async throws {
        let source = StubAssetDetailDataSource()
        source.routable = true
        source.liquidityRoutable = false
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()

        #expect(model.canBuy)
    }

    /// A refresh that fails keeps the curve already drawn rather than blanking it to an error.
    @Test func aFailedRefreshKeepsTheCurveAlreadyDrawn() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)
        let drawn = StubAssetDetailDataSource.expected(.oneDay, from: 100, to: 110)
        #expect(model.chartState == .series(drawn))

        source.chartError = Monaco.MonacoAPIError.httpStatus(500)
        await model.loadChart(range: .oneDay)

        #expect(model.chartState == .series(drawn), "a failed re-read must not throw the curve away")
    }

    /// A range that has nothing on screen yet still fails visibly.
    @Test func aFirstChartFailureIsRetryable() async throws {
        let source = StubAssetDetailDataSource()
        source.chartError = Monaco.MonacoAPIError.httpStatus(500)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneWeek)
        #expect(model.charts[.oneWeek] == .failed)

        source.chartError = nil
        await model.loadChart(range: .oneWeek)
        #expect(model.charts[.oneWeek] == .series(StubAssetDetailDataSource.expected(.oneWeek, from: 100, to: 110)))
    }

    /// The review finding: `String(someDouble)` drops into scientific notation under 1e-4, and
    /// `MonacoTheme.signed` reads the digits of "5e-05" as "505" and tints a flat move as a gain.
    @Test func aTinyMoveIsWrittenInFixedPoint() async throws {
        let source = StubAssetDetailDataSource()
        // +0.005%, small enough that Double's own description goes exponential.
        source.points = [.oneDay: [
            AssetChartPointDTO(timestamp: 1_000, priceUsdcMicros: 100_000_000),
            AssetChartPointDTO(timestamp: 2_000, priceUsdcMicros: 100_005_000),
        ]]
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)

        let ratio = try #require(model.move?.ratio)
        #expect(!ratio.lowercased().contains("e"), "got \(ratio)")
        #expect(model.move?.direction == .up)
    }

    /// #298 is about the figure and the curve agreeing. A flat move is muted, so the curve must
    /// not be painted profit-green off a separate `last >= first` of its own.
    @Test func aFlatMoveIsNeitherAGainNorALoss() async throws {
        let source = StubAssetDetailDataSource()
        source.points = [.oneDay: StubAssetDetailDataSource.series(from: 100, to: 100)]
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)

        #expect(model.move?.direction == .flat)
        #expect(model.curveDirection == .flat)
    }

    @Test func aFallingWindowReadsAsALoss() async throws {
        let source = StubAssetDetailDataSource()
        source.points = [.oneDay: StubAssetDetailDataSource.series(from: 110, to: 100)]
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)

        #expect(model.move?.direction == .down)
    }

    // MARK: - What VoiceOver reads for the curve

    /// The headline is the token's own mark ($185 here), but the series can be the underlying
    /// share's price ($250 to $275 here). A series that does not say which instrument it is
    /// gets no dollar figures read next to a price in another unit; the move is a ratio and
    /// holds.
    @Test func theChartSummarySpeaksTheMoveNotDollarsInAnotherUnit() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneWeek] = StubAssetDetailDataSource.series(from: 250, to: 275)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        model.range = .oneWeek

        await model.loadDetail()
        await model.loadChart(range: .oneWeek)

        let summary = model.chartAccessibilitySummary
        #expect(summary == "One week price history. +10.0% past week.")
        #expect(!summary.contains("$"), "got \(summary)")
    }

    /// Once the series names its instrument, the low and the high are read with it.
    @Test func aNamedCurveReadsItsRangeInItsOwnUnit() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneWeek] = StubAssetDetailDataSource.series(from: 250, to: 275)
        source.basis = .underlying
        source.basisSymbol = "AAPL"
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        model.range = .oneWeek

        await model.loadChart(range: .oneWeek)

        #expect(
            model.chartAccessibilitySummary
                == "One week price history. +10.0% past week. Low $250.00, high $275.00, AAPL on its home exchange."
        )
    }

    // MARK: - A series built for another window

    /// The data layer echoes the range it built a series for. One built for another
    /// window is not an answer to the question this chip asked.
    @Test func aSeriesBuiltForAnotherRangeIsRefusedRatherThanDrawn() async throws {
        let source = StubAssetDetailDataSource()
        source.echoedRange = .oneYear
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)

        #expect(model.charts[.oneDay] == .failed)
        #expect(model.series == nil)
    }

    /// A backend persistently answering the wrong window fails every read, so the quiet
    /// poll has to be able to see it. `refreshChart` throwing is what `pollWhileVisible`
    /// backs off on; while this returned nil the poll kept firing at the full rate into a
    /// slot it could never fill.
    @Test func aMismatchedQuietReadIsReportedSoThePollCanBackOff() async throws {
        let source = StubAssetDetailDataSource()
        source.echoedRange = .oneYear
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)
        #expect(model.charts[.oneDay] == .failed)

        await #expect(throws: AssetDetailModel.ChartLoadError.rangeMismatch(requested: .oneDay)) {
            try await model.refreshChart()
        }
    }

    /// A range the server does echo back still refreshes silently — the throw above is
    /// about the mismatch, not about every quiet read.
    @Test func aMatchingQuietReadDoesNotThrow() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)
        try await model.refreshChart()
    }

    @Test func aMismatchedRefreshKeepsTheCurveAlreadyDrawn() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)
        let drawn = StubAssetDetailDataSource.expected(.oneDay, from: 100, to: 110)

        source.echoedRange = .all
        await model.loadChart(range: .oneDay)

        #expect(model.chartState == .series(drawn))
    }

    // MARK: - Scrubbing

    @Test func scrubbingMovesThePriceAndTheFigureToThePointUnderTheFinger() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()
        await model.loadChart(range: .oneDay)
        #expect(model.heroPriceUsdcMicros == 185_000_000)
        #expect(!model.isScrubbing)

        model.scrubbedIndex = 0

        #expect(model.isScrubbing)
        #expect(model.heroPriceUsdcMicros == 100_000_000)
        let move = try #require(model.move)
        // The first sample is the baseline here, so the move to it is zero — and the
        // label is that sample's own time, not the name of the window.
        #expect(PercentReturnFormatter.format(move.ratio) == "0.0%")
        #expect(move.label != AssetChartRange.oneDay.moveLabel)

        model.scrubbedIndex = nil
        #expect(model.heroPriceUsdcMicros == 185_000_000)
        #expect(model.move?.label == AssetChartRange.oneDay.moveLabel)
    }

    /// While a finger is on the share's curve the hero shows the share's price, not the
    /// token's mark, and the line above it says so.
    @Test func aScrubbedSharePriceSaysItIsTheShare() async throws {
        let source = StubAssetDetailDataSource()
        source.basis = .underlying
        source.basisSymbol = "AAPL"
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        await model.loadDetail()
        await model.loadChart(range: .oneDay)
        #expect(model.heroPriceCaption == nil, "the live hero is the token's own mark")

        model.scrubbedIndex = 1

        #expect(model.heroPriceCaption == "AAPL on its home exchange")
    }

    /// A series the backend did not name could be either instrument. The scrubbed price must
    /// not pass for the token's mark, so the name line says only that it is the chart's.
    @Test func aScrubbedUnnamedSampleIsCaptionedAsTheChartsPrice() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        await model.loadDetail()
        await model.loadChart(range: .oneDay)
        #expect(model.heroPriceCaption == nil)

        model.scrubbedIndex = 0

        #expect(model.heroPriceCaption == AssetDetailModel.unnamedChartPriceCaption)
        #expect(model.heroPriceCaption == "Chart price")
    }

    /// A sample of the token's own Chainlink rounds is the same instrument as the hero.
    @Test func aScrubbedTokenRoundNeedsNoCaption() async throws {
        let source = StubAssetDetailDataSource()
        source.basis = .token
        source.basisSymbol = "AAPLc"
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        await model.loadChart(range: .oneDay)

        model.scrubbedIndex = 1

        #expect(model.heroPriceCaption == nil)
    }

    /// A line that changed colour under the finger would read as the price having
    /// moved. The curve keeps the window's verdict; only the header follows the scrub.
    @Test func theCurveKeepsItsOwnVerdictWhileScrubbing() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)
        model.scrubbedIndex = 0

        #expect(model.move?.direction == .flat)
        #expect(model.curveDirection == .up)
    }

    @Test func changingRangeDropsTheScrub() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)
        model.scrubbedIndex = 1
        #expect(model.isScrubbing)

        model.range = .oneMonth

        #expect(model.scrubbedIndex == nil)
    }

    /// A shorter series must not leave the header reading a sample that is gone.
    @Test func aShorterSeriesDropsAScrubPastItsEnd() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneDay] = [
            AssetChartPointDTO(timestamp: 1_000, priceUsdcMicros: 100_000_000),
            AssetChartPointDTO(timestamp: 2_000, priceUsdcMicros: 101_000_000),
            AssetChartPointDTO(timestamp: 3_000, priceUsdcMicros: 102_000_000),
        ]
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)
        model.scrubbedIndex = 2
        #expect(model.heroPriceUsdcMicros == 102_000_000)

        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        try await model.refreshChart()

        #expect(model.scrubbedIndex == nil)
    }

    // MARK: - The live hero

    /// The flash says "that number changed while you were looking at it". The first
    /// load changed nothing — there was nothing there before.
    @Test func theFirstLoadDoesNotFlash() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()

        #expect(model.priceTick == nil)
        #expect(model.heroTick == nil)
    }

    @Test func aPollThatMovesThePriceRaisesATickWithItsDirection() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        await model.loadDetail()

        source.priceUsdcMicros = 186_000_000
        try await model.refreshDetail()
        let up = try #require(model.priceTick)
        #expect(up.direction == .up)
        #expect(model.heroPriceUsdcMicros == 186_000_000)

        source.priceUsdcMicros = 184_000_000
        try await model.refreshDetail()
        let down = try #require(model.priceTick)
        #expect(down.direction == .down)
        // Two ticks are two events, however they are signed.
        #expect(down.sequence > up.sequence)
    }

    @Test func aPollThatChangesNothingDoesNotFlash() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        await model.loadDetail()

        try await model.refreshDetail()

        #expect(model.priceTick == nil)
    }

    /// The race the 10 s poll opened: the first load stalls (a slow Base RPC), a poll issued
    /// after it answers first with a newer mark, and then the stalled load lands. The older
    /// mark must not move the hero price backwards, nor flash it the wrong way.
    @Test func aSlowFirstLoadCannotOverwriteANewerPoll() async throws {
        let source = StubAssetDetailDataSource()
        source.scriptedDetail = [
            .init(delay: .milliseconds(400), price: 185_000_000),
            .init(delay: .milliseconds(20), price: 186_000_000),
        ]
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        let slow = Task { await model.loadDetail() }
        try await Task.sleep(for: .milliseconds(50))
        try await model.refreshDetail()
        #expect(model.heroPriceUsdcMicros == 186_000_000)
        let tickAfterPoll = model.priceTick

        await slow.value

        #expect(source.detailCalls == 2)
        #expect(model.heroPriceUsdcMicros == 186_000_000, "the older mark landed on top of the newer one")
        #expect(model.priceTick == tickAfterPoll)
    }

    /// The same holds between two polls, and between a Retry and a poll.
    @Test func aLateOlderPollCannotOverwriteANewerRetry() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        await model.loadDetail()
        source.scriptedDetail = [
            .init(delay: .milliseconds(400), price: 184_000_000),
            .init(delay: .milliseconds(20), price: 187_000_000),
        ]

        let stalePoll = Task { try await model.refreshDetail() }
        try await Task.sleep(for: .milliseconds(50))
        await model.loadDetail()
        let up = try #require(model.priceTick)
        #expect(up.direction == .up)

        try await stalePoll.value

        #expect(model.heroPriceUsdcMicros == 187_000_000)
        #expect(model.priceTick == up)
    }

    /// A failure that a newer read has already overtaken says nothing about the screen now:
    /// it must not fail a loaded screen, sign the member out, or back the poll off.
    @Test func aSupersededFailureIsDropped() async throws {
        let source = StubAssetDetailDataSource()
        source.scriptedDetail = [
            .init(delay: .milliseconds(300), price: nil, error: RejectedSession(token: "old-token")),
            .init(delay: .milliseconds(20), price: 185_000_000),
        ]
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        let stale = Task { try await model.refreshDetail() }
        try await Task.sleep(for: .milliseconds(50))
        await model.loadDetail()
        #expect(model.detail != nil)

        // Superseded, so the poll loop is not told to back off either.
        try await stale.value
        #expect(model.detail != nil)
        #expect(model.rejectedSession == nil)
    }

    /// A new sign-in starts with no rejection on record, so the screen reads again.
    @Test func aNewSessionClearsTheRejection() async throws {
        let source = StubAssetDetailDataSource()
        source.detailError = RejectedSession(token: "token-1")
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        await model.loadDetail()
        #expect(model.sessionExpired)

        source.detailError = nil
        model.beginSession()
        await model.loadDetail()

        #expect(!model.sessionExpired)
        #expect(model.detail != nil)
    }

    /// A poll nobody asked for must never put an error in front of the member. It does
    /// report the failure to the poll loop, which is what backs the loop off.
    @Test func aFailedPollLeavesTheScreenExactlyAsItWas() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        await model.loadDetail()
        let loaded = model.detailState

        source.detailError = Monaco.MonacoAPIError.httpStatus(500)
        await #expect(throws: (any Error).self) { try await model.refreshDetail() }

        #expect(model.detailState == loaded)
    }

    /// A quiet re-read that comes back empty is a source hiccup, not news.
    @Test func aQuietRefreshThatComesBackEmptyKeepsTheCurve() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)
        let drawn = StubAssetDetailDataSource.expected(.oneDay, from: 100, to: 110)

        source.points[.oneDay] = []
        try await model.refreshChart()

        #expect(model.chartState == .series(drawn))
    }

    /// The chip's spinner is driven by this, not by `ChartState.loading`: a range that
    /// already has a curve keeps showing it while it re-reads.
    @Test func aRangeInFlightIsNamedSoItsChipCanSaySo() async throws {
        let source = StubAssetDetailDataSource()
        source.delays[.oneDay] = .milliseconds(200)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        let load = Task { await model.loadChart(range: .oneDay) }
        try await Task.sleep(for: .milliseconds(50))
        #expect(model.isLoadingCurrentRange)

        _ = await load.value
        #expect(!model.isLoadingCurrentRange)
        #expect(model.loadingRanges.isEmpty)
    }

    // MARK: - Market session

    @Test func theSessionChipReadsTheStatusBlock() async throws {
        let source = StubAssetDetailDataSource()
        source.market = MarketSampleData.sessionAfterHours
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()

        #expect(model.sessionChip?.title == "After hours")
        #expect(model.sessionChip?.detail == "Token still trades on Base")
        #expect(!model.isMarketLive)
    }

    /// The token line is a claim about the pools, so it rests on this read's Kyber probe.
    @Test func withoutARouteTheChipDoesNotSayTheTokenTrades() async throws {
        let source = StubAssetDetailDataSource()
        source.market = MarketSampleData.sessionWeekend
        source.liquidityRoutable = false
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()

        #expect(model.sessionChip?.title == "Market closed")
        #expect(model.sessionChip?.detail == nil)
    }

    /// A buy route alone is half a market: the chip does not say the token trades.
    @Test func aBuyRouteWithoutASellRouteDoesNotSayTheTokenTrades() async throws {
        let source = StubAssetDetailDataSource()
        source.market = MarketSampleData.sessionAfterHours
        source.liquidityRoutable = true
        source.sellProbeOutAmount = nil
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()

        #expect(model.sessionChip?.title == "After hours")
        #expect(model.sessionChip?.detail == nil)
    }

    /// A sell route that pays nothing is not a way out. The backend fills
    /// `sellProbeOutAmount` for any routable quote that came back with an amount, so a
    /// whole token priced at nothing ships as "0" — and the same response's own pricing
    /// step rejects it, reporting the token leg unavailable. The chip has to agree with
    /// that rather than read the field's presence as a market.
    @Test func aSellRouteThatPaysNothingDoesNotSayTheTokenTrades() async throws {
        let source = StubAssetDetailDataSource()
        source.market = MarketSampleData.sessionAfterHours
        source.liquidityRoutable = true
        source.sellProbeOutAmount = "0"
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()

        #expect(model.sessionChip?.title == "After hours")
        #expect(model.sessionChip?.detail == nil)
    }

    /// A backend that sends only the mirrored session still gets a chip.
    @Test func aMirroredSessionIsEnoughForAChip() async throws {
        let source = StubAssetDetailDataSource()
        source.marketSession = .open
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()

        #expect(model.sessionChip?.title == "Market open")
        #expect(model.isMarketLive)
    }

    /// An older backend says nothing about the market. Nothing is what the screen shows.
    @Test func noSessionMeansNoChipAndNoPulse() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()

        #expect(model.sessionChip == nil)
        #expect(!model.isMarketLive)
    }

    // MARK: - A silent poll is silent

    /// The bug: the two-minute background chart re-read inserted into `loadingRanges`
    /// like any other load, so the selected chip sprouted a spinner — and said
    /// "Loading" to VoiceOver — every two minutes for a refresh nobody asked for.
    /// The spinner means "you tapped this and it has not arrived".
    @Test func aQuietReReadNeverSpinsTheChip() async throws {
        let source = StubAssetDetailDataSource()
        source.delays[.oneDay] = .milliseconds(200)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        let refresh = Task { try await model.refreshChart() }
        try await Task.sleep(for: .milliseconds(60))

        #expect(model.loadingRanges.isEmpty)
        #expect(!model.isLoadingCurrentRange)
        try await refresh.value
        #expect(model.loadingRanges.isEmpty)
    }

    /// And it never puts an error in front of the member either, even on a range that
    /// has nothing drawn yet: the member did not ask for this read. The failure goes to
    /// the poll loop only.
    @Test func aQuietReReadThatFailsLeavesTheRangeAlone() async throws {
        let source = StubAssetDetailDataSource()
        source.chartError = Monaco.MonacoAPIError.httpStatus(500)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await #expect(throws: (any Error).self) { try await model.refreshChart() }

        #expect(model.chartState == .loading)
        #expect(model.charts[.oneDay] == nil)
    }

    /// The bug: `defer { loadingRanges.remove(range) }` had no in-flight guard, so a
    /// quiet poll and a tap on one range raced — whichever returned first stopped the
    /// spinner while the other request was still out.
    @Test func theChipKeepsSpinningUntilTheLastAskedForRequestReturns() async throws {
        let source = StubAssetDetailDataSource()
        source.scripted[.oneDay] = [
            (delay: .milliseconds(400), points: StubAssetDetailDataSource.series(from: 100, to: 110)),
            (delay: .milliseconds(80), points: StubAssetDetailDataSource.series(from: 200, to: 210)),
        ]
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        let slow = Task { await model.loadChart(range: .oneDay) }
        try await Task.sleep(for: .milliseconds(30))
        let quick = Task { await model.loadChart(range: .oneDay) }
        _ = await quick.value

        // The second request is home; the first is still out, so the chip is still
        // working.
        #expect(model.isLoadingCurrentRange)

        _ = await slow.value
        #expect(!model.isLoadingCurrentRange)
    }

    /// And the older request, arriving last, must not repaint the slot the newer one
    /// already wrote.
    @Test func aLateOlderResponseDoesNotOverwriteTheNewerOne() async throws {
        let source = StubAssetDetailDataSource()
        source.scripted[.oneDay] = [
            (delay: .milliseconds(400), points: StubAssetDetailDataSource.series(from: 100, to: 110)),
            (delay: .milliseconds(80), points: StubAssetDetailDataSource.series(from: 200, to: 210)),
        ]
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        let stale = Task { await model.loadChart(range: .oneDay) }
        try await Task.sleep(for: .milliseconds(30))
        await model.loadChart(range: .oneDay)
        let newest = StubAssetDetailDataSource.expected(.oneDay, from: 200, to: 210)
        #expect(model.chartState == .series(newest))

        _ = await stale.value

        #expect(model.chartState == .series(newest), "the older answer landed on top of the newer one")
    }

    // MARK: - Whose number is under the price

    /// The hero price is the B20 token's Chainlink mark and every Pyth history source
    /// serves the underlying equity, so a figure folded from the curve is a different
    /// instrument from the price it sits under — the two cannot be reconciled by
    /// subtraction. The pill has to say which instrument it is about.
    @Test func aFigureFoldedFromTheCurveSaysWhichInstrumentItIsAbout() async throws {
        let source = StubAssetDetailDataSource()
        source.basis = .underlying
        source.basisSymbol = "AAPL"
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()
        await model.loadChart(range: .oneDay)

        let move = try #require(model.move)
        #expect(move.basisSymbol == "AAPL")
        // The token's own ticker is also "AAPL", so the tag says it is the share.
        #expect(move.basisTag == "AAPL share")
        #expect(move.basisCaption == "AAPL on its home exchange")
    }

    /// Under the token's live mark the share's dollars would read as the token's, and the
    /// multiplier makes them different numbers. The ratio holds across it; the dollars come
    /// back only while the hero itself shows the share's price.
    @Test func theSharesDollarsAreOnlyShownUnderTheSharesPrice() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        source.basis = .underlying
        source.basisSymbol = "AAPL"
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        await model.loadDetail()
        await model.loadChart(range: .oneDay)

        let live = try #require(model.move)
        #expect(live.dollars == nil)
        #expect(PercentReturnFormatter.format(live.ratio) == "+10.0%")

        model.scrubbedIndex = 1
        #expect(model.heroPriceCaption == "AAPL on its home exchange")
        #expect(model.move?.dollars == "10.00")
    }

    /// A curve that is the token's own rounds is in the hero's unit, so its dollars stay.
    @Test func theTokensOwnCurveKeepsItsDollars() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        source.basis = .token
        source.basisSymbol = "AAPLc"
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        await model.loadChart(range: .oneDay)

        #expect(model.move?.dollars == "10.00")
        #expect(model.move?.basisTag == nil)
    }

    /// Scrubbing does not change whose numbers these are.
    @Test func aScrubbedFigureCarriesTheSameInstrument() async throws {
        let source = StubAssetDetailDataSource()
        source.basis = .underlying
        source.basisSymbol = "AAPL"
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()
        await model.loadChart(range: .oneDay)
        model.scrubbedIndex = 0

        #expect(model.move?.basisSymbol == "AAPL")
    }

    /// With no curve the figure is the backend's day move, which on Base is the share's
    /// (Pyth, against its previous close), not the token's. The label names the share,
    /// so the pill carries no second tag; VoiceOver still hears the long form.
    @Test func theFallbackFigureIsTheSharesAndSaysSo() async throws {
        let source = StubAssetDetailDataSource()
        source.basis = .underlying
        source.basisSymbol = "AAPL"
        source.chartError = Monaco.MonacoAPIError.httpStatus(500)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()
        await model.loadChart(range: .oneDay)

        let move = try #require(model.move)
        #expect(move.label == "AAPL day move")
        #expect(move.basisSymbol == nil)
        #expect(move.basisCaption == "AAPL on its home exchange")
        #expect(model.curveDirection == .up)
    }

    /// A curve the backend says is the token's is the same instrument as the hero, so
    /// it needs no tag either — and one it will not name gets no guess.
    @Test func aTokenCurveAndAnUnnamedCurveBothGoUntagged() async throws {
        let token = StubAssetDetailDataSource()
        token.basis = .token
        token.basisSymbol = "AAPLc"
        let tokenModel = AssetDetailModel(symbol: "AAPLc", dataSource: token)
        await tokenModel.loadChart(range: .oneDay)
        #expect(tokenModel.move?.basisSymbol == nil)

        let unnamed = AssetDetailModel(symbol: "AAPLc", dataSource: StubAssetDetailDataSource())
        await unnamed.loadChart(range: .oneDay)
        #expect(unnamed.move?.basisSymbol == nil)
    }

    /// Going untagged is not the same as being in the hero's unit, and only the hero's
    /// unit earns dollars.
    ///
    /// `loadDetail` first, so the token's Chainlink mark is actually in the hero — without
    /// it there is no price for a dollar figure to sit under and the bug cannot be seen.
    /// A curve the backend names as the token's is that same per-token unit, so its
    /// dollars are a real subtraction. A curve the backend does not name is not: an
    /// unrecognised basis decodes to `.unknown` and a missing one stays nil, and neither
    /// says "token". Both must come back ratio-only, the way the share's curve does.
    @Test func onlyTheTokensOwnCurveGetsDollarsUnderTheTokensMark() async throws {
        func model(basis: MarketPriceBasis?, symbol: String?) async -> AssetDetailModel {
            let source = StubAssetDetailDataSource()
            source.basis = basis
            source.basisSymbol = symbol
            let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
            await model.loadDetail()
            await model.loadChart(range: .oneDay)
            return model
        }

        // The hero is the token's mark, and the curve is the token's too: same unit.
        let token = await model(basis: .token, symbol: "AAPLc")
        #expect(token.detail?.priceUsdcMicros != nil)
        #expect(token.move?.dollars != nil)

        // The share's curve, per share, under a per-token price: ratio only.
        let underlying = await model(basis: .underlying, symbol: "AAPL")
        #expect(underlying.move?.ratio != nil)
        #expect(underlying.move?.dollars == nil)

        // A basis the backend never sent. Unit unknown, so no dollars.
        let unnamed = await model(basis: nil, symbol: nil)
        #expect(unnamed.move?.ratio != nil)
        #expect(unnamed.move?.dollars == nil)

        // A basis a later backend added that this build does not know. `MarketPriceBasis`
        // decodes it to `.unknown` on purpose, so this is a live path, not a hypothetical.
        let unknown = await model(basis: .unknown, symbol: "AAPL-NEW")
        #expect(unknown.move?.ratio != nil)
        #expect(unknown.move?.dollars == nil)
    }

    /// An unrecognised basis string really does land on `.unknown` rather than failing
    /// the decode — the premise the test above rests on.
    @Test func anUnrecognisedBasisDecodesToUnknown() throws {
        let decoded = try JSONDecoder().decode(MarketPriceBasis.self, from: Data("\"wrapped_share\"".utf8))
        #expect(decoded == .unknown)
    }

    /// Over a weekend the hero is a number that has not moved since Friday, rolling its
    /// digits next to a chip that says the token still trades on Base. Both are true —
    /// the pools trade, the mark does not — and the hero has to say which it is showing.
    @Test func aHeldMarkSaysWhenItLastPrinted() async throws {
        let source = StubAssetDetailDataSource()
        source.stockVsToken = MarketSampleData.stockVsTokenWeekend
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()

        #expect(model.heroPriceAsOf?.hasPrefix("As of ") == true)
    }

    /// A live mark carries no qualifier, and neither does a screen whose backend sends no
    /// card at all — this line is a fact off the payload, never an assumption.
    @Test func aLiveMarkAndAMissingCardBothLeaveTheHeroUnqualified() async throws {
        let live = StubAssetDetailDataSource()
        live.stockVsToken = MarketSampleData.stockVsTokenLive
        let liveModel = AssetDetailModel(symbol: "AAPLc", dataSource: live)
        await liveModel.loadDetail()
        #expect(liveModel.heroPriceAsOf == nil)

        let silent = AssetDetailModel(symbol: "AAPLc", dataSource: StubAssetDetailDataSource())
        await silent.loadDetail()
        #expect(silent.heroPriceAsOf == nil)
    }

    /// While a finger is down the price is the curve's own sample, which carries its own
    /// time in the change row. The mark's as-of line would be about a different number.
    @Test func aScrubbedHeroDropsTheMarksAsOfLine() async throws {
        let source = StubAssetDetailDataSource()
        source.stockVsToken = MarketSampleData.stockVsTokenWeekend
        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadDetail()
        await model.loadChart(range: .oneDay)
        #expect(model.heroPriceAsOf != nil)

        model.scrubbedIndex = 1
        #expect(model.heroPriceAsOf == nil)
    }

    // MARK: - Walking out of a scrub

    /// VoiceOver's adjustable action has no release, so walking forward off the last
    /// sample is the way back to the live price: the hero unpins, the change row goes
    /// back to the window's own label, and a poll can flash again.
    @Test func walkingOffTheEndOfTheCurveRestoresTheLiveHero() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        await model.loadDetail()
        await model.loadChart(range: .oneDay)

        let count = try #require(model.series).points.count
        model.scrubbedIndex = MonacoScrubStep.next(from: nil, forward: false, count: count)
        #expect(model.isScrubbing)

        model.scrubbedIndex = MonacoScrubStep.next(from: model.scrubbedIndex, forward: true, count: count)

        #expect(model.scrubbedIndex == nil)
        #expect(!model.isScrubbing)
        #expect(model.heroPriceUsdcMicros == 185_000_000)
        #expect(model.move?.label == AssetChartRange.oneDay.moveLabel)
    }

    /// The tick a poll raised while a finger was down lands when the scrub ends,
    /// rather than being lost: that is why the hero must be able to end a scrub.
    @Test func theFlashHeldBackByAScrubArrivesWhenItEnds() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        await model.loadDetail()
        await model.loadChart(range: .oneDay)
        model.scrubbedIndex = 0

        source.priceUsdcMicros = 186_000_000
        try await model.refreshDetail()
        #expect(model.heroTick == nil, "a sample from the past must not flash")

        model.scrubbedIndex = MonacoScrubStep.next(from: 0, forward: true, count: 2)
        model.scrubbedIndex = MonacoScrubStep.next(from: model.scrubbedIndex, forward: true, count: 2)

        #expect(model.scrubbedIndex == nil)
        #expect(model.heroTick != nil)
    }
}
