import Foundation

/// A price series the detail screen can actually draw: the points, the baseline the
/// day change is measured against, and everything the scrub needs to answer "what
/// was the price here, and when".
///
/// This is the value the chart view and the screen's header both read, so the
/// figure under the price and the curve can never disagree — they are two renderings
/// of one struct. It is deliberately free of SwiftUI and of the network: the
/// nearest-point search, the baseline choice and the change arithmetic are all
/// testable on the host in microseconds.
///
/// Timestamps are UTC, as they are everywhere else. Only `ChartScrubLabel` converts,
/// and only for display.
public struct AssetChartSeries: Equatable, Sendable {
    /// The range this series is for — the chip it belongs under.
    public let range: AssetChartRange
    /// Ascending by timestamp, one point per timestamp. `init` sorts, because the
    /// nearest-point search is a binary search and a series that arrived out of
    /// order would silently return the wrong point rather than fail; it also drops
    /// repeats, because the drawing side keys its marks by timestamp and two
    /// samples at one instant are two marks with one identity.
    public let points: [AssetChartPointDTO]
    /// The close of the regular session before this window, when the source knew
    /// one. Only the day chart has a use for it.
    public let previousCloseUsdcMicros: Int64?
    public let source: AssetChartSource?
    public let basis: MarketPriceBasis?
    public let basisSymbol: String?

    public init(
        range: AssetChartRange,
        points: [AssetChartPointDTO],
        previousCloseUsdcMicros: Int64? = nil,
        source: AssetChartSource? = nil,
        basis: MarketPriceBasis? = nil,
        basisSymbol: String? = nil
    ) {
        self.range = range
        self.points = Self.canonical(points)
        self.previousCloseUsdcMicros = previousCloseUsdcMicros
        self.source = source
        self.basis = basis
        self.basisSymbol = basisSymbol
    }

    /// Builds the series for `requested`, or nil when the response is not about it.
    ///
    /// The backend echoes the range it built a series for. A response naming
    /// another one is a series for a chip the member has already tapped away from
    /// (or a cache serving the wrong key), and drawing it would put a year of
    /// history under a 1D chip. Nil means "this answer is not an answer to the
    /// question I asked" — the caller decides what to do about it.
    public init?(_ dto: AssetChartDTO, requested: AssetChartRange) {
        if let echoed = dto.range, echoed != requested { return nil }
        self.init(
            range: requested,
            points: dto.points,
            previousCloseUsdcMicros: dto.previousCloseUsdcMicros,
            source: dto.source,
            basis: dto.basis,
            basisSymbol: dto.basisSymbol
        )
    }

    /// Sorted ascending, with at most one point per timestamp.
    ///
    /// Nothing upstream promises distinct instants: a source that stitches two
    /// windows together, or a cache that concatenates a refresh onto its tail, can
    /// hand us the same second twice. Downstream that is not a cosmetic problem —
    /// the chart draws one mark per point keyed by its timestamp, so a repeat is a
    /// duplicate identity, which SwiftUI warns about and then renders wrong. The
    /// last sample for an instant wins, on the same "a later read is a better read"
    /// rule the quiet poll uses.
    private static func canonical(_ points: [AssetChartPointDTO]) -> [AssetChartPointDTO] {
        let sorted = points.sorted { $0.timestamp < $1.timestamp }
        var deduplicated: [AssetChartPointDTO] = []
        deduplicated.reserveCapacity(sorted.count)
        for point in sorted {
            if deduplicated.last?.timestamp == point.timestamp {
                deduplicated[deduplicated.count - 1] = point
            } else {
                deduplicated.append(point)
            }
        }
        return deduplicated
    }

    // MARK: - Shape

    /// Two points make a line. One point is a dot, and a dot reads as a broken chart.
    public var isDrawable: Bool { points.count >= 2 }

    public var lowValue: Double { points.map(\.chartValue).min() ?? 0 }

    public var highValue: Double { points.map(\.chartValue).max() ?? 0 }

    public var lastPoint: AssetChartPointDTO? { points.last }

    public func point(at index: Int) -> AssetChartPointDTO? {
        points.indices.contains(index) ? points[index] : nil
    }

    // MARK: - Baseline

    /// What the change is measured from, and where the dashed rule goes.
    ///
    /// On a day chart that is the previous session's close, which is what "up
    /// today" means on every broker's screen — the first point of the window is
    /// four hours of pre-market later. Over longer windows "previous close" means
    /// the close before the window, which is not a number anybody reads a year
    /// chart against, so those measure from their own first point.
    public var baselineUsdcMicros: Int64? {
        if let previousClose = previousCloseBaselineUsdcMicros { return previousClose }
        return points.first?.priceUsdcMicros
    }

    public var baselineValue: Double? {
        baselineUsdcMicros.map { Double($0) / 1_000_000 }
    }

    /// True only when the baseline is a real previous close. A dashed rule drawn at
    /// the curve's own first point sits *on* the curve at t0, which says nothing —
    /// draw no rule rather than a decorative one.
    public var drawsBaselineRule: Bool { previousCloseBaselineUsdcMicros != nil }

    private var previousCloseBaselineUsdcMicros: Int64? {
        guard range.showsPreviousCloseBaseline,
              let previousClose = previousCloseUsdcMicros,
              previousClose > 0
        else { return nil }
        return previousClose
    }

    // MARK: - Change

    /// The move from the baseline to `index` — the last point when `index` is nil —
    /// as a backend-style ratio ("0.012400") that `PercentReturnFormatter` can read.
    ///
    /// Always fixed-point and POSIX. `String(someDouble)` switches to scientific
    /// notation below 1e-4, and `MonacoTheme.signed` reads the digits of "5e-05" as
    /// "505" and tints a flat move as a gain.
    /// Nil when there is no baseline to measure from, and nil when `index` names a
    /// sample the series does not have: falling back to the last point there would
    /// hand a caller a real-looking number for a sample that does not exist.
    public func changeRatio(toIndex index: Int? = nil) -> String? {
        guard let baseline = baselineValue, baseline > 0 else { return nil }
        guard let value = target(at: index)?.chartValue else { return nil }
        return Self.ratioString(value / baseline - 1)
    }

    /// The same move in dollars — "5.50", "-1.20" — for the badge beside the percent.
    ///
    /// Both legs come from the curve, never from the hero price above it: the curve is
    /// usually the underlying equity's, per share, and the hero is the B20 token's
    /// Chainlink mark, per token. Subtracting one from the other would fold the
    /// token's multiplier (splits and reinvested dividends) into the day's move.
    public func changeDollars(toIndex index: Int? = nil) -> String? {
        guard let baseline = baselineValue else { return nil }
        guard let value = target(at: index)?.chartValue else { return nil }
        let delta = value - baseline
        guard delta.isFinite else { return nil }
        return String(format: "%.2f", locale: Locale(identifier: "en_US_POSIX"), delta)
    }

    /// The sample a change is measured *to*. No index means the end of the window —
    /// "what has it done so far". An index means that sample and no other, so an
    /// index the series does not have is nil rather than the end: a caller that
    /// trusts an index it computed elsewhere should get nothing, not a plausible
    /// number for a different point.
    private func target(at index: Int?) -> AssetChartPointDTO? {
        guard let index else { return points.last }
        return point(at: index)
    }

    /// Fixed-point, POSIX, so no formatter downstream ever sees an exponent.
    public static func ratioString(_ ratio: Double) -> String {
        guard ratio.isFinite else { return "0.000000" }
        return String(format: "%.6f", locale: Locale(identifier: "en_US_POSIX"), ratio)
    }

    // MARK: - Scrubbing

    /// Index of the point nearest `date`, or nil when there is nothing to select.
    ///
    /// A drag reports a continuous x; the series is a few dozen to a few hundred
    /// samples. Binary search for the insertion point, then take whichever
    /// neighbour is closer in time, so the dot the member drags never jumps a
    /// sample ahead of their finger.
    public func nearestIndex(to date: Date) -> Int? {
        guard !points.isEmpty else { return nil }
        let target = Int64(date.timeIntervalSince1970.rounded())
        var low = 0
        var high = points.count - 1
        if target <= points[low].timestamp { return low }
        if target >= points[high].timestamp { return high }
        while low + 1 < high {
            let mid = (low + high) / 2
            if points[mid].timestamp == target { return mid }
            if points[mid].timestamp < target {
                low = mid
            } else {
                high = mid
            }
        }
        let before = target - points[low].timestamp
        let after = points[high].timestamp - target
        // A tie takes the earlier point, so the same x always resolves to the same
        // sample rather than flickering between two.
        return after < before ? high : low
    }

    public func nearestPoint(to date: Date) -> AssetChartPointDTO? {
        nearestIndex(to: date).flatMap(point(at:))
    }

    // MARK: - Whose price this is

    /// "AAPL on its home exchange" — the caption under a curve drawn from the
    /// underlying equity's candles while the hero price above it is the token's.
    ///
    /// The two are different units: the hero is the B20 token's Chainlink
    /// total-return mark, per token, which carries the token's multiplier; the
    /// curve is Pyth's price for the share. A curve that ends a little below the
    /// price on screen is two instruments, not a bug. "AAPL token on Base" when the
    /// backend fell back to the token's own Chainlink rounds. Nil when the backend
    /// did not say which instrument the series is, in which case the chart is drawn
    /// uncaptioned rather than under a guess.
    ///
    /// Worded in one place (`MarketPriceBasisCaption`), shared with the stats grid.
    public var basisCaption: String? {
        MarketPriceBasisCaption.caption(basis: basis, symbol: basisSymbol)
    }

    /// The instrument's display ticker ("AAPL") when the curve is the underlying
    /// equity's, which is the one case where a figure folded from it is about a
    /// different instrument from the token price above it. Nil for the token's own
    /// rounds and for a series the backend would not name.
    public var underlyingDisplaySymbol: String? {
        guard basis == .underlying, let basisSymbol, !basisSymbol.isEmpty else { return nil }
        return AssetSymbolFormatter.display(basisSymbol)
    }

    // MARK: - Which session a day chart draws

    /// True when this is a day chart of one exchange session.
    ///
    /// The backend's Benchmarks 1D window is the most recent trading session from
    /// the exchange calendar: 04:00 to 20:00 ET of that day, measured against the
    /// previous session's close. On a Saturday that is Friday's session, so calling
    /// its move "Past day" would be wrong. The Hermes sampler and the Chainlink
    /// rounds are a rolling 24 hours instead, which "Past day" does describe.
    public var drawsOneSession: Bool {
        range == .oneDay && source == .benchmarks && !points.isEmpty
    }

    /// The session this day chart draws, named for the header, or nil when it is not
    /// a one-session chart.
    public func session(now: Date, locale: Locale = .autoupdatingCurrent) -> ChartSessionDay? {
        guard drawsOneSession, let last = points.last else { return nil }
        return ChartSessionDay(sessionInstant: last.date, now: now, locale: locale)
    }
}

/// The trading session a day chart draws, as the header names it: "Today", or the
/// session's own date ("Fri, Sep 25") when it is not today's.
///
/// The day is the exchange's calendar day, not the reader's: a session is Friday's
/// on the exchange whatever time it is in the reader's time zone, and in Tokyo the
/// end of Friday's session is already Saturday morning. Only the date is shown, so
/// no wall-clock time is converted here.
public struct ChartSessionDay: Equatable, Sendable {
    /// US equities trade on New York's calendar.
    public static let exchangeTimeZone = TimeZone(identifier: "America/New_York")!

    public let isToday: Bool
    /// "Fri, Sep 25", in the reader's locale.
    public let date: String

    public init(sessionInstant: Date, now: Date, locale: Locale = .autoupdatingCurrent) {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = Self.exchangeTimeZone
        isToday = calendar.isDate(sessionInstant, inSameDayAs: now)
        calendar.locale = locale
        date = SharedFormatters.string(
            from: sessionInstant,
            pattern: .template("EEEMMMd"),
            locale: locale,
            calendar: calendar,
            timeZone: Self.exchangeTimeZone
        )
    }

    /// What the header prints beside the move: "Today" or "Fri, Sep 25".
    public var caption: String { isToday ? "Today" : date }

    /// The same, as it reads inside a sentence: "today" or "on Fri, Sep 25".
    public var spokenPhrase: String { isToday ? "today" : "on \(date)" }
}

/// The time under a scrubbed price: "Tue 2:05 PM" on a day chart, "Tue, Sep 22"
/// inside a month, "Sep 22, 2026" over a year.
///
/// The series is UTC; this is the one place it becomes a local wall clock, and the
/// pattern is a locale template rather than a fixed format so a 24-hour locale
/// reads "Tue 14:05" instead of an American clock.
public enum ChartScrubLabel {
    public static func caption(
        for date: Date,
        range: AssetChartRange,
        locale: Locale = .autoupdatingCurrent,
        timeZone: TimeZone = .autoupdatingCurrent
    ) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.locale = locale
        calendar.timeZone = timeZone
        return SharedFormatters.string(
            from: date,
            pattern: .template(template(for: range)),
            locale: locale,
            calendar: calendar,
            timeZone: timeZone
        )
    }

    /// `j` is the locale's own hour field: 12-hour with an AM/PM marker where that
    /// is how people read a clock, 24-hour where it is not.
    private static func template(for range: AssetChartRange) -> String {
        switch range {
        case .oneDay: return "EEEjmm"
        case .oneWeek, .oneMonth, .threeMonths: return "EEEMMMd"
        case .oneYear, .all: return "MMMdyyyy"
        }
    }
}
