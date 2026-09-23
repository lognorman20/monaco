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
    let auth: DynamicAuthService
    private let apiClient = MonacoAPIClient()

    init(auth: DynamicAuthService) {
        self.auth = auth
    }

    func detail(symbol: String) async throws -> AssetDetailDTO {
        try await auth.sendingAccessToken { try await apiClient.getMarketAsset(accessToken: $0, symbol: symbol) }
    }

    func chart(symbol: String, range: AssetChartRange) async throws -> AssetChartDTO {
        try await auth.sendingAccessToken {
            try await apiClient.getMarketAssetChart(accessToken: $0, symbol: symbol, range: range)
        }
    }
}

/// How often the screen re-reads itself while the member is looking at it.
///
/// Both cadences are chosen against what the backend caches, so while the sources are
/// healthy a screen left open adds little upstream traffic:
///
/// - The detail route carries the hero price (the Chainlink mark, read on every request,
///   uncached), the Pyth equity quote (shared between callers for 10 s,
///   `pyth.EquityQuoteTTL`), the Kyber probes (60 s, `liquidityProbeTTL`) and the Benchmarks
///   1D and 1Y series behind the grid (1 and 10 minutes, `pyth.ChartCacheDayTTL` /
///   `ChartCacheLongTTL`). While everything answers, a 10 s poll costs one Chainlink read
///   per viewer and at most one fresh Hermes quote per symbol.
/// - A chart range is cached for a minute (1D) or ten (everything longer), so this is about
///   a screen left open across a range's worth of new bars, not about live ticks.
///
/// Two costs this does not bound. The Chainlink `latestRoundData` read is per viewer per
/// poll. And the backend does not cache a Kyber probe that *errored*, so during a Kyber
/// outage every viewer's poll fires a fresh buy and sell probe; the detail call itself still
/// succeeds, so the back-off below never engages. A short negative cache for errored probes
/// belongs in the backend, not here.
///
/// A poll whose request fails backs off (see `pollWhileVisible`), so an API outage is not
/// hammered at the healthy rate.
enum AssetDetailPolling {
    static let price: Duration = .seconds(10)
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

    /// A chart read that failed for a reason the transport did not report.
    enum ChartLoadError: Error, Equatable {
        /// The response echoed a range other than the one requested, so nothing in it
        /// can be drawn under the selected chip.
        case rangeMismatch(requested: AssetChartRange)
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
        /// The same move in dollars ("5.50"), when the curve can measure one *in the unit
        /// of the price above it*. Nil when the only figure available is the backend's own
        /// day move, and nil under the token's live mark when the curve is the share's:
        /// the token carries a multiplier, so the share's dollars are not the token's.
        var dollars: String?
        /// Which instrument this figure is about, when that is *not* the instrument of
        /// the price it sits under and the label does not already say so.
        ///
        /// The hero price is the B20 token's Chainlink total-return mark, per token.
        /// Every Pyth history source serves the underlying equity, per share, so a move
        /// folded from the curve is `AAPL` on its exchange. The two cannot be reconciled
        /// by subtraction (the token carries a multiplier), and a `PnLBadge` is the same
        /// component the app uses for a member's own P&L — unlabelled, "▲ $5.50" under
        /// "$232.05" reads as a subtraction that does not work.
        var basisSymbol: String?
        /// The long form of the same fact, for VoiceOver: "AAPL on its home exchange".
        var basisCaption: String?

        /// The pill's short tag: "AAPL share". The bare ticker is not enough, because the
        /// token's own display ticker (the screen's title) is also "AAPL".
        var basisTag: String? {
            basisSymbol.map { "\($0) share" }
        }

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

    /// The rejection that ended this screen's reads, naming the token the request sent. The
    /// view reports that token to the guarded sign-out.
    private(set) var rejectedSession: RejectedSession?

    var sessionExpired: Bool { rejectedSession != nil }

    /// What "today" is when a day chart names its session. Only tests set it.
    var now: () -> Date = Date.init

    private let dataSource: AssetDetailDataSource
    private var tickSequence = 0
    /// Issue order of detail reads. The first load, a Retry, the 10 s poll and the tick on
    /// returning from the background can all be in flight at once, and only the newest may
    /// write: an older Chainlink mark landing last would move the hero price backwards and
    /// flash it the wrong way.
    private var detailRequestSequence = 0
    /// Per-range issue order, so only the newest request for a range may write it.
    private var chartRequestSequence: [AssetChartRange: Int] = [:]
    /// How many requests the member is actually waiting on, per range.
    private var visibleChartRequests: [AssetChartRange: Int] = [:]

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

    /// Only the stock-level verdict blocks the buy; an unknown answer leaves it open.
    ///
    /// Not `liquidity.routable`: that is this one response's quote probe, and a probe that
    /// *errored* (a rate limit, a timeout) reports `routable = false` for that response even
    /// though it says nothing about the pool. Reading it would call a buyable stock unbuyable
    /// after one bad probe.
    ///
    /// On the backend today this is effectively always `true`: the stock-level `routable` is
    /// `asset.Routable || tokenAddress != "" || liquidity.Routable`, and every pinned B20 stock
    /// has a token address, so a genuine no-route never reaches this flag and the "Can't be
    /// bought right now." caption does not render. Making the stock-level verdict tell a real
    /// no-route from an errored probe is backend work, tracked against the data stage (#407).
    var canBuy: Bool {
        detail?.routable ?? true
    }

    // MARK: - The exchange

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

    /// The chip only says the token still trades on Base when this read's Kyber probes
    /// found a route both ways. "Trades" means a member could get in and out; a buy route
    /// with no sell route is half a market, and the chip stays quiet about it.
    var sessionChip: MarketSessionChipCopy? {
        MarketSessionCopy.chip(for: marketStatus, tokenRoutable: tokenRoutesBothWays)
    }

    /// A route out means a *payout*, not just a field on the response. The backend
    /// writes `sellProbeOutAmount` for any routable quote that carried an amount, so a
    /// route that values a whole token at nothing arrives as "0"; its own pricing step
    /// then throws that away and reports the token leg unavailable. Reading the string's
    /// presence rather than its value is how the chip came to claim a market the same
    /// payload had already priced at nothing. `routesBothWays` asks for a positive sell.
    private var tokenRoutesBothWays: Bool {
        detail?.liquidity.routesBothWays ?? false
    }

    /// True while the exchange behind the curve is still printing. The chart's
    /// end-of-line pulse and nothing else reads this.
    var isMarketLive: Bool { sessionChip?.isLive ?? false }

    /// What the hero's change pill flashes on, or nil when nothing has moved yet.
    ///
    /// Silent while a finger is on the curve: the pill is showing a sample from the
    /// past then, and flashing it would claim that *that* number had just moved. The
    /// tick is still recorded, so the flash lands when the finger lifts and the live
    /// price comes back.
    var heroTick: MonacoPriceTick? {
        guard !isScrubbing else { return nil }
        return priceTick.map { MonacoPriceTick(sequence: $0.sequence, isUp: $0.direction == .up) }
    }

    // MARK: - What the hero shows

    var isScrubbing: Bool { scrubbedPoint != nil }

    var scrubbedPoint: AssetChartPointDTO? {
        guard let scrubbedIndex, let series else { return nil }
        return series.point(at: scrubbedIndex)
    }

    /// The price in the hero: the sample under the finger while scrubbing, otherwise the
    /// token's live Chainlink mark from the detail route.
    var heroPriceUsdcMicros: Int64? {
        scrubbedPoint?.priceUsdcMicros ?? detail?.priceUsdcMicros
    }

    /// Whose price the hero is showing while a finger is on the curve.
    ///
    /// On an underlying curve it is "AAPL on its home exchange": the sample under the finger
    /// is the share's price, not the token's, so the line that names the stock says so for
    /// as long as it is shown. A sample of the token's own rounds is the hero's instrument
    /// and needs nothing. A series the backend did not name gets "Chart price", so an
    /// unknown instrument never passes for the token's mark. Nil when not scrubbing.
    var heroPriceCaption: String? {
        guard isScrubbing, let series else { return nil }
        if series.underlyingDisplaySymbol != nil { return series.basisCaption }
        if series.basis == .token { return nil }
        return Self.unnamedChartPriceCaption
    }

    static let unnamedChartPriceCaption = "Chart price"

    /// "As of Fri 4:00 PM" under the hero when the mark behind it is holding rather than
    /// moving, and nil when it is live.
    ///
    /// The hero is the token's Chainlink total-return mark. Outside the cash session that
    /// feed holds the last close, so `MoneyText` keeps rolling a number that has not moved
    /// since Friday — and the session chip beside it may still say the token trades on
    /// Base, which is true of the pools and not of this price. The mark carries its own
    /// verdict (`stockVsToken.mark.status`), which is not the same fact as `afterHours`:
    /// that is the exchange calendar's, and a feed can hold while the calendar says open.
    ///
    /// Silent during a scrub, where the price shown is the curve's own sample and already
    /// carries that sample's time in the change row.
    var heroPriceAsOf: String? {
        guard !isScrubbing, let mark = detail?.stockVsToken?.mark else { return nil }
        return StaleMarkCaption.caption(status: mark.status, publishedAt: mark.publishedAt)
    }


    /// The figure under the price.
    ///
    /// While scrubbing it is the move from the window's baseline to the point under the
    /// finger, labelled with that point's own time. Otherwise it is the drawn window's
    /// move (see `windowMove`).
    var move: Move? {
        if let series, let index = scrubbedIndex, let point = series.point(at: index),
           let ratio = series.changeRatio(toIndex: index) {
            return curveMove(
                series,
                ratio: ratio,
                label: ChartScrubLabel.caption(for: point.date, range: series.range),
                dollars: series.changeDollars(toIndex: index)
            )
        }
        return windowMove
    }

    /// The drawn window's move under the window's name, measured from the series'
    /// baseline: the previous session's close on a Benchmarks day chart, the window's
    /// first sample otherwise.
    ///
    /// A day chart of one session is named for that session ("Today", or "Fri, Sep 25"
    /// over a weekend), because the backend's 1D window is the last trading session and
    /// not a rolling 24 hours.
    ///
    /// With no curve it falls back to the stock's day move under its own label ("AAPL day
    /// move"), so the number never claims a period it did not measure, nor to be the
    /// token's. Without a basis from the backend there is no figure at all.
    var windowMove: Move? {
        if let series, let ratio = windowRatio {
            // Unscrubbed, the hero is the token's Chainlink mark, per token. Dollars are
            // shown only when the curve is in that same unit — `basis == .token` — because
            // a delta from any other curve would be subtracted under a price it was not
            // measured in. That rules out the underlying's per-share curve (the token's
            // multiplier would land in the day's move) and equally a curve the backend did
            // not name: `.unknown` and a missing basis are unit-unknown, not "the token".
            // The ratio holds across the multiplier, so it is shown either way. A scrub
            // puts the curve's own sample in the hero, and the dollars come back with it.
            let dollars = series.basis == .token ? series.changeDollars() : nil
            return curveMove(series, ratio: ratio, label: windowLabel(series), dollars: dollars)
        }
        guard let dayMove = detail?.stockDayMove else { return nil }
        // The label already names the instrument, so the pill carries no second tag;
        // VoiceOver still gets the long form.
        return Move(
            ratio: dayMove.ratio,
            label: dayMove.caption,
            basisCaption: MarketPriceBasisCaption.caption(basis: .underlying, symbol: dayMove.symbol)
        )
    }

    /// A figure measured from the curve, tagged with the curve's instrument whenever
    /// that is not the one the hero price is quoted in.
    private func curveMove(_ series: AssetChartSeries, ratio: String, label: String, dollars: String?) -> Move {
        var move = Move(ratio: ratio, label: label, dollars: dollars)
        // Only the underlying differs from the token in the hero. A series the
        // backend says is the token's needs no tag, and one it will not name at all
        // gets no guess.
        if let symbol = series.underlyingDisplaySymbol {
            move.basisSymbol = symbol
            move.basisCaption = series.basisCaption
        }
        return move
    }

    private func windowLabel(_ series: AssetChartSeries) -> String {
        series.session(now: now())?.caption ?? series.range.moveLabel
    }

    private func spokenWindow(_ series: AssetChartSeries) -> String {
        series.session(now: now())?.spokenPhrase ?? series.range.moveLabel.lowercased()
    }

    /// The curve's own verdict, which does *not* follow the scrub: a line that changed
    /// colour under the finger would read as the price having moved, and the member is
    /// only looking at where it has been.
    var curveDirection: Move.Direction {
        windowMove?.direction ?? .flat
    }

    private var windowRatio: String? {
        guard let series, series.isDrawable else { return nil }
        return series.changeRatio()
    }

    /// What VoiceOver reads for the chart: the range, the window's move and whose price
    /// the curve is.
    ///
    /// The low and the high are dollars on the curve's own instrument, which is usually
    /// the share (Pyth) while the price above is the token (Chainlink). They are only read
    /// when the series names its instrument, so a dollar figure is never spoken next to a
    /// price in another unit without saying so. The move is a ratio and holds in either.
    var chartAccessibilitySummary: String {
        guard let series, series.isDrawable else {
            return "\(range.accessibilityLabel) price history."
        }
        let moveText = windowMove.map { PercentReturnFormatter.format($0.ratio) } ?? "—"
        var sentence = "\(series.range.accessibilityLabel) price history. \(moveText) \(spokenWindow(series))."
        if let caption = series.basisCaption {
            sentence += " Low \(UsdAmountFormatter.format(micros: Self.micros(series.lowValue))), "
                + "high \(UsdAmountFormatter.format(micros: Self.micros(series.highValue))), \(caption)."
        }
        return sentence
    }

    /// What VoiceOver reads for one sample: "$231.40, up 2.4%, Tue 2:05 PM".
    func describePoint(at index: Int) -> String {
        guard let series, let point = series.point(at: index) else { return "—" }
        let price = UsdAmountFormatter.format(micros: point.priceUsdcMicros)
        let time = ChartScrubLabel.caption(for: point.date, range: series.range)
        guard let ratio = series.changeRatio(toIndex: index) else { return "\(price), \(time)" }
        return "\(price), \(PnLSpeech.percent(PercentReturnFormatter.format(ratio))), \(time)"
    }

    private static func micros(_ value: Double) -> Int64 {
        Int64((value * 1_000_000).rounded())
    }

    // MARK: - Loading

    func loadDetail() async {
        if detail == nil { detailState = .loading }
        let sequence = beginDetailRequest()
        do {
            let fresh = try await dataSource.detail(symbol: symbol)
            guard isCurrentDetailRequest(sequence) else { return }
            apply(fresh)
        } catch {
            guard isCurrentDetailRequest(sequence) else { return }
            handle(error) { if detail == nil { detailState = .failed } }
        }
    }

    /// A new session starts with no rejection on record. Called when the signed-in member
    /// changes, before the screen reads again.
    func beginSession() {
        rejectedSession = nil
    }

    /// A poll tick. It never shows a spinner, never shows an error, and never replaces
    /// what is on screen with an equal value — a refresh nobody asked for must leave the
    /// screen exactly as the member last saw it.
    ///
    /// It rethrows a failure so the poll loop can back off; nothing on screen reads it. A
    /// failure that a newer read has already superseded is dropped, not rethrown: the
    /// newer read is the one that speaks for the backend now.
    func refreshDetail() async throws {
        let sequence = beginDetailRequest()
        do {
            let fresh = try await dataSource.detail(symbol: symbol)
            guard isCurrentDetailRequest(sequence) else { return }
            apply(fresh)
        } catch {
            guard isCurrentDetailRequest(sequence) else { return }
            handle(error) {}
            throw error
        }
    }

    private func beginDetailRequest() -> Int {
        detailRequestSequence += 1
        return detailRequestSequence
    }

    private func isCurrentDetailRequest(_ sequence: Int) -> Bool {
        detailRequestSequence == sequence
    }

    /// Re-reads the range on screen without disturbing it. Throws only so the poll can
    /// back off.
    func refreshChart() async throws {
        if let error = await loadChart(range: range, quietly: true) { throw error }
    }

    /// Writes only into `range`'s slot, so a late response lands where it belongs or nowhere.
    ///
    /// A quiet load is invisible by definition: no spinner, no error, and nothing on
    /// screen replaced by an equal value. `loadingRanges` is what the chip's spinner
    /// reads, and that spinner means "you tapped this and it has not arrived" — a
    /// two-minute background re-read must never raise it, or the selected chip sprouts
    /// a `ProgressView` (and says "Loading" to VoiceOver) for a refresh nobody asked for.
    ///
    /// Returns the failure, if any, for the poll's back-off; callers that are not polling
    /// ignore it.
    @discardableResult
    func loadChart(range: AssetChartRange, quietly: Bool = false) async -> Error? {
        if !quietly, charts[range] == nil || charts[range] == .failed {
            charts[range] = .loading
        }
        // Newest request for a range wins its slot, whichever order the answers come
        // back in: a tap and a background poll overlap constantly, and without this
        // the older response can land last and repaint the newer one.
        chartRequestSequence[range, default: 0] += 1
        let sequence = chartRequestSequence[range] ?? 0
        if !quietly { beginVisibleRequest(range) }
        defer { if !quietly { endVisibleRequest(range) } }
        do {
            let response = try await dataSource.chart(symbol: symbol, range: range)
            guard chartRequestSequence[range] == sequence else { return nil }
            guard let series = AssetChartSeries(response, requested: range) else {
                // The server answered about a window nobody asked for. Drawing it would put
                // a year of history under a 1D chip; leave the slot alone and offer a retry.
                if !quietly, !hasDrawnCurve(range) { charts[range] = .failed }
                // Reported as a failure, not as a quiet nil. A backend stuck answering the
                // wrong window fails every read, and returning nil here left `refreshChart`
                // non-throwing, so the poll never backed off and kept asking every two
                // minutes for a slot that was already `.failed`. This is a failed read like
                // any other; only its cause differs.
                return ChartLoadError.rangeMismatch(requested: range)
            }
            apply(series, to: range, quietly: quietly)
            return nil
        } catch {
            guard chartRequestSequence[range] == sequence else { return nil }
            handle(error) {
                if quietly { return }
                if hasDrawnCurve(range) { return }
                charts[range] = .failed
            }
            return error
        }
    }

    /// The chip spins while at least one *asked-for* request for its range is out.
    ///
    /// Counted rather than a bare flag: a tap and a retry on the same range overlap,
    /// and the first to return would otherwise stop the spinner while the other
    /// request is still in flight.
    private func beginVisibleRequest(_ range: AssetChartRange) {
        visibleChartRequests[range, default: 0] += 1
        loadingRanges.insert(range)
    }

    private func endVisibleRequest(_ range: AssetChartRange) {
        let remaining = (visibleChartRequests[range] ?? 1) - 1
        if remaining > 0 {
            visibleChartRequests[range] = remaining
        } else {
            visibleChartRequests[range] = nil
            loadingRanges.remove(range)
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
        if let rejected = error as? RejectedSession {
            rejectedSession = rejected
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
