import MonacoCore
import SwiftUI

/// What the pot is made of, as one bar: every stock's share of the pot's value in the cabal's
/// own tint, deepest first, and the cash left over at the end in paper.
///
/// A holdings list says what the cabal owns; this says how much of the fund each thing is,
/// which is the question a member asks before proposing the next buy. It draws only from
/// figures already on the pot rows — no valuation happens here.
struct PotMixBar: View {
    let pot: [PotRowDTO]
    let groupId: String

    struct Segment: Identifiable, Equatable {
        let symbol: String
        let fraction: Double
        let isCash: Bool
        var id: String { symbol }
    }

    /// Stocks by value, largest first, then cash. Zero-value rows are dropped rather than
    /// drawn as a sliver.
    static func segments(for pot: [PotRowDTO]) -> [Segment] {
        let values = pot.compactMap { row -> (PotRowDTO, Double)? in
            guard let value = GroupHeroMath.decimal(from: row.valueUsd) else { return nil }
            let double = (value as NSDecimalNumber).doubleValue
            return double > 0 ? (row, double) : nil
        }
        let total = values.reduce(0) { $0 + $1.1 }
        guard total > 0 else { return [] }
        let stocks = values
            .filter { !PotSectionView.isCash($0.0) }
            .sorted { $0.1 > $1.1 }
            .map { Segment(symbol: AssetSymbolFormatter.display($0.0.symbol), fraction: $0.1 / total, isCash: false) }
        let cash = values.filter { PotSectionView.isCash($0.0) }.reduce(0) { $0 + $1.1 }
        return stocks + (cash > 0 ? [Segment(symbol: "Cash", fraction: cash / total, isCash: true)] : [])
    }

    private var segments: [Segment] { Self.segments(for: pot) }

    private var tint: MonacoTheme.CabalTint { .forGroupId(groupId) }

    var body: some View {
        if !segments.isEmpty {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                GeometryReader { geometry in
                    HStack(spacing: 2) {
                        ForEach(segments) { segment in
                            Rectangle()
                                .fill(color(for: segment))
                                .frame(width: max(2, (geometry.size.width - CGFloat(segments.count - 1) * 2) * segment.fraction))
                        }
                    }
                }
                .frame(height: 8)
                .clipShape(Capsule())
                legend
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Pot mix")
            .accessibilityValue(spoken)
            .accessibilityIdentifier("pot-mix")
        }
    }

    /// The first four slices named, in the market's voice; the rest are on the rows below.
    private var legend: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            ForEach(segments.prefix(4)) { segment in
                HStack(spacing: 5) {
                    Circle()
                        .fill(color(for: segment))
                        .frame(width: 7, height: 7)
                    Text("\(segment.symbol) \(Self.percent(segment.fraction))")
                        .font(MonacoTheme.Typo.dataCaption)
                        .foregroundStyle(MonacoTheme.muted)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 0)
        }
    }

    /// The cabal's tint, stepping down the ladder one holding at a time; cash is the paper.
    private func color(for segment: Segment) -> Color {
        if segment.isCash { return MonacoTheme.surfaceSunken }
        let index = segments.firstIndex(of: segment) ?? 0
        let opacities: [Double] = [1, 0.7, 0.5, 0.36, 0.26]
        return tint.fill.opacity(opacities[min(index, opacities.count - 1)])
    }

    static func percent(_ fraction: Double) -> String {
        let value = fraction * 100
        return value < 1 ? "<1%" : String(format: "%.0f%%", value)
    }

    private var spoken: String {
        segments.map { "\($0.symbol) \(Self.percent($0.fraction))" }.joined(separator: ", ")
    }
}
