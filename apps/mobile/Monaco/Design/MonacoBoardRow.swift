import MonacoCore
import SwiftUI

/// One line of a leaderboard: the rank in the market's voice, the face, the name, and the
/// return on the right. Rank 1 wears the crown.
///
/// Every board in the app — Home's investors, the cabal's members, the platform's cabals — is
/// the same argument ("who is ahead"), so it is one row. The crown is the one place gold draws
/// as a glyph rather than as a coin: it means *first*, and the row prints the number beside it
/// so the meaning never rests on the colour alone.
struct BoardRow<Leading: View>: View {
    /// Nil on a list that is not ranked (search results), which drops the rank column.
    let rank: Int?
    let name: String
    /// A second line under the name — "3 members · Open", "(you)" and the like. Optional.
    var detail: String? = nil
    let percentReturn: String?
    /// The figure under the return: a signed P&L, or a pot value for a cabal board.
    var dollarPnl: String? = nil
    var potValueUsd: String? = nil
    /// The viewer's own row is washed in the brand so it can be found in a long board.
    var isViewer = false
    var isLast = false
    var chevron = false
    @ViewBuilder let leading: Leading

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var titleWidthFloor = MonacoRowLayout.baseMinimumTitleWidth

    private var layout: MonacoRowLayout {
        MonacoRowLayout(dynamicTypeSize: dynamicTypeSize, scaledTitleWidthFloor: titleWidthFloor)
    }

    private var isLeader: Bool { rank == 1 }

    var body: some View {
        Group {
            if layout.isStacked {
                // The figures drop under the name at the accessibility sizes, the way every
                // `MonacoRow` stacks, instead of squeezing the name to an ellipsis.
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    HStack(spacing: MonacoTheme.Space.sm) {
                        if rank != nil { rankColumn }
                        leading.frame(width: 40, height: 40)
                        labels
                        if chevron { chevronGlyph }
                    }
                    figures(alignment: .leading)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            } else {
                HStack(spacing: MonacoTheme.Space.sm) {
                    if rank != nil { rankColumn }
                    leading.frame(width: 40, height: 40)
                    labels
                        .frame(minWidth: layout.minimumTitleWidth, alignment: .leading)
                    figures(alignment: .trailing)
                        .layoutPriority(1)
                    if chevron { chevronGlyph }
                }
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)

        .padding(.vertical, 8)
        .frame(minHeight: 60)
        .background(isViewer ? MonacoTheme.brandWash : Color.clear)
        .contentShape(Rectangle())
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule().padding(.leading, ruleInset)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(spoken)
    }

    /// The rule starts under the name, past the rank (when there is one) and the face.
    private var ruleInset: CGFloat {
        let rankColumnWidth: CGFloat = rank == nil ? 0 : 24 + MonacoTheme.Space.sm
        return MonacoTheme.Space.m + rankColumnWidth + 40 + MonacoTheme.Space.sm
    }

    private func figures(alignment: HorizontalAlignment) -> some View {
        VStack(alignment: alignment, spacing: 2) {
            PercentText(percentReturn: percentReturn, style: .row)
            if let dollarPnl {
                PnLText(dollarPnl: dollarPnl, style: .caption)
            } else if let potValueUsd {
                MoneyText(decimalString: potValueUsd, style: .caption, color: MonacoTheme.muted)
            }
        }
    }

    private var chevronGlyph: some View {
        Image(systemName: "chevron.right")
            .font(.footnote.weight(.semibold))
            .foregroundStyle(MonacoTheme.tertiaryText)
            .accessibilityHidden(true)
    }

    @ViewBuilder
    private var rankColumn: some View {

        ZStack {
            if isLeader {
                Image(systemName: "crown.fill")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(MonacoTheme.goldGlyph)
            } else if let rank {
                Text("\(rank)")
                    .font(MonacoTheme.Typo.data)
                    .foregroundStyle(MonacoTheme.tertiaryText)
            }
        }
        .frame(width: 24, alignment: .center)
        .accessibilityHidden(true)
    }

    private var labels: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(name)
                .font(MonacoTheme.Typo.rowTitle)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(layout.titleLineLimit)
                .truncationMode(.tail)
            if let detail, !detail.isEmpty {
                Text(detail)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .lineLimit(layout.subtitleLineLimit)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var spoken: String {
        var sentence = name
        if let rank { sentence = isLeader ? "First, \(name)" : "Rank \(rank), \(name)" }
        if let detail, !detail.isEmpty { sentence += ", \(detail)" }
        sentence += ", \(PnLSpeech.percent(PercentReturnFormatter.format(percentReturn)))"
        if let dollarPnl { sentence += ", \(PnLSpeech.dollars(dollarPnl))" }
        if let potValueUsd { sentence += ", pot \(UsdAmountFormatter.format(decimalString: potValueUsd))" }
        return sentence
    }
}

/// Placeholder rows in the shape of `BoardRow`: a rank slot, a 40pt face, a name and a figure,
/// ruled top and bottom like the list they stand in for.
struct BoardRowSkeleton: View {
    var rows: Int = 3

    var body: some View {
        VStack(spacing: 0) {
            ForEach(0..<rows, id: \.self) { index in
                HStack(spacing: MonacoTheme.Space.sm) {
                    SkeletonBlock(width: 18, height: 12)
                    SkeletonBlock(width: 40, height: 40, radius: 20)
                    VStack(alignment: .leading, spacing: 6) {
                        SkeletonBlock(width: 132, height: 14)
                        SkeletonBlock(width: 72, height: 11)
                    }
                    Spacer(minLength: MonacoTheme.Space.s)
                    VStack(alignment: .trailing, spacing: 6) {
                        SkeletonBlock(width: 56, height: 14)
                        SkeletonBlock(width: 40, height: 11)
                    }
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .padding(.vertical, MonacoTheme.Space.sm)
                .overlay(alignment: .bottom) {
                    if index < rows - 1 {
                        MonacoRule().padding(.leading, MonacoTheme.Space.m)
                    }
                }
            }
        }
        .overlay(alignment: .top) { MonacoRule() }
        .overlay(alignment: .bottom) { MonacoRule() }
        .accessibilityHidden(true)
    }
}
