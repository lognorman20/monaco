import MonacoCore
import SwiftUI

struct HomePositionsSection: View {
    @ObservedObject var auth: PrivyAuthService
    let rows: [HomeMyGroupRowDTO]
    var onLeft: () async -> Void = {}
    var onBrowseCabals: () -> Void = {}

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text("Your cabals")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)

            if rows.isEmpty {
                HomeCabalsEmptyState(onBrowseCabals: onBrowseCabals)
            } else {
                ForEach(rows) { row in
                    NavigationLink {
                        GroupDetailView(
                            auth: auth,
                            groupId: row.groupId,
                            groupName: row.name,
                            onLeft: onLeft
                        )
                    } label: {
                        MonacoRowCard(
                            systemImage: "person.3.fill",
                            title: row.name,
                            subtitle: "Your slice \(UsdAmountFormatter.format(decimalString: row.equityUsd)) · \(SlicePercentFormatter.format(row.slicePercent))",
                            trailing: row.dollarPnl,
                            trailingCaption: PercentReturnFormatter.format(row.percentReturn),
                            trailingColor: MonacoTheme.signed(row.dollarPnl)
                        )
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("home-my-group-\(row.groupId)")
                }
            }
        }
    }
}

/// Interim empty state. Phase B swaps this for the shared `EmptyState` primitive once
/// WP1 lands it — same copy, same "Browse cabals" action.
private struct HomeCabalsEmptyState: View {
    let onBrowseCabals: () -> Void

    var body: some View {
        VStack(spacing: MonacoTheme.Space.s) {
            Text("No cabals yet")
                .font(.body.weight(.semibold))
                .foregroundStyle(MonacoTheme.ink)
            Text("Start one with friends or join an open one.")
                .font(MonacoTheme.TypeRole.body)
                .foregroundStyle(MonacoTheme.muted)
                .multilineTextAlignment(.center)
            Button("Browse cabals", action: onBrowseCabals)
                .buttonStyle(.monacoSecondary)
                .accessibilityIdentifier("home-cabals-empty-browse")
        }
        .frame(maxWidth: .infinity)
        .padding(MonacoTheme.Space.l)
        .monacoSurfaceCard()
        .accessibilityIdentifier("home-cabals-empty")
    }
}
