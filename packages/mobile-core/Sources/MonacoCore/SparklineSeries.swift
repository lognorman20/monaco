import Foundation

/// A row-sized price series, reduced to everything a sparkline needs to be drawn.
///
/// The view gets a finished array of 0...1 heights rather than raw micros, because
/// the reduction is the expensive part and a list row must not do it inside `body`:
/// a scrolling list rebuilds bodies constantly, and normalising two dozen `Int64`s
/// per row per pass is exactly the work that turns a 60fps list into a 45fps one.
/// Building the series is the data layer's job; drawing it is the view's.
///
/// Nil-returning rather than empty-returning on purpose: "there is no series" and
/// "there is a flat series" are different pictures. A stock we have no history for
/// draws nothing; a stock that genuinely did not move draws a straight line.
public struct SparklineSeries: Equatable, Sendable {
    /// Heights in 0...1, in order, where 0 is the window's low and 1 its high.
    public let heights: [Double]
    /// The first and last usable closes, in USDC micros.
    public let firstUsdcMicros: Int64
    public let lastUsdcMicros: Int64

    /// Most points a row will ever draw. A 1D series is about two dozen points by
    /// the time it reaches the app; this is the ceiling for anything denser, so one
    /// odd response cannot make a row expensive.
    public static let maximumPoints = 48

    /// Nil when there is nothing worth drawing: no points, or only one.
    ///
    /// Non-positive micros are dropped before anything else. A zero is not a price
    /// — it is a gap the upstream could not fill — and leaving it in would drag the
    /// whole curve to the bottom of the box and make every other point a flat line
    /// across the top.
    public init?(usdcMicros: [Int64], maximumPoints: Int = SparklineSeries.maximumPoints) {
        let usable = usdcMicros.filter { $0 > 0 }
        guard usable.count >= 2 else { return nil }

        let sampled = Self.downsample(usable, to: maximumPoints)
        let low = sampled.min() ?? 0
        let high = sampled.max() ?? 0
        let span = Double(high - low)

        if span <= 0 {
            // A genuinely flat window: draw it through the middle rather than at the
            // floor, which is where a 0-span normalisation would otherwise put it.
            heights = Array(repeating: 0.5, count: sampled.count)
        } else {
            heights = sampled.map { Double($0 - low) / span }
        }
        firstUsdcMicros = sampled.first ?? 0
        lastUsdcMicros = sampled.last ?? 0
    }

    /// True when the window ended no lower than it started. Only a fallback: the
    /// row tints itself from the day change the backend reported, which is measured
    /// against the previous close and so can disagree with the first drawn point.
    public var isRising: Bool { lastUsdcMicros >= firstUsdcMicros }

    /// Evenly spaced samples that always keep the real first and last point, so the
    /// ends of the drawn line are the ends of the window rather than near them.
    static func downsample(_ values: [Int64], to limit: Int) -> [Int64] {
        guard limit >= 2, values.count > limit else { return values }
        let lastIndex = values.count - 1
        var out: [Int64] = []
        out.reserveCapacity(limit)
        for step in 0..<limit {
            let position = Double(step) * Double(lastIndex) / Double(limit - 1)
            out.append(values[Int(position.rounded())])
        }
        return out
    }
}

/// The two faces of a day-change pill: the percent the backend reports, and the
/// dollars that percent is worth at the current price.
///
/// Robinhood lets the number be tapped to swap between them, and members read the
/// two very differently — a percent compares stocks, a dollar amount answers "what
/// did that cost me". Both come from figures already on the row, so the toggle
/// costs no request.
public enum DayChangeFigures {
    /// "+1.24%", "−0.83%", "0.0%", or "—" when there is no day change to show.
    public static func percentText(change24h: String?) -> String {
        PercentReturnFormatter.format(change24h)
    }

    /// The signed dollar move behind `change24h` at `priceUsdcMicros`, as a raw
    /// decimal string for `SignedUsdFormatter`.
    ///
    /// The change is a ratio against the previous close, so the close is
    /// `price / (1 + ratio)` and the move is `price − close`. Nil when either input
    /// is missing or the ratio implies a non-positive previous close, which is not
    /// a day anything traded through.
    public static func dollarDelta(change24h: String?, priceUsdcMicros: Int64?) -> String? {
        guard let priceUsdcMicros, priceUsdcMicros > 0,
              let ratio = ratio(from: change24h)
        else { return nil }
        let denominator = Decimal(1) + ratio
        guard denominator > 0 else { return nil }
        let price = Decimal(priceUsdcMicros) / Decimal(1_000_000)
        let delta = price * ratio / denominator
        var rounded = Decimal()
        var source = delta
        NSDecimalRound(&rounded, &source, 6, .plain)
        return NSDecimalNumber(decimal: rounded).stringValue
    }

    /// "+$2.84" / "−$1.10" / "$0.00" on a flat day, or nil when the dollar move
    /// cannot be worked out at all — in which case the pill stays on percent rather
    /// than inventing a figure.
    public static func dollarText(change24h: String?, priceUsdcMicros: Int64?) -> String? {
        guard let delta = dollarDelta(change24h: change24h, priceUsdcMicros: priceUsdcMicros) else {
            return nil
        }
        let formatted = SignedUsdFormatter.format(delta)
        return formatted == "—" ? nil : formatted
    }

    /// The ratio as a decimal: "0.0124" and "+1.24%" both mean the same move.
    static func ratio(from raw: String?) -> Decimal? {
        guard let raw else { return nil }
        var trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
            .replacingOccurrences(of: "\u{2212}", with: "-")
        guard !trimmed.isEmpty, trimmed != "—" else { return nil }
        var isPercent = false
        if trimmed.hasSuffix("%") {
            isPercent = true
            trimmed.removeLast()
        }
        // Decimal(string:) accepts "nan" and leading garbage; Double is the gate.
        guard let probe = Double(trimmed), probe.isFinite,
              let value = Decimal(string: trimmed, locale: Locale(identifier: "en_US_POSIX"))
        else { return nil }
        return isPercent ? value / 100 : value
    }
}

/// The day's biggest moves, from a list the app already has.
///
/// Client-side on purpose: "top movers" is a re-sort of the popular rows, not a
/// different set of stocks, so asking the backend for it would be a second request
/// for data already on screen.
public enum TopMovers {
    /// Biggest absolute day move first. Rows with no readable day change are left
    /// out — a mover strip is about movement, and "unknown" is not a move.
    public static func rank(_ assets: [MarketAssetDTO], limit: Int = 6) -> [MarketAssetDTO] {
        guard limit > 0 else { return [] }
        let scored = assets.compactMap { asset -> (MarketAssetDTO, Decimal)? in
            guard let ratio = DayChangeFigures.ratio(from: asset.change24h) else { return nil }
            return (asset, ratio < 0 ? -ratio : ratio)
        }
        // Ties keep catalogue order rather than flipping between refreshes: a strip
        // that reshuffles when nothing moved reads as live data when it is not.
        return scored
            .enumerated()
            .sorted { left, right in
                if left.element.1 != right.element.1 { return left.element.1 > right.element.1 }
                return left.offset < right.offset
            }
            .prefix(limit)
            .map(\.element.0)
    }
}
