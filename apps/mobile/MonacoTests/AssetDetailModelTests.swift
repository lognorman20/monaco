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
    /// The quote probe's own verdict. Main's backend caches a `false` here for a minute after a
    /// probe error, so it can disagree with `routable`.
    var liquidityRoutable = true
    var change24h: String? = "0.05"
    /// The backend labels `change24h` as the underlying's; nil plays an older backend.
    var change24hBasis: MarketPriceBasis? = .underlying
    var points: [AssetChartRange: [AssetChartPointDTO]] = [:]

    func detail(symbol: String) async throws -> AssetDetailDTO {
        detailCalls += 1
        if let detailError { throw detailError }
        return AssetDetailDTO(
            symbol: symbol,
            name: "Apple",
            tokenAddress: "0xb200000000000000000000000000000000000001",
            routable: routable,
            priceUsdcMicros: 185_000_000,
            change24h: change24h,
            change24hBasis: change24hBasis,
            change24hBasisSymbol: change24hBasis == nil ? nil : "AAPL",
            liquidity: AssetLiquidityDTO(
                label: "Via DEX",
                routable: liquidityRoutable,
                buyProbeUsdcMicros: 1_000_000,
                buyProbeOutAmount: "100000000",
                sellProbeInAmount: nil,
                sellProbeOutAmount: nil,
                spreadBps: 12
            )
        )
    }

    func chart(symbol: String, range: AssetChartRange) async throws -> AssetChartDTO {
        chartCalls.append(range)
        if let delay = delays[range] {
            try? await Task.sleep(for: delay)
        }
        if let chartError { throw chartError }
        return AssetChartDTO(points: points[range] ?? Self.series(from: 100, to: 110), emptyReason: nil)
    }

    static func series(from first: Int64, to last: Int64) -> [AssetChartPointDTO] {
        [
            AssetChartPointDTO(timestamp: 1_000, priceUsdcMicros: first * 1_000_000),
            AssetChartPointDTO(timestamp: 2_000, priceUsdcMicros: last * 1_000_000),
        ]
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
        #expect(model.chartState == .series(StubAssetDetailDataSource.series(from: 100, to: 110)))
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
        await slow.value

        #expect(model.range == .oneDay)
        #expect(model.chartState == .series(StubAssetDetailDataSource.series(from: 100, to: 110)))
        // The late month series is kept, in its own slot, so switching back is instant.
        #expect(model.charts[.oneMonth] == .series(StubAssetDetailDataSource.series(from: 50, to: 60)))
    }

    /// The bug: the header always showed the 24h move, unlabelled, under a 1W or 1M curve.
    @Test func theHeaderFigureMeasuresTheWindowTheCurveDraws() async throws {
        let source = StubAssetDetailDataSource()
        source.points[.oneWeek] = StubAssetDetailDataSource.series(from: 100, to: 110)
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)
        model.range = .oneWeek

        await model.loadDetail()
        await model.loadChart(range: .oneWeek)

        let move = try #require(model.move)
        #expect(move.label == "Past week")
        #expect(PercentReturnFormatter.format(move.ratio) == "+10.0%")
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

    @Test func buyIsBlockedOnlyWhenTheStockIsKnownToBeUnbuyable() async throws {
        let source = StubAssetDetailDataSource()
        source.routable = false
        source.liquidityRoutable = false
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        #expect(model.canBuy)

        await model.loadDetail()
        #expect(!model.canBuy)
    }

    /// The backend keeps a failed quote probe's `liquidity.routable = false` for 60 seconds, so
    /// the probe snippet cannot decide the button: the stock-level verdict does.
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
        let drawn = StubAssetDetailDataSource.series(from: 100, to: 110)
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
        #expect(model.charts[.oneWeek] == .series(StubAssetDetailDataSource.series(from: 100, to: 110)))
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
    }

    @Test func aFallingWindowReadsAsALoss() async throws {
        let source = StubAssetDetailDataSource()
        source.points = [.oneDay: StubAssetDetailDataSource.series(from: 110, to: 100)]
        let model = AssetDetailModel(symbol: "AAPLc", dataSource: source)

        await model.loadChart(range: .oneDay)

        #expect(model.move?.direction == .down)
    }

    /// The headline is the token's own mark ($185 here), but the series can be the underlying
    /// share's price ($250 to $275 here) depending on backend configuration. VoiceOver must not
    /// read a dollar range next to a price in a different unit; the move is a ratio and holds.
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
}
