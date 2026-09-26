import MonacoCore
import SwiftUI

/// Reordering and removing, as the system list does it: drag handles to move a stock, the
/// delete control or a swipe to take one off. A removal is saved at once; the order is saved
/// when the member taps Done, as one write.
struct WatchlistEditSheet: View {
    let model: WatchlistModel

    @Environment(\.dismiss) private var dismiss
    @State private var order: [MarketRowData] = []
    @State private var didLoad = false

    var body: some View {
        NavigationStack {
            List {
                ForEach(order) { row in
                    WatchlistEditRow(asset: row.asset)
                        .listRowInsets(EdgeInsets(top: 0, leading: MonacoTheme.Space.m, bottom: 0, trailing: MonacoTheme.Space.m))
                        .listRowBackground(MonacoTheme.canvas)
                        .listRowSeparatorTint(MonacoTheme.hairline)
                        .accessibilityIdentifier("watchlist-edit-row-\(row.asset.symbol)")
                }
                .onMove { from, to in
                    order.move(fromOffsets: from, toOffset: to)
                }
                .onDelete { offsets in
                    let symbols = offsets.map { order[$0].asset.symbol }
                    order.remove(atOffsets: offsets)
                    for symbol in symbols {
                        Task { await model.remove(symbol: symbol) }
                    }
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .environment(\.editMode, .constant(.active))
            .monacoCanvas()
            .navigationTitle(WatchlistCopy.editTitle)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(WatchlistCopy.doneAction) {
                        let symbols = order.map(\.asset.symbol)
                        dismiss()
                        Task { await model.reorder(to: symbols) }
                    }
                    .font(MonacoTheme.Typo.bodyStrong)
                    .accessibilityIdentifier("watchlist-edit-done")
                }
            }
            .overlay {
                if order.isEmpty, didLoad {
                    EmptyState(title: WatchlistCopy.editEmptyTitle, message: WatchlistCopy.editEmptyMessage)
                }
            }
        }
        .tint(MonacoTheme.ink)
        .onAppear {
            guard !didLoad else { return }
            order = model.rows
            didLoad = true
        }
        .accessibilityIdentifier("watchlist-edit-root")
    }
}

/// A stock in the edit list: its mark, ticker and name. The figures stay on the tab; here the
/// member is arranging, not reading prices.
private struct WatchlistEditRow: View {
    let asset: MarketAssetDTO

    var body: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            StockMark(symbol: asset.symbol, displayName: asset.name, assetKind: asset.resolvedKind, size: 36, logoURL: asset.logoURL)
                .frame(width: 36, height: 36)
            VStack(alignment: .leading, spacing: 2) {
                Text(AssetSymbolFormatter.display(asset.symbol, kind: asset.resolvedKind))
                    .font(MonacoTheme.Typo.ticker)
                    .foregroundStyle(MonacoTheme.ink)
                Text(AssetCatalogDisplayName.format(catalogName: asset.name, symbol: asset.symbol, kind: asset.resolvedKind))
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: 0)
        }
        .frame(minHeight: 56)
        .accessibilityElement(children: .combine)
    }
}
