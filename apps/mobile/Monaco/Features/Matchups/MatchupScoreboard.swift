import MonacoCore
import SwiftUI

/// How a side's score is set. The leader is the one that stands out — in green when it is a gain,
/// in ink when it is not, because green only ever means "went up" — and the other is plain.
enum MatchupScoreTone: Equatable {
    case leadingGain
    case leading
    case plain

    static func tone(for side: MatchupLeader, leading: MatchupLeader?, score: String?) -> MatchupScoreTone {
        guard leading == side else { return .plain }
        return MatchupCopy.score(score).hasPrefix("+") ? .leadingGain : .leading
    }

    var color: Color {
        switch self {
        case .leadingGain: return MonacoTheme.profit
        case .leading: return MonacoTheme.ink
        case .plain: return MonacoTheme.secondaryText
        }
    }

    var font: Font {
        self == .plain ? MonacoTheme.Typo.data : MonacoTheme.Typo.dataStrong
    }
}

/// The thin bar between two scores. The leader's end fills with its cabal's tint as far as its
/// lead reaches; the rest stays paper. Level is two paper halves with a hairline at the middle.
struct MatchupLeadBar: View {
    let fraction: Double
    let leading: MatchupLeader?
    let groupA: String
    let groupB: String
    var height: CGFloat = 4

    var body: some View {
        GeometryReader { proxy in
            let width = proxy.size.width
            let split = width * fraction
            ZStack(alignment: .leading) {
                Capsule().fill(MonacoTheme.surfaceSunken)
                switch leading {
                case .a:
                    Capsule()
                        .fill(MonacoTheme.CabalTint.fill(forGroupId: groupA))
                        .frame(width: max(height, split))
                case .b:
                    Capsule()
                        .fill(MonacoTheme.CabalTint.fill(forGroupId: groupB))
                        .frame(width: max(height, width - split))
                        .offset(x: split)
                case .tie, nil:
                    Rectangle()
                        .fill(MonacoTheme.tertiaryText)
                        .frame(width: 1, height: height + 4)
                        .offset(x: width / 2)
                }
            }
        }
        .frame(height: height)
        .accessibilityHidden(true)
    }
}

/// Two cabals, their marks and their scores on two lines, the lead bar under them and the days
/// left. Home's card and the cabal screen's section both draw it.
struct MatchupScoreboard: View {
    let matchup: MatchupDTO
    /// A chevron at the end of the last line, for a scoreboard that opens the matchup.
    var showsChevron = false

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            if let b = matchup.b {
                sideRow(matchup.a, side: .a)
                sideRow(b, side: .b)
                MatchupLeadBar(
                    fraction: MatchupCopy.leadFraction(a: matchup.a.score, b: b.score),
                    leading: matchup.leading,
                    groupA: matchup.a.groupID,
                    groupB: b.groupID
                )
                .padding(.top, MonacoTheme.Space.xs)
                HStack {
                    Text(MatchupCopy.daysLeft(matchup.daysLeft))
                        .font(MonacoTheme.Typo.stamp)
                        .foregroundStyle(MonacoTheme.tertiaryText)
                    Spacer(minLength: MonacoTheme.Space.s)
                    chevron
                }
            } else {
                byeRow
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(MatchupCopy.spoken(matchup))
    }

    private var pairScores: (a: String, b: String) {
        MatchupCopy.scores(matchup.a.score, matchup.b?.score)
    }

    @ViewBuilder
    private var chevron: some View {
        if showsChevron {
            Image(systemName: "chevron.right")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(MonacoTheme.tertiaryText)
                .accessibilityHidden(true)
        }
    }

    /// Mark, name and score on one line; at the accessibility sizes the score drops under the
    /// name instead of squeezing it to an ellipsis.
    @ViewBuilder
    private func sideRow(_ side: MatchupSideDTO, side key: MatchupLeader) -> some View {
        let tone = MatchupScoreTone.tone(for: key, leading: matchup.leading, score: side.score)
        let score = Text(key == .a ? pairScores.a : pairScores.b)
            .font(tone.font)
            .monospacedDigit()
            .foregroundStyle(tone.color)
            .lineLimit(1)
        let name = Text(side.name)
            .font(MonacoTheme.Typo.rowTitle)
            .foregroundStyle(MonacoTheme.ink)
        if dynamicTypeSize.isAccessibilitySize {
            HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
                CabalMark(groupId: side.groupID, name: side.name, size: 32, pictureUrl: side.pictureUrl)
                VStack(alignment: .leading, spacing: 2) {
                    name.lineLimit(2)
                    score
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        } else {
            HStack(spacing: MonacoTheme.Space.sm) {
                CabalMark(groupId: side.groupID, name: side.name, size: 32, pictureUrl: side.pictureUrl)
                name
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .frame(maxWidth: .infinity, alignment: .leading)
                score.fixedSize()
            }
        }
    }

    private var byeRow: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            CabalMark(groupId: matchup.a.groupID, name: matchup.a.name, size: 32, pictureUrl: matchup.a.pictureUrl)
            VStack(alignment: .leading, spacing: 2) {
                Text(matchup.a.name)
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                Text(MatchupCopy.byeTitle)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.secondaryText)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            chevron
        }
    }
}

/// Placeholder in the scoreboard's shape: two mark-name-score lines and the bar.
struct MatchupScoreboardSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            ForEach(0..<2, id: \.self) { _ in
                HStack(spacing: MonacoTheme.Space.sm) {
                    SkeletonBlock(width: 32, height: 32, radius: MonacoTheme.Radius.tile * 32 / 44)
                    SkeletonBlock(width: 140, height: 14)
                    Spacer(minLength: MonacoTheme.Space.s)
                    SkeletonBlock(width: 48, height: 14)
                }
            }
            SkeletonBlock(height: 4, radius: 2)
                .padding(.top, MonacoTheme.Space.xs)
            SkeletonBlock(width: 72, height: 10)
        }
        .accessibilityHidden(true)
    }
}
