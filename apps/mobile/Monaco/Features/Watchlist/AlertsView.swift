import MonacoCore
import SwiftUI

/// Every price alert, grouped by stock. Each group opens on its stock and today's price; under
/// it the lines still waiting, then the ones that fired, each with when. A swipe removes one.
///
/// Reached from Profile. Polls at the resting cadence, so an alert that fires while the page is
/// open moves to "Fired" without a pull.
struct AlertsView: View {
    @ObservedObject var auth: PrivyAuthService

    @State private var model: AlertsModel
    @State private var openSymbol: String?

    init(auth: PrivyAuthService, dataSource: PriceAlertDataSource? = nil) {
        self.auth = auth
        _model = State(initialValue: AlertsModel(dataSource: dataSource ?? LiveWatchlistDataSource(auth: auth)))
    }

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .monacoCanvas()
            .foregroundStyle(MonacoTheme.ink)
            .navigationTitle(AlertCopy.screenTitle)
            .navigationBarTitleDisplayMode(.inline)
            .task { await model.load() }
            .pollWhileVisible(every: LiveRefreshCadence.resting) { try await model.poll() }
            .monacoToast($model.toast)
            .navigationDestination(isPresented: Binding(
                get: { openSymbol != nil },
                set: { if !$0 { openSymbol = nil } }
            )) {
                if let openSymbol {
                    AssetDetailView(auth: auth, symbol: openSymbol)
                }
            }
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("alerts-root")
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .loading where model.groups.isEmpty:
            ScrollView { skeleton }
                .scrollDisabled(true)
                .accessibilityIdentifier("alerts-loading")
        case .failed where model.groups.isEmpty:
            centered {
                EmptyState(
                    title: AlertCopy.loadFailed,
                    actionTitle: "Retry",
                    action: { Task { await model.load() } }
                )
                .accessibilityIdentifier("alerts-failed")
            }
        default:
            if model.isEmpty {
                centered {
                    EmptyState(title: AlertCopy.emptyTitle, message: AlertCopy.emptyMessage)
                        .accessibilityIdentifier("alerts-empty")
                }
            } else {
                list
            }
        }
    }

    private var list: some View {
        List {
            if model.refreshFailed {
                plainRow {
                    Text(AssetsTabView.staleRefreshCaption)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.warning)
                        .accessibilityIdentifier("alerts-stale")
                }
            }
            ForEach(model.groups) { group in
                plainRow {
                    AlertGroupHeader(group: group) { open(group.symbol) }
                        .padding(.top, MonacoTheme.Space.m)
                }
                ForEach(Array(group.alerts.enumerated()), id: \.element.id) { index, alert in
                    PriceAlertLedgerRow(alert: alert, isFirst: index == 0)
                        .listRowInsets(EdgeInsets(top: 0, leading: MonacoTheme.Space.m, bottom: 0, trailing: MonacoTheme.Space.m))
                        .listRowSeparator(.hidden)
                        .listRowBackground(MonacoTheme.canvas)
                        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                            Button(role: .destructive) {
                                Task { await model.delete(alert) }
                            } label: {
                                Label("Remove", systemImage: "trash")
                            }
                        }
                        .accessibilityAction(named: Text("Remove")) {
                            Task { await model.delete(alert) }
                        }
                        .accessibilityIdentifier("alerts-row-\(alert.id)")
                }
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .refreshable { await model.load() }
        .accessibilityIdentifier("alerts-list")
    }

    private func plainRow<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        content()
            .frame(maxWidth: .infinity, alignment: .leading)
            .listRowInsets(EdgeInsets(top: MonacoTheme.Space.xs, leading: MonacoTheme.Space.m, bottom: MonacoTheme.Space.xs, trailing: MonacoTheme.Space.m))
            .listRowSeparator(.hidden)
            .listRowBackground(MonacoTheme.canvas)
    }

    private func open(_ symbol: String) {
        Haptics.selection()
        openSymbol = symbol
    }

    /// Two groups in the loaded shape: a stock line over two ruled alert lines.
    private var skeleton: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
            ForEach(0..<2, id: \.self) { _ in
                VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                    HStack(spacing: MonacoTheme.Space.sm) {
                        SkeletonBlock(width: 40, height: 40, radius: 20)
                        VStack(alignment: .leading, spacing: 6) {
                            SkeletonBlock(width: 56, height: 14, radius: 3)
                            SkeletonBlock(width: 96, height: 10, radius: 3)
                        }
                        Spacer()
                        SkeletonBlock(width: 72, height: 14, radius: 3)
                    }
                    VStack(spacing: 0) {
                        ForEach(0..<2, id: \.self) { _ in
                            HStack {
                                SkeletonBlock(width: 124, height: 14, radius: 3)
                                Spacer()
                                SkeletonBlock(width: 68, height: 10, radius: 3)
                            }
                            .frame(minHeight: 52)
                            .overlay(alignment: .bottom) { MonacoRule() }
                        }
                    }
                    .overlay(alignment: .top) { MonacoRule() }
                }
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.top, MonacoTheme.Space.m)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading your alerts")
    }

    private func centered<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        VStack {
            Spacer(minLength: 0)
            content()
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// A stock's line over its alerts: the mark, the ticker over the name, and the price now. Taps
/// through to the stock.
private struct AlertGroupHeader: View {
    let group: PriceAlertGroup
    let open: () -> Void

    private var kind: AssetKind { group.asset?.resolvedKind ?? .stock }
    private var name: String {
        AssetCatalogDisplayName.format(catalogName: group.asset?.name ?? "", symbol: group.symbol, kind: kind)
    }

    var body: some View {
        Button(action: open) {
            HStack(spacing: MonacoTheme.Space.sm) {
                StockMark(symbol: group.symbol, displayName: group.asset?.name, assetKind: kind, size: 40, logoURL: group.asset?.logoURL)
                    .frame(width: 40, height: 40)
                VStack(alignment: .leading, spacing: 2) {
                    Text(AssetSymbolFormatter.display(group.symbol, kind: kind))
                        .font(MonacoTheme.Typo.ticker)
                        .foregroundStyle(MonacoTheme.ink)
                    Text(name)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                        .lineLimit(1)
                }
                Spacer(minLength: MonacoTheme.Space.s)
                if let price = group.asset?.priceUsdcMicros {
                    MoneyText(micros: price, style: .row, voice: .market)
                } else {
                    Text("—").moneyFont(.row, voice: .market).foregroundStyle(MonacoTheme.muted)
                }
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(MonacoTheme.tertiaryText)
                    .accessibilityHidden(true)
            }
            .frame(minHeight: 48)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityHint("Opens \(name)")
        .accessibilityIdentifier("alerts-stock-\(group.symbol)")
    }
}
