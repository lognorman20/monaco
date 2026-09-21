import Foundation

/// The two-column stats grid, as cells rather than as a view.
///
/// Every figure here is optional at the source, so the decision that matters is
/// which cells exist at all. A cell we could not source is dropped, never filled
/// with a placeholder number and never shown as an empty row with a label and a
/// dash — a grid of dashes is worse than a shorter grid.
///
/// Market cap, P/E and dividend yield are deliberately absent. Nothing behind
/// xStocks publishes them, and six honest cells beat nine with three invented.
public struct AssetStatsGrid: Equatable, Sendable {
    public struct Cell: Equatable, Sendable, Identifiable {
        /// A stable key, so a UI test can ask for one cell by name.
        public let id: String
        public let label: String
        public let value: String
        /// The long form for VoiceOver, when the short label would be read wrong:
        /// "Prev close" is not a word.
        public let spokenLabel: String

        public init(id: String, label: String, value: String, spokenLabel: String? = nil) {
            self.id = id
            self.label = label
            self.value = value
            self.spokenLabel = spokenLabel ?? label
        }

        public var spoken: String { "\(spokenLabel), \(value)" }
    }

    public let cells: [Cell]
    /// "AAPL on its home exchange" — which instrument the candle-derived cells are
    /// about. The hero price above them is the token's, so a current price above
    /// "the day's high" is two instruments and not a bug. Nil when the backend did
    /// not say, in which case the grid is drawn unheaded rather than under a guess.
    public let basisCaption: String?
    /// Where in its 52-week range the stock is sitting, 0...1, when both ends and a
    /// current price are known. The bar is the one part of this card that is worth
    /// more than the number it draws.
    public let week52Position: Double?
    public let week52LowLabel: String?
    public let week52HighLabel: String?

    public var isEmpty: Bool { cells.isEmpty }

    /// Builds the grid, or nil when nothing could be sourced.
    ///
    /// - Parameter currentUsdcMicros: the price the 52-week bar places. It is the
    ///   token's while the range is the equity's, which is close enough to position
    ///   a marker and is never shown as a figure.
    public static func make(_ stats: AssetStatsDTO?, currentUsdcMicros: Int64? = nil) -> AssetStatsGrid? {
        guard let stats, !stats.isEmpty else { return nil }

        var cells: [Cell] = []
        func money(_ id: String, _ label: String, _ micros: Int64?, spoken: String? = nil) {
            guard let micros, micros > 0 else { return }
            cells.append(Cell(id: id, label: label, value: UsdAmountFormatter.format(micros: micros), spokenLabel: spoken))
        }

        money("open", "Open", stats.openUsdcMicros, spoken: "Opening price")
        money("high", "Day high", stats.highUsdcMicros)
        money("low", "Day low", stats.lowUsdcMicros)
        money("prev-close", "Prev close", stats.previousCloseUsdcMicros, spoken: "Previous close")
        money("52w-high", "52-week high", stats.week52HighUsdcMicros)
        money("52w-low", "52-week low", stats.week52LowUsdcMicros)

        if let spreadBps = stats.spreadBps, spreadBps >= 0 {
            cells.append(Cell(
                id: "spread",
                label: "Trading cost",
                value: spreadCopy(spreadBps),
                spokenLabel: "Round trip trading cost"
            ))
        }
        if let conf = stats.confUsdcMicros, conf > 0 {
            // Pyth's own confidence interval, which nothing else in the app has ever
            // shown. "Price certainty" rather than "confidence" because a member is
            // not reading a statistics paper.
            cells.append(Cell(
                id: "certainty",
                label: "Price certainty",
                value: "±\(UsdAmountFormatter.format(micros: conf))",
                spokenLabel: "Price certainty, give or take"
            ))
        }

        guard !cells.isEmpty else { return nil }

        return AssetStatsGrid(
            cells: cells,
            basisCaption: stats.basisCaption,
            week52Position: week52Position(stats, currentUsdcMicros: currentUsdcMicros),
            week52LowLabel: stats.week52LowUsdcMicros.map { UsdAmountFormatter.format(micros: $0) },
            week52HighLabel: stats.week52HighUsdcMicros.map { UsdAmountFormatter.format(micros: $0) }
        )
    }

    /// "~0.12%" — a round-trip cost, phrased as an estimate because it is one probe
    /// at one size, not a quoted fee.
    static func spreadCopy(_ bps: Int) -> String {
        let percent = Double(bps) / 100
        if percent < 0.01 && bps > 0 { return "< 0.01%" }
        return "~\(String(format: "%.2f", percent))%"
    }

    /// Where the current price sits between the year's low and high, clamped to the
    /// bar's own ends. A price outside the range is a real thing — a new high is a
    /// new high — and the marker pins to the end rather than running off the track.
    static func week52Position(_ stats: AssetStatsDTO, currentUsdcMicros: Int64?) -> Double? {
        guard let low = stats.week52LowUsdcMicros,
              let high = stats.week52HighUsdcMicros,
              let current = currentUsdcMicros,
              low > 0, high > low
        else { return nil }
        let position = Double(current - low) / Double(high - low)
        return min(max(position, 0), 1)
    }
}
