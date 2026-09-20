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
    var points: [AssetChartRange: [AssetChartPointDTO]] = [:]

    func detail(symbol: String) async throws -> AssetDetailDTO {
        detailCalls += 1
        if let detailError { throw detailError }
        return AssetDetailDTO(
            symbol: symbol,
            name: "Apple xStock",
            solanaMint: "MintAAPL",
            routable: routable,
            priceUsdcMicros: 185_000_000,
            change24h: change24h,
            liquidity: AssetLiquidityDTO(
                label: "Via Jupiter",
                routable: routable,
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
        let model = AssetDetailModel(symbol: "AAPLx", dataSource: source)

        #expect(model.chartState == .loading)

        await model.loadChart(range: .oneDay)
        #expect(model.chartState == .series(StubAssetDetailDataSource.series(from: 100, to: 110)))
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
        #expect(model.chartState == .series(StubAssetDetailDataSource.series(from: 100, to: 110)))
        // The late month series is kept, in its own slot, so switching back is instant.
        #expect(model.charts[.oneMonth] == .series(StubAssetDetailDataSource.series(from: 50, to: 60)))
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
}
