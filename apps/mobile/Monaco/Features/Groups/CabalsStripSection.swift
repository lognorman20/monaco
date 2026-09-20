import MonacoCore
import SwiftUI

/// Horizontal strip of the viewer's cabals: tinted tile, name, pot, P&L. Tap opens the cabal.
struct CabalsStripSection: View {
    @ObservedObject var auth: DynamicAuthService
    let rows: [HomeGroupBoardRowDTO]
    var onChanged: () async -> Void

    private static let cardSize = CGSize(width: 176, height: 148)

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Your cabals")

            if rows.isEmpty {
                EmptyState(
                    title: "No cabals yet",
                    message: "Search above or start one with the + button."
                )
                .accessibilityIdentifier("cabals-strip-empty")
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    LazyHStack(spacing: MonacoTheme.Space.s) {
                        ForEach(rows) { row in
                            NavigationLink {
                                GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name, onLeft: onChanged)
                            } label: {
                                CabalStripCard(row: row, size: Self.cardSize)
                            }
                            .buttonStyle(.plain)
                            .accessibilityIdentifier("cabals-strip-card-\(row.groupId)")
                        }
                        NavigationLink {
                            CreateGroupView(auth: auth)
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
        }
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
