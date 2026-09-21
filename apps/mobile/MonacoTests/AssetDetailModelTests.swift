import MonacoCore
import Testing
@testable import Monaco

// The app target shadows these MonacoCore DTOs; pin the tests to the ones the views use.
private typealias AssetChartRange = Monaco.AssetChartRange
private typealias AssetChartDTO = Monaco.AssetChartDTO
private typealias AssetChartPointDTO = Monaco.AssetChartPointDTO
private typealias AssetDetailDTO = Monaco.AssetDetailDTO
private typealias AssetLiquidityDTO = Monaco.AssetLiquidityDTO

@MainActor
private final class StubAssetDetailDataSource: AssetDetailDataSource {
    var detailCalls = 0
    var chartCalls: [AssetChartRange] = []
    /// Per-range latency, so a slow range can answer after a faster one.
    var delays: [AssetChartRange: Duration] = [:]
    var detailError: Error?
    var chartError: Error?
    var routable = true
    var change24h: String? = "0.05"
    var priceUsdcMicros: Int64? = 185_000_000
    var marketSession: MarketSession?
    var market: MarketStatusDTO?
    var points: [AssetChartRange: [AssetChartPointDTO]] = [:]
    var previousCloseUsdcMicros: Int64?
    /// The range the server claims each series was built for. Nil echoes the request.
    var echoedRange: AssetChartRange?
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
        if let detailError { throw detailError }
        return AssetDetailDTO(
            symbol: symbol,
            name: "Apple xStock",
            solanaMint: "MintAAPL",
            routable: routable,
            priceUsdcMicros: priceUsdcMicros,
            change24h: change24h,
            liquidity: AssetLiquidityDTO(
                label: "Via Jupiter",
                routable: routable,
                buyProbeUsdcMicros: 1_000_000,
                buyProbeOutAmount: "100000000",
                sellProbeInAmount: nil,
                sellProbeOutAmount: nil,
                spreadBps: 12
            ),
            marketSession: marketSession,
            afterHours: marketSession.map { !$0.isRegularSession } ?? false,
            market: market
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
            emptyReason: nil,
            previousCloseUsdcMicros: previousCloseUsdcMicros,
            range: echoedRange ?? range,
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
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        #expect(model.chartState == .loading)

        await model.loadChart(range: .oneDay)
        #expect(model.chartState == .series(StubAssetDetailDataSource.expected(.oneDay, from: 100, to: 110)))
    }

    @Test func aFailedChartIsRetryableInsteadOfLookingEmpty() async throws {
        let source = StubAssetDetailDataSource()
        source.chartError = Monaco.MonacoAPIError.httpStatus(500)
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadChart(range: .oneDay)

        #expect(model.chartState == .failed)
    }

    @Test func aShortSeriesReadsAsEmptyNotAsAChart() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneWeek] = [AssetChartPointDTO(timestamp: 1_000, priceUsdcMicros: 1)]
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)
        model.range = .oneWeek

        await model.loadChart(range: .oneWeek)

        #expect(model.chartState == .empty)
    }

    /// The bug: a slow 1M response overwrote the 1D curve the user had already switched to.
    @Test func aSlowRangeCannotRepaintTheRangeOnScreen() async throws {
        let source = StubAssetDetailDataSource()
        source.delays[.oneMonth] = .milliseconds(400)
        source.points[.oneMonth] = StubAssetDetailDataSource.series(from: 50, to: 60)
        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        model.range = .oneMonth
        let slow = Task { await model.loadChart(range: .oneMonth) }
        try await Task.sleep(for: .milliseconds(50))
        model.range = .oneDay
        await model.loadChart(range: .oneDay)
        await slow.value

        #expect(model.range == .oneDay)
        #expect(model.chartState == .series(StubAssetDetailDataSource.expected(.oneDay, from: 100, to: 110)))
        // The late month series is kept, in its own slot, so switching back is instant.
        #expect(model.charts[.oneMonth] == .series(StubAssetDetailDataSource.expected(.oneMonth, from: 50, to: 60)))
    }

    /// The bug: the header always showed the 24h move, unlabelled, under a 1W or 1M curve.
    @Test func theHeaderFigureMeasuresTheWindowTheCurveDraws() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneWeek] = StubAssetDetailDataSource.series(from: 100, to: 110)
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)
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
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)
        model.range = .oneMonth

        await model.loadDetail()
        await model.loadChart(range: .oneMonth)

        let move = try #require(model.move)
        #expect(move.label == "Past day")
        #expect(PercentReturnFormatter.format(move.ratio) == "+5.0%")
        // Nothing measured a dollar move, so the pill shows a percent alone.
        #expect(move.dollars == nil)
    }

    /// The day change every broker shows is measured against the previous session's
    /// close, not against whatever the window's first sample happened to be.
    @Test func theDayChangeIsMeasuredAgainstThePreviousClose() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        source.previousCloseUsdcMicros = 200_000_000
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

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
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)
        model.range = .oneWeek

        await model.loadChart(range: .oneWeek)

        #expect(PercentReturnFormatter.format(try #require(model.move).ratio) == "+10.0%")
        #expect(try #require(model.series).drawsBaselineRule == false)
    }

    /// The bug: a missing token returned before the loading flag was cleared, leaving the
    /// screen on a bare spinner with no retry.
    @Test func aMissingTokenEndsInAFailedState() async throws {
        let source = StubAssetDetailDataSource()
        source.detailError = Monaco.MonacoAPIError.missingAccessToken
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadDetail()

        #expect(model.detailState == .failed)
    }

    @Test func rejectedSessionAsksTheViewToSignOut() async throws {
        let source = StubAssetDetailDataSource()
        source.detailError = Monaco.MonacoAPIError.httpStatus(401)
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadDetail()

        #expect(model.sessionExpired)
        #expect(model.detailState == .loading)
    }

    @Test func buyIsBlockedOnlyWhenTheStockIsKnownToBeUnbuyable() async throws {
        let source = StubAssetDetailDataSource()
        source.routable = false
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        #expect(model.canBuy)

        await model.loadDetail()
        #expect(!model.canBuy)
    }

    /// A refresh that fails keeps the curve already drawn rather than blanking it to an error.
    @Test func aFailedRefreshKeepsTheCurveAlreadyDrawn() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

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
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

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
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

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
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadChart(range: .oneDay)

        #expect(model.move?.direction == .flat)
        #expect(model.curveDirection == .flat)
    }

    @Test func aFallingWindowReadsAsALoss() async throws {
        let source = StubAssetDetailDataSource()
        source.points = [.oneDay: StubAssetDetailDataSource.series(from: 110, to: 100)]
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadChart(range: .oneDay)

        #expect(model.move?.direction == .down)
    }

    // MARK: - A series built for another window

    /// The data layer echoes the range it built a series for. One built for another
    /// window is not an answer to the question this chip asked.
    @Test func aSeriesBuiltForAnotherRangeIsRefusedRatherThanDrawn() async throws {
        let source = StubAssetDetailDataSource()
        source.echoedRange = .oneYear
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadChart(range: .oneDay)

        #expect(model.charts[.oneDay] == .failed)
        #expect(model.series == nil)
    }

    @Test func aMismatchedRefreshKeepsTheCurveAlreadyDrawn() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

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
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

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

    /// A line that changed colour under the finger would read as the price having
    /// moved. The curve keeps the window's verdict; only the header follows the scrub.
    @Test func theCurveKeepsItsOwnVerdictWhileScrubbing() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadChart(range: .oneDay)
        model.scrubbedIndex = 0

        #expect(model.move?.direction == .flat)
        #expect(model.curveDirection == .up)
    }

    @Test func changingRangeDropsTheScrub() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

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
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadChart(range: .oneDay)
        model.scrubbedIndex = 2
        #expect(model.heroPriceUsdcMicros == 102_000_000)

        source.points[.oneDay] = StubAssetDetailDataSource.series(from: 100, to: 110)
        await model.refreshChart()

        #expect(model.scrubbedIndex == nil)
    }

    // MARK: - The live hero

    /// The flash says "that number changed while you were looking at it". The first
    /// load changed nothing — there was nothing there before.
    @Test func theFirstLoadDoesNotFlash() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadDetail()

        #expect(model.priceTick == nil)
        #expect(model.heroTick == nil)
    }

    @Test func aPollThatMovesThePriceRaisesATickWithItsDirection() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)
        await model.loadDetail()

        source.priceUsdcMicros = 186_000_000
        await model.refreshDetail()
        let up = try #require(model.priceTick)
        #expect(up.direction == .up)
        #expect(model.heroPriceUsdcMicros == 186_000_000)

        source.priceUsdcMicros = 184_000_000
        await model.refreshDetail()
        let down = try #require(model.priceTick)
        #expect(down.direction == .down)
        // Two ticks are two events, however they are signed.
        #expect(down.sequence > up.sequence)
    }

    @Test func aPollThatChangesNothingDoesNotFlash() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)
        await model.loadDetail()

        await model.refreshDetail()

        #expect(model.priceTick == nil)
    }

    /// A poll nobody asked for must never put an error in front of the member.
    @Test func aFailedPollLeavesTheScreenExactlyAsItWas() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)
        await model.loadDetail()
        let loaded = model.detailState

        source.detailError = Monaco.MonacoAPIError.httpStatus(500)
        await model.refreshDetail()

        #expect(model.detailState == loaded)
    }

    /// A quiet re-read that comes back empty is a source hiccup, not news.
    @Test func aQuietRefreshThatComesBackEmptyKeepsTheCurve() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadChart(range: .oneDay)
        let drawn = StubAssetDetailDataSource.expected(.oneDay, from: 100, to: 110)

        source.points[.oneDay] = []
        await model.refreshChart()

        #expect(model.chartState == .series(drawn))
    }

    /// The chip's spinner is driven by this, not by `ChartState.loading`: a range that
    /// already has a curve keeps showing it while it re-reads.
    @Test func aRangeInFlightIsNamedSoItsChipCanSaySo() async throws {
        let source = StubAssetDetailDataSource()
        source.delays[.oneDay] = .milliseconds(200)
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        let load = Task { await model.loadChart(range: .oneDay) }
        try await Task.sleep(for: .milliseconds(50))
        #expect(model.isLoadingCurrentRange)

        await load.value
        #expect(!model.isLoadingCurrentRange)
        #expect(model.loadingRanges.isEmpty)
    }

    // MARK: - Market session

    @Test func theSessionChipReadsTheStatusBlock() async throws {
        let source = StubAssetDetailDataSource()
        source.market = MarketSampleData.sessionAfterHours
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadDetail()

        #expect(model.sessionChip?.title == "After hours")
        #expect(!model.isMarketLive)
    }

    /// A backend that sends only the mirrored session still gets a chip.
    @Test func aMirroredSessionIsEnoughForAChip() async throws {
        let source = StubAssetDetailDataSource()
        source.marketSession = .open
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadDetail()

        #expect(model.sessionChip?.title == "Market open")
        #expect(model.isMarketLive)
    }

    /// An older backend says nothing about the market. Nothing is what the screen shows.
    @Test func noSessionMeansNoChipAndNoPulse() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

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
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        let refresh = Task { await model.refreshChart() }
        try await Task.sleep(for: .milliseconds(60))

        #expect(model.loadingRanges.isEmpty)
        #expect(!model.isLoadingCurrentRange)
        await refresh.value
        #expect(model.loadingRanges.isEmpty)
    }

    /// And it never puts an error in front of the member either, even on a range that
    /// has nothing drawn yet: the member did not ask for this read.
    @Test func aQuietReReadThatFailsLeavesTheRangeAlone() async throws {
        let source = StubAssetDetailDataSource()
        source.chartError = Monaco.MonacoAPIError.httpStatus(500)
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.refreshChart()

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
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        let slow = Task { await model.loadChart(range: .oneDay) }
        try await Task.sleep(for: .milliseconds(30))
        let quick = Task { await model.loadChart(range: .oneDay) }
        await quick.value

        // The second request is home; the first is still out, so the chip is still
        // working.
        #expect(model.isLoadingCurrentRange)

        await slow.value
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
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        let stale = Task { await model.loadChart(range: .oneDay) }
        try await Task.sleep(for: .milliseconds(30))
        await model.loadChart(range: .oneDay)
        let newest = StubAssetDetailDataSource.expected(.oneDay, from: 200, to: 210)
        #expect(model.chartState == .series(newest))

        await stale.value

        #expect(model.chartState == .series(newest), "the older answer landed on top of the newer one")
    }

    // MARK: - Whose number is under the price

    /// The hero price is the token's and every Pyth history source serves the
    /// underlying equity, so a figure folded from the curve is a different instrument
    /// from the price it sits under — the two cannot be reconciled by subtraction.
    /// The pill has to say which instrument it is about.
    @Test func aFigureFoldedFromTheCurveSaysWhichInstrumentItIsAbout() async throws {
        let source = StubAssetDetailDataSource()
        source.basis = .underlying
        source.basisSymbol = "AAPL"
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadDetail()
        await model.loadChart(range: .oneDay)

        let move = try #require(model.move)
        #expect(move.basisSymbol == "AAPL")
        #expect(move.basisCaption == "AAPL on its home exchange")
    }

    /// Scrubbing does not change whose numbers these are.
    @Test func aScrubbedFigureCarriesTheSameInstrument() async throws {
        let source = StubAssetDetailDataSource()
        source.basis = .underlying
        source.basisSymbol = "AAPL"
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadDetail()
        await model.loadChart(range: .oneDay)
        model.scrubbedIndex = 0

        #expect(model.move?.basisSymbol == "AAPL")
    }

    /// The fallback is the detail route's 24h change, which is the token's — the same
    /// instrument as the price above it, so it wears no tag. The bug this closes: the
    /// identical "Past day" label described the equity or the token depending on
    /// whether an unrelated chart request had succeeded.
    @Test func theFallbackFigureIsTheTokensAndSaysNothingElse() async throws {
        let source = StubAssetDetailDataSource()
        source.basis = .underlying
        source.basisSymbol = "AAPL"
        source.chartError = Monaco.MonacoAPIError.httpStatus(500)
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        await model.loadDetail()
        await model.loadChart(range: .oneDay)

        let move = try #require(model.move)
        #expect(move.label == "Past day")
        #expect(move.basisSymbol == nil)
        #expect(move.basisCaption == nil)
    }

    /// A curve the backend says is the token's is the same instrument as the hero, so
    /// it needs no tag either — and one it will not name gets no guess.
    @Test func aTokenCurveAndAnUnnamedCurveBothGoUntagged() async throws {
        let token = StubAssetDetailDataSource()
        token.basis = .token
        token.basisSymbol = "AAPLx"
        let tokenModel = AssetDetailModel(symbol: "AAPLx", dataSource: token)
        await tokenModel.loadChart(range: .oneDay)
        #expect(tokenModel.move?.basisSymbol == nil)

        let unnamed = AssetDetailModel(symbol: "AAPLx", dataSource: StubAssetDetailDataSource())
        await unnamed.loadChart(range: .oneDay)
        #expect(unnamed.move?.basisSymbol == nil)
    }

    // MARK: - Walking out of a scrub

    /// VoiceOver's adjustable action has no release, so walking forward off the last
    /// sample is the way back to the live price: the hero unpins, the change row goes
    /// back to the window's own label, and a poll can flash again.
    @Test func walkingOffTheEndOfTheCurveRestoresTheLiveHero() async throws {
        let source = StubAssetDetailDataSource()
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)
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
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)
        await model.loadDetail()
        await model.loadChart(range: .oneDay)
        model.scrubbedIndex = 0

        source.priceUsdcMicros = 186_000_000
        await model.refreshDetail()
        #expect(model.heroTick == nil, "a sample from the past must not flash")

        model.scrubbedIndex = MonacoScrubStep.next(from: 0, forward: true, count: 2)
        model.scrubbedIndex = MonacoScrubStep.next(from: model.scrubbedIndex, forward: true, count: 2)

        #expect(model.scrubbedIndex == nil)
        #expect(model.heroTick != nil)
    }
}
