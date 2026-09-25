import Foundation
import MonacoCore
import SwiftUI
import Testing
@testable import Monaco

/// The pot's curve on the cabal hero: what the slot shows for what came back.
@MainActor
struct GroupHeroChartTests {
    private func series(_ count: Int) -> GroupPnLSeriesDTO {
        GroupPnLSeriesDTO(
            groupID: "g1",
            name: "Weekend investors",
            range: "1M",
            points: (0..<count).map {
                GroupPnLPointDTO(at: Date(timeIntervalSince1970: Double($0) * 86_400), potValueUsd: "100", netInUsd: "90", dollarPnl: "+\($0).00")
            }
        )
    }

    @Test func nothingBackYetIsLoading() {
        #expect(GroupHeroChart.resolve(nil, failed: false) == .loading)
    }

    @Test func aFailedReadWithNoSeriesSaysSo() {
        #expect(GroupHeroChart.resolve(nil, failed: true) == .failed)
    }

    /// A flat two-point line reads as broken, so the curve needs three points to draw.
    @Test func fewerThanThreePointsIsSparseNotACurve() {
        #expect(GroupHeroChart.resolve(series(2), failed: false) == .sparse)
        #expect(GroupHeroChart.resolve(series(3), failed: false) == .curve(series(3).points))
    }
}

/// Canned history reads, one answer per range, with a switch to make them fail.
@MainActor
private final class StubHistorySource: GroupPnLHistorySource {
    var answers: [GroupPnLRange: GroupPnLSeriesDTO] = [:]
    var failing = false
    private(set) var reads: [GroupPnLRange] = []

    func history(groupId: String, range: GroupPnLRange) async throws -> GroupPnLSeriesDTO {
        reads.append(range)
        if failing { throw URLError(.notConnectedToInternet) }
        guard let answer = answers[range] else { throw URLError(.badServerResponse) }
        return answer
    }
}

@MainActor
struct GroupPnLHistoryModelTests {
    private func series(range: GroupPnLRange, count: Int) -> GroupPnLSeriesDTO {
        GroupPnLSeriesDTO(
            groupID: "g1",
            name: "Weekend investors",
            range: range.rawValue,
            points: (0..<count).map {
                GroupPnLPointDTO(at: Date(timeIntervalSince1970: Double($0) * 3600), potValueUsd: "100", netInUsd: "90", dollarPnl: "+\($0).50")
            }
        )
    }

    @Test func eachRangeKeepsItsOwnSlot() async {
        let source = StubHistorySource()
        source.answers[.oneMonth] = series(range: .oneMonth, count: 5)
        source.answers[.oneWeek] = series(range: .oneWeek, count: 2)
        let model = GroupPnLHistoryModel(groupId: "g1", source: source)

        await model.load(range: .oneMonth)
        #expect(model.chart == .curve(series(range: .oneMonth, count: 5).points))

        model.range = .oneWeek
        #expect(model.chart == .loading, "a range that has not answered yet is loading, not the other range's curve")
        await model.load(range: .oneWeek)
        #expect(model.chart == .sparse)

        model.range = .oneMonth
        #expect(model.chart == .curve(series(range: .oneMonth, count: 5).points), "coming back to a drawn range is instant")
    }

    @Test func aFailedFirstReadIsReportedAndARetryClearsIt() async {
        let source = StubHistorySource()
        source.failing = true
        let model = GroupPnLHistoryModel(groupId: "g1", source: source)

        await model.load(range: .oneMonth)
        #expect(model.chart == .failed)

        source.failing = false
        source.answers[.oneMonth] = series(range: .oneMonth, count: 4)
        await model.load(range: .oneMonth)
        #expect(model.chart == .curve(series(range: .oneMonth, count: 4).points))
    }

    /// The cabal screen polls; a quiet re-read that fails must not blank the curve on screen.
    @Test func aQuietFailureLeavesTheDrawnCurveAlone() async {
        let source = StubHistorySource()
        source.answers[.oneMonth] = series(range: .oneMonth, count: 4)
        let model = GroupPnLHistoryModel(groupId: "g1", source: source)
        await model.load(range: .oneMonth)

        source.failing = true
        await model.refresh()
        #expect(model.chart == .curve(series(range: .oneMonth, count: 4).points))
        #expect(model.loadingRanges.isEmpty)
    }

    /// The window's move is read off the curve's ends, as the backend's own signed string.
    @Test func theWindowMoveIsTheCurvesEnds() async {
        let source = StubHistorySource()
        source.answers[.oneMonth] = series(range: .oneMonth, count: 4)
        let model = GroupPnLHistoryModel(groupId: "g1", source: source)
        await model.load(range: .oneMonth)
        #expect(model.windowDollarPnl == "+3.00")
    }
}

/// The pot-mix bar's slices: largest stock first, cash last, nothing for a zero row.
struct PotMixBarTests {
    private func row(_ symbol: String, value: String) -> PotRowDTO {
        PotRowDTO(symbol: symbol, units: "1", markUsd: "1", valueUsd: value, dollarPnl: "+0.00", afterHours: nil)
    }

    @Test func stocksLeadByValueAndCashCloses() {
        let segments = PotMixBar.segments(for: [
            row("USDC", value: "50.00"),
            row("NVDAx", value: "150.00"),
            row("AAPLx", value: "300.00"),
        ])
        #expect(segments.map(\.symbol) == ["AAPL", "NVDA", "Cash"])
        #expect(segments.map(\.isCash) == [false, false, true])
        #expect(abs(segments[0].fraction - 0.6) < 0.0001)
        #expect(abs(segments[2].fraction - 0.1) < 0.0001)
    }

    @Test func zeroAndUnparseableRowsAreDropped() {
        let segments = PotMixBar.segments(for: [
            row("USDC", value: "0.00"),
            row("AAPLx", value: "not money"),
            row("TSLAx", value: "12.00"),
        ])
        #expect(segments.map(\.symbol) == ["TSLA"])
        #expect(segments[0].fraction == 1)
    }

    @Test func anEmptyPotHasNoBar() {
        #expect(PotMixBar.segments(for: []).isEmpty)
        #expect(PotMixBar.segments(for: [row("USDC", value: "0")]).isEmpty)
    }

    @Test func slicesUnderOnePercentSayLessThanOne() {
        #expect(PotMixBar.percent(0.004) == "<1%")
        #expect(PotMixBar.percent(0.5) == "50%")
    }
}

/// Custom faces do not synthesise weights, so a weight asked for in SwiftUI terms has to land
/// on a real face of the family.
struct MonacoTypefaceTests {
    @Test func everyWeightLandsOnARealFace() {
        #expect(MonacoTypeface.avenirNext(.regular) == "AvenirNext-Regular")
        #expect(MonacoTypeface.avenirNext(.medium) == "AvenirNext-Medium")
        #expect(MonacoTypeface.avenirNext(.semibold) == "AvenirNext-DemiBold")
        #expect(MonacoTypeface.avenirNext(.bold) == "AvenirNext-Bold")
        #expect(MonacoTypeface.avenirNext(.heavy) == "AvenirNext-Bold")
        #expect(MonacoTypeface.avenirNext(.light) == "AvenirNext-Regular")
    }

    /// The family ships with iOS; a missing face would fall back to a system font silently.
    @Test func theFacesExistOnThisRuntime() {
        for name in ["AvenirNext-Regular", "AvenirNext-Medium", "AvenirNext-DemiBold", "AvenirNext-Bold"] {
            #expect(UIFont(name: name, size: 17) != nil, "\(name) is not installed")
        }
    }

    /// Money lines up in a column because Avenir Next's lining figures are tabular by default.
    @Test func avenirNextFiguresAreTabular() {
        let font = UIFont(name: "AvenirNext-DemiBold", size: 40)!
        let narrow = ("1111.11" as NSString).size(withAttributes: [.font: font]).width
        let wide = ("8888.88" as NSString).size(withAttributes: [.font: font]).width
        #expect(abs(narrow - wide) < 0.01)
    }
}
