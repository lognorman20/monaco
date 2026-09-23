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

/// Where a sparkline takes its colour from.
///
/// Normally the reported day change, which is measured against the previous close
/// and so can legitimately disagree with the slope of the drawn window — a stock
/// can be down on the day while the last few hours rose. That is a known, correct
/// disagreement about one instrument.
///
/// But when the series and the change are about *different instruments*, the
/// change's sign says nothing at all about this line. A B20 token and the equity
/// it tracks are different instruments (the token carries a multiplier and trades
/// its pools when the exchange is shut), which is the premise of the stock-vs-token
/// card on the detail screen. In that case the line is tinted from its own first
/// and last close, so the colour describes the picture the member is looking at.
///
/// On Base the backend reads the line and the day move from one Pyth series of the
/// underlying, so both are labelled `underlying` and the tint is the day move. This
/// rule is what keeps that true if the two ever come from different places.
///
/// One decision, in one place, for every surface that draws a sparkline next to a
/// day-change pill: the Stocks tab's rows, the mover strip, and a cabal's holdings.
public enum SparkTint: Equatable, Sendable {
    /// Tint from the reported day change: it is about the same instrument as the
    /// line, or nothing has said otherwise.
    case reportedDayChange
    /// Tint from the drawn series itself.
    case series(rising: Bool, flat: Bool)

    public init(series: SparklineSeries?, basesDisagree: Bool) {
        guard let series, basesDisagree else {
            self = .reportedDayChange
            return
        }
        self = .series(
            rising: series.isRising,
            flat: series.lastUsdcMicros == series.firstUsdcMicros
        )
    }
}

/// One market list row, prepared once when its data lands.
///
/// The normalised series is the reason this type exists. Building it inside the row
/// view would redo the same reduction on every layout pass of every visible row,
/// which is precisely the per-row work that costs a list its frame budget. A model
/// builds these when a response arrives; the view only draws them.
public struct MarketRowData: Identifiable, Equatable, Sendable {
    public let asset: MarketAssetDTO
    /// Nil when the backend had no day series for this symbol, in which case the
    /// row draws no sparkline at all.
    public let spark: SparklineSeries?
    /// Replaces the ticker under the name, for the rows that have something more
    /// useful to say there ("2 cabals · your slice $294.70").
    public let subtitle: String?
    /// Read by VoiceOver after the row's figures, for what the subtitle cannot say
    /// in the space it has ("closes in 4 hours").
    public let accessoryLabel: String?

    public var id: String { asset.symbol }

    public init(asset: MarketAssetDTO, subtitle: String? = nil, accessoryLabel: String? = nil) {
        self.asset = asset
        spark = SparklineSeries(usdcMicros: asset.sparkUsdcMicros)
        self.subtitle = subtitle
        self.accessoryLabel = accessoryLabel
    }

    /// Where the row's sparkline takes its colour from; see `SparkTint`.
    public typealias SparkTint = MonacoCore.SparkTint

    public var sparkTint: SparkTint {
        SparkTint(
            series: spark,
            basesDisagree: asset.sparkAndChangeDisagreeOnInstrument
        )
    }

    /// The labelled day move the pill shows ("AAPL day move"), or nil when the
    /// backend did not say whose move it is.
    public var dayMove: StockDayMove? { asset.stockDayMove }

    /// The price the pill's dollar face is measured on: the last close of the row's
    /// line, when the line and the move are the same instrument. Never a price in
    /// the other instrument's unit — the token carries a multiplier the share does
    /// not, so a share's move taken on a token price is a move of neither. Nil
    /// leaves the pill on percent.
    public var dayMoveReferencePriceUsdcMicros: Int64? {
        DayChangeFigures.referencePrice(
            sparkUsdcMicros: asset.sparkUsdcMicros,
            sparkBasis: asset.sparkBasis,
            dayMove: dayMove
        )
    }

    /// The instrument the drawn line is about, when it is not the one the rest of
    /// the row is about. Nil when they agree, so nothing is said that need not be.
    public var sparkBasisNote: String? {
        guard asset.sparkAndChangeDisagreeOnInstrument,
              let basisSymbol = asset.sparkBasisSymbol,
              !basisSymbol.isEmpty
        else { return nil }
        return basisSymbol
    }
}

/// The two faces of a day-change pill: the percent the backend reports, and the
/// dollars that percent is per share of the underlying.
///
/// Robinhood lets the number be tapped to swap between them, and members read the
/// two very differently: a percent compares stocks, a dollar amount answers "how
/// far did it move". Both come from figures already on the row, so the toggle
/// costs no request.
public enum DayChangeFigures {
    /// "+1.24%", "−0.83%", "0.0%", or "—" when there is no day change to show.
    public static func percentText(change24h: String?) -> String {
        PercentReturnFormatter.format(change24h)
    }

    /// The price a day move's dollar face is measured on: the last close of the
    /// row's line, when the line and the move are about the *same* instrument. The
    /// line's last point is then the same latest price the backend measured the
    /// move with, so the dollar face is that instrument's own move.
    ///
    /// The rule used to name the instrument — both had to be the underlying's —
    /// which was the same rule written down for the only case that could arise
    /// then. It is the agreement that matters, not which of the two it is: a
    /// token move on a token line is measured on token prices and is correct, and
    /// a share's move priced at the token's price is wrong whichever way round it
    /// is written. Nil when they disagree or the line is unlabelled, which leaves
    /// the pill on percent rather than showing a figure of neither.
    public static func referencePrice(
        sparkUsdcMicros: [Int64],
        sparkBasis: MarketPriceBasis?,
        dayMove: StockDayMove?
    ) -> Int64? {
        guard let dayMove, let sparkBasis, sparkBasis == dayMove.basis,
              let last = sparkUsdcMicros.last(where: { $0 > 0 })
        else { return nil }
        return last
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
    /// Biggest absolute day move first. Rows with no labelled day move are left out:
    /// a mover strip is about movement, and "unknown" (or a move nobody said whose
    /// it is) is not one.
    public static func rank(_ assets: [MarketAssetDTO], limit: Int = 6) -> [MarketAssetDTO] {
        guard limit > 0 else { return [] }
        let scored = assets.compactMap { asset -> (MarketAssetDTO, Decimal)? in
            guard let ratio = DayChangeFigures.ratio(from: asset.stockDayMove?.ratio) else { return nil }
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
