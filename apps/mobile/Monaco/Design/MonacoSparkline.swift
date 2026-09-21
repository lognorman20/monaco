import MonacoCore
import SwiftUI

/// The day's shape, at row size. One view for every list that draws one — the
/// Stocks tab, the mover cards and the cabal's holdings — so the three cannot
/// drift apart.
///
/// Built for lists first. The heights arrive already normalised from
/// `SparklineSeries`, so `body` does no arithmetic over the series; the path is a
/// `Shape`, which SwiftUI caches and redraws only when the rect or the heights
/// change; and there is no animation, because a line that animates on every cell
/// reuse is what makes a fast list feel slow. The draw-on belongs on the detail
/// chart, where there is one of them.
///
/// Decorative by design: the price and the day-change pill next to it say the same
/// thing in words, so this is hidden from VoiceOver rather than described badly.
struct Sparkline: View {
    let series: SparklineSeries
    /// Tinted by the day's sign, which comes from the reported day change rather
    /// than from the series: the change is measured against the previous close, so
    /// a stock can be down on the day while the drawn window slopes up.
    let tone: PnLTone
    var width: CGFloat = 56
    var height: CGFloat = 24

    /// A hair under the row height so the line never touches the separator.
    private var lineWidth: CGFloat { 1.5 }

    private var stroke: Color {
        switch tone {
        case .profit: return MonacoTheme.profitVivid
        case .loss: return MonacoTheme.lossVivid
        case .flat: return MonacoTheme.tertiaryText
        }
    }

    var body: some View {
        SparklineShape(heights: series.heights, inset: lineWidth / 2)
            .stroke(stroke, style: StrokeStyle(lineWidth: lineWidth, lineCap: .round, lineJoin: .round))
            .background {
                SparklineShape(heights: series.heights, inset: lineWidth / 2, closed: true)
                    .fill(
                        LinearGradient(
                            colors: [stroke.opacity(0.22), stroke.opacity(0)],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    )
            }
            .frame(width: width, height: height)
            .accessibilityHidden(true)
    }
}

/// The path itself. `closed` drops the line to the baseline and back so the same
/// geometry can be filled as an area under the curve.
struct SparklineShape: Shape {
    let heights: [Double]
    var inset: CGFloat = 0
    var closed = false

    func path(in rect: CGRect) -> Path {
        var path = Path()
        guard heights.count >= 2, rect.width > 0, rect.height > 0 else { return path }

        let drawable = rect.insetBy(dx: 0, dy: inset)
        let step = drawable.width / CGFloat(heights.count - 1)
        let points = heights.enumerated().map { index, value in
            CGPoint(
                x: drawable.minX + CGFloat(index) * step,
                // A height of 1 is the window's high, which is the top of the box.
                y: drawable.maxY - CGFloat(value.clamped(to: 0...1)) * drawable.height
            )
        }

        path.move(to: points[0])
        for point in points.dropFirst() {
            path.addLine(to: point)
        }
        if closed {
            path.addLine(to: CGPoint(x: points[points.count - 1].x, y: rect.maxY))
            path.addLine(to: CGPoint(x: points[0].x, y: rect.maxY))
            path.closeSubpath()
        }
        return path
    }
}

private extension Double {
    func clamped(to range: ClosedRange<Double>) -> Double {
        Swift.min(Swift.max(self, range.lowerBound), range.upperBound)
    }
}

#Preview {
    VStack(alignment: .leading, spacing: 16) {
        Sparkline(series: SparklineSeries(usdcMicros: MarketSampleData.spark())!, tone: .profit)
        Sparkline(series: SparklineSeries(usdcMicros: MarketSampleData.spark(driftUsdcMicros: -8_000_000))!, tone: .loss)
        Sparkline(series: SparklineSeries(usdcMicros: Array(repeating: 100_000, count: 12))!, tone: .flat)
    }
    .padding()
    .monacoCanvas()
}
