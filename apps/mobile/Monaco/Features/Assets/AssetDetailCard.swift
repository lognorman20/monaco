import SwiftUI

/// The shell every card below the chart shares: a surface, a title, and room.
///
/// One container rather than five nearly-identical ones, so the cards cannot drift
/// apart in padding or radius as they are edited separately — which is exactly what
/// happened to the group screens before `MonacoGroupedList` existed.
///
/// The identifier lands on the card's *title*, never on the stack.
///
/// An `.accessibilityIdentifier` on a `VStack` is applied to each of its children, so
/// putting the card's name on the outer stack renames everything inside it: every
/// stats cell, every holding row and both legs of the Pyth card become
/// "asset-detail-<card>" and are unfindable by the names they were actually given.
/// The title is a leaf with no children to rename, and "the card is on screen" and
/// "its title is on screen" are the same fact.
struct AssetDetailCard<Content: View>: View {
    let title: String
    /// Optional right-hand text, e.g. a count or a basis caption.
    var trailing: String?
    /// The identifier a UI test asks for. It lands on the title and nothing else.
    let identifier: String
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            header
            content
        }
        .padding(MonacoTheme.Space.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            MonacoTheme.surface,
            in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
        }
    }

    private var header: some View {
        // Baseline-aligned so a long trailing caption wrapping to two lines does not
        // drag the title off the top of the card.
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

/// A hairline between rows inside a card. Inset from the card's own padding so it
/// reads as a divider between lines rather than as the card's edge.
struct AssetCardDivider: View {
    var body: some View {
        Rectangle()
            .fill(MonacoTheme.hairline)
            .frame(height: 1)
            .accessibilityHidden(true)
    }
}
