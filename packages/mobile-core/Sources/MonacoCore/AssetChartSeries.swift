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
    /// Ascending by timestamp. `init` sorts, because the nearest-point search is a
    /// binary search and a series that arrived out of order would silently return
    /// the wrong point rather than fail.
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
        self.points = points.sorted { $0.timestamp < $1.timestamp }
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
    public func changeRatio(toIndex index: Int? = nil) -> String? {
        guard let baseline = baselineValue, baseline > 0 else { return nil }
        let target = index.flatMap(point(at:)) ?? points.last
        guard let value = target?.chartValue else { return nil }
        return Self.ratioString(value / baseline - 1)
    }

    /// The same move in dollars — "5.50", "-1.20" — for the badge beside the percent.
    ///
    /// Both legs come from the curve, never from the hero price above it: the curve is
    /// the underlying equity's and the hero is the token's, and subtracting one from
    /// the other would quietly fold the premium between them into the day's move.
    public func changeDollars(toIndex index: Int? = nil) -> String? {
        guard let baseline = baselineValue else { return nil }
        let target = index.flatMap(point(at:)) ?? points.last
        guard let value = target?.chartValue else { return nil }
        let delta = value - baseline
        guard delta.isFinite else { return nil }
        return String(format: "%.2f", locale: Locale(identifier: "en_US_POSIX"), delta)
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
    /// The two differ by the premium the stock-vs-token card exists to show, so a
    /// curve that ends below the price on screen is two instruments, not a bug. Nil
    /// when the backend did not say which instrument the series is, in which case
    /// the chart is drawn uncaptioned rather than under a guess.
    public var basisCaption: String? {
        guard let basisSymbol, !basisSymbol.isEmpty else { return nil }
        switch basis {
        case .underlying: return "\(basisSymbol) on its home exchange"
        case .token: return "\(basisSymbol) on Solana"
        case .unknown, nil: return nil
        }
    }
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
