import MonacoCore
import SwiftUI

/// The stats grid: two columns of the figures we can actually source, plus the
/// 52-week bar, which is the one part of this card worth more than the number it
/// draws.
///
/// Which cells exist is decided in `AssetStatsGrid` (MonacoCore, tested). A cell we
/// could not source is absent, never a dash and never a placeholder number.
struct AssetStatsCard: View {
    let grid: AssetStatsGrid

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// One column above the accessibility sizes: two columns of money at those sizes
    /// truncate the figures, and a truncated price is worse than a longer card.
    private var columnCount: Int { dynamicTypeSize.isAccessibilitySize ? 1 : 2 }

    /// The cells laid into rows up front.
    ///
    /// Plain stacks rather than a `LazyVGrid`: there are at most eight cells, all of
    /// them known before the card draws, so laziness buys nothing — and it cost
    /// something real. A lazy grid only builds the rows near the viewport, so the
    /// cells were missing from the accessibility tree whenever the card was not the
    /// part of the screen being looked at, which made every one of them unreachable
    /// to VoiceOver and to a UI test alike.
    private var rows: [[AssetStatsGrid.Cell]] {
        stride(from: 0, to: grid.cells.count, by: columnCount).map { start in
            Array(grid.cells[start..<min(start + columnCount, grid.cells.count)])
        }
    }

    var body: some View {
        AssetDetailCard(
            title: "Stats",
            trailing: grid.basisCaption,
            identifier: "asset-detail-stats"
        ) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                    ForEach(rows, id: \.first?.id) { row in
                        HStack(alignment: .top, spacing: MonacoTheme.Space.m) {
                            ForEach(row) { cell in
                                StatCell(cell: cell)
                            }
                            // An odd last row keeps its cell in the left column
                            // rather than letting it stretch across both.
                            if row.count < columnCount {
                                Color.clear.frame(maxWidth: .infinity)
                            }
                        }
                    }
                }
                if let position = grid.week52Position {
                    Week52Bar(
                        position: position,
                        lowLabel: grid.week52LowLabel,
                        highLabel: grid.week52HighLabel
                    )
                }
            }
        }
    }
}

private struct StatCell: View {
    let cell: AssetStatsGrid.Cell

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(cell.label)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
                .lineLimit(1)
                .minimumScaleFactor(0.85)
            Text(cell.value)
                .moneyFont(.row)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(cell.spoken)
        .accessibilityIdentifier("asset-stat-\(cell.id)")
    }
}

/// Where the price sits in its year. A track, a marker, and the two ends labelled —
/// no axis, no ticks: the whole point is that it is read in one glance.
private struct Week52Bar: View {
    let position: Double
    let lowLabel: String?
    let highLabel: String?

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var hasAppeared = false

    private var drawnPosition: Double {
        // The marker slides in from the low end on first draw, so the eye follows it
        // to where the price is. With Reduce Motion it is simply there.
        hasAppeared || reduceMotion ? position : 0
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            GeometryReader { geometry in
                let width = geometry.size.width
                ZStack(alignment: .leading) {
                    Capsule()
                        .fill(MonacoTheme.surfaceSunken)
                        .frame(height: 6)
                    Capsule()
                        .fill(MonacoTheme.brand)
                        .frame(width: 3, height: 16)
                        // Clamped to the track: a marker at 1.0 would otherwise sit
                        // half outside the capsule's right edge.
                        .offset(x: min(max(drawnPosition * width - 1.5, 0), max(width - 3, 0)))
                }
                .frame(height: 16)
            }
            .frame(height: 16)
            .animation(reduceMotion ? nil : .easeOut(duration: 0.5), value: drawnPosition)
            .onAppear { hasAppeared = true }

            HStack {
                Text(lowLabel ?? "")
                Spacer(minLength: MonacoTheme.Space.s)
                Text(highLabel ?? "")
            }
            .font(MonacoTheme.Typo.micro)
            .foregroundStyle(MonacoTheme.tertiaryText)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("52 week range")
        .accessibilityValue(spokenPosition)
        .accessibilityIdentifier("asset-stat-52w-bar")
    }

    /// "near the top of its 52 week range, $163.00 to $262.00" — the bar says
    /// something a list of two numbers does not, so VoiceOver gets that sentence too.
    private var spokenPosition: String {
        let place: String
        switch position {
        case ..<0.2: place = "near the bottom of its 52 week range"
        case ..<0.45: place = "in the lower half of its 52 week range"
        case ..<0.55: place = "around the middle of its 52 week range"
        case ..<0.8: place = "in the upper half of its 52 week range"
        default: place = "near the top of its 52 week range"
        }
        guard let lowLabel, let highLabel else { return place }
        return "\(place), \(lowLabel) to \(highLabel)"
    }
}
