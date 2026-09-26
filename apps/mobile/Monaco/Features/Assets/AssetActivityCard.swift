import MonacoCore
import SwiftUI

/// "Activity on AAPLx": what the member's cabals have actually done with this stock.
///
/// The cabals' own news, beside the market's in the News section (lane: news): "Weekend
/// investors bought $905 of AAPLx" is the thing a member wants to know when they are
/// looking at a stock their friends already own.
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
                    if index > 0 {
                        AssetCardDivider(leading: AssetCardDivider.inset(afterMark: ActivityRow.glyphWidth))
                    }
                    ActivityRow(line: line, openCabal: openCabal)
                }
                if lines.count > Self.visibleCount {
                    AssetSectionTextButton(title: showsAll ? "Show less" : "Show \(lines.count - Self.visibleCount) more") {
                        if reduceMotion {
                            showsAll.toggle()
                        } else {
                            withAnimation(.easeInOut(duration: 0.2)) { showsAll.toggle() }
                        }
                    }
                    // Lined up with the sentences, not with the glyphs.
                    .padding(.leading, AssetCardDivider.inset(afterMark: ActivityRow.glyphWidth))
                    .padding(.top, MonacoTheme.Space.xs)
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

    /// The glyph's column. The rules between rows start past it, under the sentence.
    static let glyphWidth: CGFloat = 22

    private var content: some View {
        // On the sentence's first baseline: the glyph and the age sit on the line they
        // are about, not at the top of a two-line row.
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.sm) {
            Image(systemName: line.glyph)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(tint)
                .frame(width: Self.glyphWidth)
                .accessibilityHidden(true)
            Text(line.title)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            Text(line.age)
                .font(MonacoTheme.Typo.stamp)
                .foregroundStyle(MonacoTheme.tertiaryText)
                .lineLimit(1)
                .fixedSize()
        }
        .padding(.vertical, MonacoTheme.Space.sm)
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
