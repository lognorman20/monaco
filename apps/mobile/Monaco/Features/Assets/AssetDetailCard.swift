import SwiftUI

/// The shell every section below the chart shares: a rule across the page, a title, and
/// the section's own lines.
///
/// One container rather than five nearly-identical ones, so the sections cannot drift apart
/// in padding as they are edited separately — which is exactly what happened to the group
/// screens before `MonacoGroupedList` existed. It used to be a white card; it is a ruled
/// section on the paper now, like every other section in the app.
///
/// The rule runs edge to edge, the way the curve above it does and the way a ruled list's
/// outer rules do everywhere else in the app: it is where one section of the ledger ends
/// and the next begins. Everything under it — the title, the figures, the rows — keeps the
/// page's 16pt inset, and the rules *between* rows (`AssetCardDivider`) start where a row's
/// text starts.
///
/// The identifier lands on the section's *title*, never on the stack.
///
/// An `.accessibilityIdentifier` on a `VStack` is applied to each of its children, so
/// putting the section's name on the outer stack renames everything inside it: every
/// stats cell, every holding row and both legs of the Pyth card become
/// "asset-detail-<card>" and are unfindable by the names they were actually given.
/// The title is a leaf with no children to rename, and "the section is on screen" and
/// "its title is on screen" are the same fact.
struct AssetDetailCard<Content: View>: View {
    let title: String
    /// Optional right-hand text, e.g. a count or a basis caption.
    var trailing: String?
    /// The identifier a UI test asks for. It lands on the title and nothing else.
    let identifier: String
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            MonacoRule()
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                header
                content
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.horizontal, MonacoTheme.Space.m)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var header: some View {
        // Baseline-aligned so a long trailing caption wrapping to two lines does not
        // drag the title off the top of the section.
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
            Text(title)
                .font(MonacoTheme.Typo.section)
                .foregroundStyle(MonacoTheme.ink)
                .accessibilityAddTraits(.isHeader)
                .accessibilityIdentifier(identifier)
            Spacer(minLength: MonacoTheme.Space.s)
            if let trailing {
                Text(trailing)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .multilineTextAlignment(.trailing)
            }
        }
    }
}

/// The rule between two lines inside a section.
///
/// It starts where the line's text starts — past the mark or the glyph when the row leads
/// with one — and runs on to the screen's trailing edge, which is how `MonacoRow` draws the
/// separators in every other ruled list. The section's own full-width rule sits above its
/// title; these are the lighter-weight lines inside it.
struct AssetCardDivider: View {
    /// How far past the section's text edge the rule starts: 0 under a plain line, the
    /// mark plus its gap under a row that leads with one (`AssetCardDivider.inset(afterMark:)`).
    var leading: CGFloat = 0

    /// The inset for a row that leads with a mark or a glyph `width` points wide.
    static func inset(afterMark width: CGFloat) -> CGFloat {
        width + MonacoTheme.Space.sm
    }

    var body: some View {
        MonacoRule()
            .padding(.leading, leading)
            // The section insets its content by `Space.m`; the rule carries on to the edge
            // rather than stopping short of it, as a list's separators do.
            .padding(.trailing, -MonacoTheme.Space.m)
    }
}

/// A section's shape while the stock is still loading: the rule, a title, and two rows
/// of the two-column table most stocks open with. The same geometry as `AssetDetailCard`,
/// so nothing moves when the real section replaces it.
struct AssetDetailSectionSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            MonacoRule()
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                SkeletonBlock(width: 96, height: 18, radius: 4)
                    .padding(.vertical, 3)
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(0..<2, id: \.self) { index in
                        if index > 0 { AssetCardDivider() }
                        HStack(alignment: .top, spacing: MonacoTheme.Space.m) {
                            cell
                            cell
                        }
                        .padding(.vertical, MonacoTheme.Space.sm)
                    }
                }
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.horizontal, MonacoTheme.Space.m)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityHidden(true)
    }

    private var cell: some View {
        VStack(alignment: .leading, spacing: 8) {
            SkeletonBlock(width: 64, height: 10, radius: 3)
            SkeletonBlock(width: 84, height: 14, radius: 3)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
