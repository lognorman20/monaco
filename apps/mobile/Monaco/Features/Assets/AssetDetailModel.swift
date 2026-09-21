import MonacoCore
import Observation
import SwiftUI

/// Reads for one stock's detail screen. The live source calls the API; tests swap in a stub.
@MainActor
protocol AssetDetailDataSource {
    func detail(symbol: String) async throws -> AssetDetailDTO
    func chart(symbol: String, range: AssetChartRange) async throws -> AssetChartDTO
}

@MainActor
struct LiveAssetDetailDataSource: AssetDetailDataSource {
    let auth: PrivyAuthService
    private let apiClient = MonacoAPIClient()

    init(auth: PrivyAuthService) {
        self.auth = auth
    }

    func detail(symbol: String) async throws -> AssetDetailDTO {
        try await auth.withAccessToken { try await apiClient.getMarketAsset(accessToken: $0, symbol: symbol) }
    }

    func chart(symbol: String, range: AssetChartRange) async throws -> AssetChartDTO {
        try await auth.withAccessToken { try await apiClient.getMarketAssetChart(accessToken: $0, symbol: symbol, range: range) }
    }
}

/// How often the screen re-reads itself while the member is looking at it.
enum AssetDetailPolling {
    /// The hero price. The detail route is the one that carries the price, the market
    /// session and the stats, and its reference quotes are cached for 10s upstream —
    /// asking faster than that would re-read the same bytes.
    static let price: Duration = .seconds(10)

    /// The drawn curve. Series are cached for ten minutes upstream, so this is about
    /// a screen left open across a range's worth of new bars, not about live ticks.
    static let chart: Duration = .seconds(120)
}

/// State for the stock detail screen.
///
/// The header figure and the curve are read from the same `AssetChartSeries`, and each
/// range owns its own slot, so a slow range can never repaint a range the user has
/// already moved on from.
@Observable
@MainActor
final class AssetDetailModel {
    enum DetailState: Equatable {
        case loading
        case loaded(AssetDetailDTO)
        case failed
    }

    enum ChartState: Equatable {
        case loading
        case series(AssetChartSeries)
        case empty
        case failed
    }

    /// The move the header shows: over the drawn window, or — while a finger is on the
    /// curve — from the baseline to the point under it.
    struct Move: Equatable {
        /// Backend-style ratio ("0.0124"), so it formats through `PercentReturnFormatter`.
        ///
        /// Always fixed-point. `String(someDouble)` switches to scientific notation below 1e-4
        /// ("5e-05"), and `MonacoTheme.signed` reads the digits of that as "505" and tints a flat
        /// move as a gain.
        let ratio: String
        let label: String
        /// The same move in dollars ("5.50"), when the curve can measure one. Nil when
        /// the only figure available is the backend's own 24h ratio.
        var dollars: String?

        private var value: Decimal {
            Decimal(string: ratio, locale: Locale(identifier: "en_US_POSIX")) ?? 0
        }

        /// The curve's tint is read from here, never recomputed from the series: a move the
        /// header calls flat must not be drawn in profit green.
        var direction: Direction {
            if value > 0 { return .up }
            if value < 0 { return .down }
            return .flat
        }

        enum Direction: Equatable {
            case up
            case down
            case flat
        }
    }

    /// A price that changed under a poll. The hero flashes on it, so it carries a
    /// sequence number: two ticks in the same direction are still two ticks.
    struct PriceTick: Equatable {
        let sequence: Int
        let direction: Move.Direction
    }

    let symbol: String

    /// The chip the member is on. Changing it drops any scrub, because the point under
    /// the finger belonged to the window that is going away.
    var range: AssetChartRange = .oneDay {
        didSet {
            guard range != oldValue else { return }
            scrubbedIndex = nil
        }
    }

    /// The sample under the finger, as an index into the current series. Set by the
    /// chart, read by the header — that is what makes the price follow the drag.
    var scrubbedIndex: Int?

    private(set) var detailState: DetailState = .loading
    private(set) var charts: [AssetChartRange: ChartState] = [:]
    /// Ranges with a request in flight. Distinct from `ChartState.loading`: a range that
    /// already has a curve keeps showing it, and the chip carries the spinner instead.
    private(set) var loadingRanges: Set<AssetChartRange> = []
    private(set) var priceTick: PriceTick?

    /// Set when the server rejects the session. The data source has already ended it.
    private(set) var sessionExpired = false

    private let dataSource: AssetDetailDataSource
    private var tickSequence = 0

    init(symbol: String, dataSource: AssetDetailDataSource) {
        self.symbol = symbol
        self.dataSource = dataSource
    }

    var detail: AssetDetailDTO? {
        if case .loaded(let detail) = detailState { return detail }
        return nil
    }

    var chartState: ChartState {
        charts[range] ?? .loading
    }

    /// The series on screen, if there is one.
    var series: AssetChartSeries? {
        if case .series(let series) = chartState { return series }
        return nil
    }

    var isLoadingCurrentRange: Bool {
        loadingRanges.contains(range)
    }

    /// Only a definitive Jupiter "no route" blocks the buy; an unknown answer leaves it open.
    var canBuy: Bool {
        detail?.liquidity.routable ?? true
    }

    /// The exchange's state for the chip under the price.
    ///
    /// The detail payload carries the full status block and also mirrors the session
    /// at the top level. A backend that sends only the mirror still gets a chip, built
    /// from what it did send — it just has no next transition to count down to.
    var marketStatus: MarketStatusDTO? {
        guard let detail else { return nil }
        if let market = detail.market { return market }
        guard let session = detail.marketSession else { return nil }
        return MarketStatusDTO(session: session, isOpen: session.isRegularSession, afterHours: detail.afterHours)
    }

    var sessionChip: MarketSessionChipCopy? {
        MarketSessionCopy.chip(for: marketStatus)
    }

    /// True while the exchange behind the curve is still printing. The chart's
    /// end-of-line pulse and nothing else reads this.
    var isMarketLive: Bool { sessionChip?.isLive ?? false }

    /// What the hero's change pill flashes on, or nil when nothing has moved yet.
    var heroTick: MonacoPriceTick? {
        priceTick.map { MonacoPriceTick(sequence: $0.sequence, isUp: $0.direction == .up) }
    }

    // MARK: - What the hero shows

    var isScrubbing: Bool { scrubbedPoint != nil }

    var scrubbedPoint: AssetChartPointDTO? {
        guard let scrubbedIndex, let series else { return nil }
        return series.point(at: scrubbedIndex)
    }

    /// The price in the hero: the sample under the finger while scrubbing, otherwise the
    /// live price from the detail route.
    var heroPriceUsdcMicros: Int64? {
        scrubbedPoint?.priceUsdcMicros ?? detail?.priceUsdcMicros
    }

    /// The figure under the price.
    ///
    /// While scrubbing it is the move from the window's baseline to the point under the
    /// finger, labelled with that point's own time. Otherwise it is the drawn window's
    /// move under the window's name, and with no curve at all it falls back to the 24h
    /// change — under its own label, so the number never claims a period it did not measure.
    var move: Move? {
        if let series, let index = scrubbedIndex, let point = series.point(at: index),
           let ratio = series.changeRatio(toIndex: index) {
            return Move(
                ratio: ratio,
                label: ChartScrubLabel.caption(for: point.date, range: series.range),
                dollars: series.changeDollars(toIndex: index)
            )
        }
        if let ratio = windowRatio {
            return Move(ratio: ratio, label: range.moveLabel, dollars: series?.changeDollars())
        }
        guard let change = detail?.change24h, !change.isEmpty else { return nil }
        return Move(ratio: change, label: AssetChartRange.oneDay.moveLabel)
    }

    /// The curve's own verdict, which does *not* follow the scrub: a line that changed
    /// colour under the finger would read as the price having moved, and the member is
    /// only looking at where it has been.
    var curveDirection: Move.Direction {
        guard let ratio = windowRatio else {
            guard let change = detail?.change24h, !change.isEmpty else { return .flat }
            return Move(ratio: change, label: "").direction
        }
        return Move(ratio: ratio, label: "").direction
    }

    private var windowRatio: String? {
        guard let series, series.isDrawable else { return nil }
        return series.changeRatio()
    }

    // MARK: - Loading

    func loadDetail() async {
        if detail == nil { detailState = .loading }
        do {
            apply(try await dataSource.detail(symbol: symbol))
        } catch {
            handle(error) { if detail == nil { detailState = .failed } }
        }
    }

    /// A poll tick. It never shows a spinner, never shows an error, and never replaces
    /// what is on screen with an equal value — a refresh nobody asked for must leave the
    /// screen exactly as the member last saw it.
    func refreshDetail() async {
        do {
            apply(try await dataSource.detail(symbol: symbol))
        } catch {
            handle(error) {}
        }
    }

    /// Re-reads the range on screen without disturbing it.
    func refreshChart() async {
        await loadChart(range: range, quietly: true)
    }

    /// Writes only into `range`'s slot, so a late response lands where it belongs or nowhere.
    func loadChart(range: AssetChartRange, quietly: Bool = false) async {
        if !quietly, charts[range] == nil || charts[range] == .failed {
            charts[range] = .loading
        }
        loadingRanges.insert(range)
        defer { loadingRanges.remove(range) }
        do {
            let response = try await dataSource.chart(symbol: symbol, range: range)
            guard let series = AssetChartSeries(response, requested: range) else {
                // The server answered about a window nobody asked for. Drawing it would put
                // a year of history under a 1D chip; leave the slot alone and offer a retry.
                if !quietly, !hasDrawnCurve(range) { charts[range] = .failed }
                return
            }
            apply(series, to: range, quietly: quietly)
        } catch {
            handle(error) {
                if hasDrawnCurve(range) { return }
                charts[range] = .failed
            }
        }
    }

    private func apply(_ series: AssetChartSeries, to range: AssetChartRange, quietly: Bool) {
        let fresh: ChartState = series.isDrawable ? .series(series) : .empty
        // A quiet re-read that comes back empty is a source hiccup, not news: keep the
        // curve the member is looking at rather than blanking it.
        if quietly, fresh == .empty, hasDrawnCurve(range) { return }
        QuietUpdate.apply(fresh, over: charts[range] ?? .loading) { charts[range] = $0 }
        clampScrubIfNeeded(for: range)
    }

    /// A shorter series must not leave the finger pointing past the end of it.
    private func clampScrubIfNeeded(for range: AssetChartRange) {
        guard range == self.range, let index = scrubbedIndex else { return }
        if series?.point(at: index) == nil { scrubbedIndex = nil }
    }

    private func hasDrawnCurve(_ range: AssetChartRange) -> Bool {
        if case .series = charts[range] { return true }
        return false
    }

    /// Records a fresh detail payload, and notices when the price moved.
    ///
    /// The tick is what the hero flashes on, so it is only ever raised by a price that
    /// *changed*: the first load has nothing to compare against and must not flash.
    private func apply(_ fresh: AssetDetailDTO) {
        let previousPrice = detail?.priceUsdcMicros
        QuietUpdate.apply(DetailState.loaded(fresh), over: detailState) { detailState = $0 }
        guard let previousPrice, let price = fresh.priceUsdcMicros, price != previousPrice else { return }
        tickSequence += 1
        priceTick = PriceTick(sequence: tickSequence, direction: price > previousPrice ? .up : .down)
    }

    private func handle(_ error: Error, otherwise: () -> Void) {
        if error.isRequestCancellation { return }
        if case MonacoAPIError.httpStatus(401) = error {
            sessionExpired = true
            return
        }
        otherwise()
    }
}

extension AssetChartRange {
    /// Period the header figure covers. Plain money language, no "24h".
    var moveLabel: String {
        switch self {
        case .oneDay: return "Past day"
        case .oneWeek: return "Past week"
        case .oneMonth: return "Past month"
        case .threeMonths: return "Past three months"
        case .oneYear: return "Past year"
        case .all: return "All time"
        }
    }

    var accessibilityLabel: String {
        switch self {
        case .oneDay: return "One day"
        case .oneWeek: return "One week"
        case .oneMonth: return "One month"
        case .threeMonths: return "Three months"
        case .oneYear: return "One year"
        case .all: return "All time"
        }
    }
}
