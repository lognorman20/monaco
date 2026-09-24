import MonacoCore
import SwiftUI

/// "Activity on AAPLx": what the member's cabals have actually done with this stock.
///
/// Monaco's answer to a news feed. There is no news vendor behind this app, and a
/// fabricated headline would be worse than none — but "Weekend investors bought $905
/// of AAPLx" is both true and the thing a member wants to know when they are looking
/// at a stock their friends already own.
///
/// The wording is `AssetActivityCopy` (MonacoCore, tested).
struct AssetActivityCard: View {
    let symbol: String
    let activity: [AssetActivityDTO]
    var openCabal: ((String) -> Void)?

    /// Enough to read at a glance; the cabal's own activity screen has the rest.
    private static let visibleCount = 6

    @State private var showsAll = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var lines: [AssetActivityCopy.Line] {
        AssetActivityCopy.lines(activity, symbol: symbol)
    }

    private var visibleLines: [AssetActivityCopy.Line] {
        showsAll ? lines : Array(lines.prefix(Self.visibleCount))
    }

    var body: some View {
        AssetDetailCard(
            title: "Activity on \(AssetSymbolFormatter.format(symbol))",
            identifier: "asset-detail-activity"
        ) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(visibleLines.enumerated()), id: \.element.id) { index, line in
                    ActivityRow(line: line, openCabal: openCabal)
                    if index < visibleLines.count - 1 {
                        AssetCardDivider()
                    }
                }
                if lines.count > Self.visibleCount {
                    Button(showsAll ? "Show less" : "Show \(lines.count - Self.visibleCount) more") {
                        if reduceMotion {
                            showsAll.toggle()
                        } else {
                            withAnimation(.easeInOut(duration: 0.2)) { showsAll.toggle() }
                        }
                    }
                    .font(MonacoTheme.Typo.callout.weight(.semibold))
                    .foregroundStyle(MonacoTheme.brand)
                    .frame(minHeight: 44, alignment: .leading)
                    .accessibilityIdentifier("asset-activity-toggle")
                }
            }
        }
    }
}

private struct ActivityRow: View {
    let line: AssetActivityCopy.Line
    var openCabal: ((String) -> Void)?

    var body: some View {
        Group {
            if let openCabal {
                Button { openCabal(line.groupId) } label: { content }
                    .buttonStyle(.plain)
            } else {
                content
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(line.spoken)
        .accessibilityAddTraits(openCabal == nil ? [] : .isButton)
        .accessibilityIdentifier("asset-activity-row-\(line.id)")
    }

    private var content: some View {
        HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
            Image(systemName: line.glyph)
                .font(.system(size: 15))
                .foregroundStyle(tint)
                .frame(width: 22, height: 22)
                .accessibilityHidden(true)
            Text(line.title)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: MonacoTheme.Space.s)
            Text(line.age)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.tertiaryText)
                .monospacedDigit()
        }
        .padding(.vertical, MonacoTheme.Space.s)
        .frame(minHeight: 44)
        .contentShape(Rectangle())
    }

    /// Red is money. A vote that failed cost nobody anything, so the glyph carries
    /// the tone and the text stays ink.
    private var tint: Color {
        switch line.tone {
        case .positive: return MonacoTheme.profit
        case .negative: return MonacoTheme.loss
        case .neutral: return MonacoTheme.muted
        }
    }
}
