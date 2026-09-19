import MonacoCore
import SwiftUI

/// Horizontal strip of the viewer's cabals: name, pot, P&L. Tap opens the cabal.
struct CabalsStripSection: View {
    @ObservedObject var auth: PrivyAuthService
    let rows: [HomeGroupBoardRowDTO]
    var onChanged: () async -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text("Your cabals")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)

            if rows.isEmpty {
                MonacoEmptyStateCard(
                    message: "You're not in a cabal yet. Search above or create one from the + menu.",
                    systemImage: "person.3"
                )
                .accessibilityIdentifier("cabals-strip-empty")
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    LazyHStack(spacing: MonacoTheme.Space.s) {
                        ForEach(rows) { row in
                            NavigationLink {
                                GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name, onLeft: onChanged)
                            } label: {
                                CabalStripCard(row: row)
                            }
                            .buttonStyle(.plain)
                            .accessibilityIdentifier("cabals-strip-card-\(row.groupId)")
                        }
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

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(row.name)
                .font(MonacoTheme.TypeRole.body.weight(.semibold))
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(2)
                .multilineTextAlignment(.leading)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
            Text(UsdAmountFormatter.format(decimalString: row.potValueUsd))
                .font(.title3.monospacedDigit().weight(.semibold))
                .foregroundStyle(MonacoTheme.ink)
                .minimumScaleFactor(0.7)
                .lineLimit(1)
            CabalPnLLabel(dollarPnl: row.dollarPnl, percentReturn: row.percentReturn)
        }
        .padding(MonacoTheme.Space.m)
        .frame(width: 168, height: 132, alignment: .topLeading)
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

/// "+$48.20 · +12.4%" colored by gain or loss. Percent is omitted until the
/// cabal has money in.
struct CabalPnLLabel: View {
    let dollarPnl: String
    let percentReturn: String?

    var body: some View {
        let loss = SignedUsdFormatter.isLoss(dollarPnl)
        let text = percentReturn == nil
            ? SignedUsdFormatter.format(dollarPnl)
            : "\(SignedUsdFormatter.format(dollarPnl)) · \(PercentReturnFormatter.format(percentReturn))"
        Text(text)
            .font(MonacoTheme.TypeRole.caption.monospacedDigit())
            .foregroundStyle(loss ? MonacoTheme.loss : MonacoTheme.profit)
            .lineLimit(1)
            .minimumScaleFactor(0.8)
    }
}
