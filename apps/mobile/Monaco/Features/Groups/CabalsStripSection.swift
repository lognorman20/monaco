import MonacoCore
import SwiftUI

/// Whether the viewer's cabals are known yet. An empty list only means "no
/// cabals" once the server has answered; before that it means "not loaded".
enum CabalsStripState {
    /// The list is on its way.
    case loading
    /// The server answered; an empty list is genuinely empty.
    case loaded
    /// We have no list and nothing is in flight — the load did not land.
    case unavailable
}

/// Horizontal strip of the viewer's cabals: tinted tile, name, pot, P&L. Tap opens the cabal.
struct CabalsStripSection: View {
    let rows: [HomeGroupBoardRowDTO]
    var state: CabalsStripState = .loaded
    var onSelect: (CabalsRoute) -> Void
    var onRetry: () -> Void = {}

    private static let cardSize = CGSize(width: 176, height: 148)

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Your cabals")

            if !rows.isEmpty {
                strip
            } else {
                switch state {
                case .loading:
                    placeholderStrip
                case .unavailable:
                    EmptyState(
                        title: "Couldn't load your cabals",
                        message: "Check your connection and try again.",
                        actionTitle: "Try again",
                        action: onRetry
                    )
                    .accessibilityIdentifier("cabals-strip-error")
                case .loaded:
                    EmptyState(
                        title: "No cabals yet",
                        message: "Search above or start one with the + button."
                    )
                    .accessibilityIdentifier("cabals-strip-empty")
                }
            }
        }
    }

    private var strip: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            LazyHStack(spacing: MonacoTheme.Space.s) {
                ForEach(rows) { row in
                    Button {
                        onSelect(.cabal(id: row.groupId, name: row.name))
                    } label: {
                        CabalStripCard(row: row, size: Self.cardSize)
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("cabals-strip-card-\(row.groupId)")
                }
                Button {
                    onSelect(.create)
                } label: {
                    NewCabalStripCard(size: Self.cardSize)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("cabals-strip-new")
            }
            .padding(.vertical, 2)
        }
        .accessibilityIdentifier("cabals-strip")
    }

    /// Two cards in the real shape while the list loads, so the section does not
    /// jump from a message to content.
    private var placeholderStrip: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            ForEach(0..<2, id: \.self) { _ in
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    SkeletonBlock(width: 36, height: 36, radius: MonacoTheme.Radius.card)
                    SkeletonBlock(width: 104, height: 14)
                    Spacer(minLength: 0)
                    SkeletonBlock(width: 84, height: 20)
                    SkeletonBlock(width: 64, height: 12)
                }
                .padding(MonacoTheme.Space.m)
                .frame(width: Self.cardSize.width, height: Self.cardSize.height, alignment: .topLeading)
                .background(
                    MonacoTheme.surface,
                    in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                )
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement()
        .accessibilityLabel("Loading your cabals")
        .accessibilityIdentifier("cabals-strip-loading")
    }
}

private struct CabalStripCard: View {
    let row: HomeGroupBoardRowDTO
    let size: CGSize

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
            CabalMark(groupId: row.groupId, name: row.name, size: 36)
            Text(row.name)
                .font(MonacoTheme.Typo.rowTitle)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(2)
                .multilineTextAlignment(.leading)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
            MoneyText(decimalString: row.potValueUsd, style: .large)
            PnLBadge(dollarPnl: row.dollarPnl, percentReturn: row.percentReturn, style: .caption)
        }
        .padding(MonacoTheme.Space.m)
        .frame(width: size.width, alignment: .topLeading)
        .frame(minHeight: size.height, alignment: .topLeading)
        .background(
            MonacoTheme.surface,
            in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
        }
        .accessibilityElement(children: .combine)
    }
}

private struct NewCabalStripCard: View {
    let size: CGSize

    var body: some View {
        VStack(spacing: MonacoTheme.Space.s) {
            Image(systemName: "plus")
                .font(.title2.weight(.semibold))
                .foregroundStyle(MonacoTheme.muted)
            Text("New cabal")
                .font(MonacoTheme.Typo.callout.weight(.semibold))
                .foregroundStyle(MonacoTheme.muted)
        }
        .frame(width: size.width, height: size.height)
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                .strokeBorder(MonacoTheme.hairline, style: StrokeStyle(lineWidth: 1, dash: [6, 4]))
        }
        .accessibilityLabel("New cabal")
    }
}
