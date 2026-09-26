import MonacoCore
import SwiftUI

/// The top of the Stocks tab's browse list: the member's watchlist, in their order, as the
/// same market rows as every section below it.
///
/// No section at all when the list is empty — the first-use hint under the search field says
/// what the star is for. While the first read is in flight the section holds room for as many
/// rows as the member had last time, and a failed first read offers a retry only to a member
/// who had a watchlist to lose. Loading and the edit sheet live in `watchlistLifecycle`, on
/// the tab itself, so they keep working while this section has nothing to draw.
struct WatchlistSection: View {
    let model: WatchlistModel
    /// Opens a stock. The tab owns navigation.
    let open: (String) -> Void

    var body: some View {
        content
    }

    @ViewBuilder
    private var content: some View {
        if !model.rows.isEmpty {
            section(showsEdit: true, staleCaption: model.refreshFailed) {
                MonacoGroupedList {
                    ForEach(model.rows) { row in
                        Button {
                            open(row.asset.symbol)
                        } label: {
                            StockListRow(row: row, afterHours: model.afterHours, isLast: row.id == model.rows.last?.id)
                        }
                        .buttonStyle(.monacoRow)
                        .contextMenu {
                            Button(role: .destructive) {
                                Task { await model.remove(symbol: row.asset.symbol) }
                            } label: {
                                Label(WatchlistCopy.removeAction, systemImage: "star.slash")
                            }
                        }
                        .accessibilityAction(named: Text(WatchlistCopy.removeAction)) {
                            Task { await model.remove(symbol: row.asset.symbol) }
                        }
                        .accessibilityIdentifier("assets-watchlist-\(row.asset.symbol)")
                    }
                }
            }
            .accessibilityIdentifier("assets-watchlist")
        } else if model.skeletonRowCount > 0 {
            section(showsEdit: false, staleCaption: false) {
                MonacoGroupedList {
                    ForEach(0..<model.skeletonRowCount, id: \.self) { index in
                        StockListRowSkeleton(isLast: index == model.skeletonRowCount - 1)
                    }
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("Loading your watchlist")
            }
            .accessibilityIdentifier("assets-watchlist-loading")
        } else if model.showsFailure {
            section(showsEdit: false, staleCaption: false) {
                EmptyState(
                    title: WatchlistCopy.loadFailed,
                    message: "The market below is still live.",
                    actionTitle: "Retry",
                    action: { Task { await model.load() } }
                )
            }
            .accessibilityIdentifier("assets-watchlist-failed")
        }
    }

    /// The same anatomy as the tab's other sections: an inset header over edge-to-edge rows.
    private func section<Rows: View>(showsEdit: Bool, staleCaption: Bool, @ViewBuilder rows: () -> Rows) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                if showsEdit {
                    MonacoSectionHeader(WatchlistCopy.sectionTitle, trailing: WatchlistCopy.editAction) {
                        Haptics.selection()
                        model.isEditing = true
                    }
                    .accessibilityIdentifier("assets-watchlist-edit")
                } else {
                    MonacoSectionHeader(WatchlistCopy.sectionTitle)
                }
                if staleCaption {
                    Text(AssetsTabView.staleRefreshCaption)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.warning)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .fixedSize(horizontal: false, vertical: true)
                        .accessibilityIdentifier("assets-watchlist-stale")
                }
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            rows()
        }
        .accessibilityElement(children: .contain)
    }
}

/// One line under the search field until the member first follows a stock. Nothing at all
/// otherwise, and nothing while they are searching.
struct WatchlistFirstUseHint: View {
    let model: WatchlistModel
    let isSearching: Bool

    var body: some View {
        if model.showsFirstUseHint, !isSearching {
            Label {
                Text(WatchlistCopy.firstUseHint)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            } icon: {
                Image(systemName: "star")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.brand)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, MonacoTheme.Space.m)
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("assets-watchlist-hint")
        }
    }
}

extension View {
    /// Keeps the Stocks tab's watchlist current and hosts what it presents: a read on appear,
    /// when the tab is picked again, when the app returns to the foreground, and whenever the
    /// stock screen says the member starred something; the edit sheet; and its toasts.
    func watchlistLifecycle(_ model: WatchlistModel) -> some View {
        modifier(WatchlistLifecycle(model: model))
    }
}

private struct WatchlistLifecycle: ViewModifier {
    @Bindable var model: WatchlistModel

    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.selectedMainTab) private var selectedMainTab
    @Environment(\.hostMainTab) private var hostMainTab

    func body(content: Content) -> some View {
        content
            .task { await model.refreshIfStale() }
            .onChange(of: scenePhase) { _, phase in
                guard phase == .active else { return }
                Task { await model.refreshIfStale() }
            }
            .onChange(of: selectedMainTab) { _, tab in
                guard let hostMainTab, tab == hostMainTab else { return }
                Task { await model.refreshIfStale() }
            }
            .onReceive(NotificationCenter.default.publisher(for: .monacoWatchlistDidChange)) { _ in
                Task { await model.load() }
            }
            .sheet(isPresented: $model.isEditing) {
                WatchlistEditSheet(model: model)
            }
            .monacoToast($model.toast)
    }
}
