import MonacoCore
import SwiftUI
import UIKit

/// Every dollar that moved: money added, cabals funded and cashed out of, and the member's
/// slice of every buy and sell. Grouped by day, newest first, filtered by chips, exportable
/// as a CSV.
struct HistoryView: View {
    @ObservedObject var auth: PrivyAuthService

    @State private var model: HistoryModel
    @State private var toast: MonacoToast?
    @State private var exported: ExportedHistoryFile?
    @State private var showDeposit = false

    @Environment(AppSessionStore.self) private var session: AppSessionStore?

    init(auth: PrivyAuthService, service: PortfolioService? = nil, model: HistoryModel? = nil) {
        self.auth = auth
        _model = State(initialValue: model ?? HistoryModel(service: service ?? LivePortfolioService(auth: auth)))
    }

    var body: some View {
        VStack(spacing: 0) {
            HistoryFilterChips(selection: model.filter) { filter in
                Task { await model.select(filter) }
            }
            .padding(.bottom, MonacoTheme.Space.s)

            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .monacoCanvas()
        .navigationTitle(PortfolioCopy.historyTitle)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                exportButton
            }
        }
        .navigationDestination(isPresented: $showDeposit) {
            DepositView(auth: auth, joinedCabals: session?.joinedCabals ?? [])
        }
        .task {
            if model.items.isEmpty, model.phase == .loading { await model.loadFirstPage() }
        }
        .pollWhileVisible(every: LiveRefreshCadence.resting, isActive: model.phase == .loaded) {
            try await model.poll()
        }
        .sheet(item: $exported) { file in
            ActivityShareSheet(items: [file.url])
                .presentationDetents([.medium, .large])
                .ignoresSafeArea()
        }
        .monacoToast($toast)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("history-root")
        .monacoFrameStats("History")
    }

    // MARK: Header action

    /// Nothing to export until rows have loaded; it looks unavailable until then.
    private var canExport: Bool {
        !model.isExporting && model.phase == .loaded && !model.items.isEmpty
    }

    private var exportButton: some View {
        Button {
            Task { await export() }
        } label: {
            if model.isExporting {
                ProgressView()
                    .tint(MonacoTheme.ink)
                    .accessibilityLabel("Exporting")
            } else {
                Text(PortfolioCopy.exportCSV)
                    .font(MonacoTheme.Typo.calloutStrong)
                    .foregroundStyle(canExport ? MonacoTheme.brand : MonacoTheme.disabledLabel)
            }
        }
        .disabled(!canExport)
        .accessibilityIdentifier("history-export-csv")
    }

    private func export() async {
        do {
            let url = try await model.exportFile()
            exported = ExportedHistoryFile(url: url)
        } catch {
            guard !PortfolioModel.isCancellation(error) else { return }
            Haptics.warning()
            toast = MonacoToast(message: PortfolioCopy.exportFailed)
        }
    }

    // MARK: States

    @ViewBuilder
    private var content: some View {
        switch model.phase {
        case .loading:
            HistorySkeleton()
        case .failed(let message):
            ScrollView {
                EmptyState(
                    title: PortfolioCopy.historyLoadFailed,
                    message: message,
                    actionTitle: PortfolioCopy.tryAgain,
                    action: { Task { await model.loadFirstPage() } }
                )
                .padding(.top, MonacoTheme.Space.xl)
            }
            .scrollBounceBehavior(.always)
            .accessibilityIdentifier("history-error")
        case .loaded:
            if model.items.isEmpty {
                emptyState
            } else {
                list
            }
        }
    }

    /// A real next step either way: add money when nothing has moved, or widen the filter.
    private var emptyState: some View {
        ScrollView {
            Group {
                if model.filter == .all {
                    EmptyState(
                        title: model.filter.emptyTitle,
                        message: PortfolioCopy.emptyHistoryMessage,
                        actionTitle: "Add money",
                        action: { showDeposit = true }
                    )
                } else {
                    EmptyState(
                        title: model.filter.emptyTitle,
                        actionTitle: "Show all",
                        action: { Task { await model.select(.all) } }
                    )
                }
            }
            .padding(.top, MonacoTheme.Space.xl)
        }
        .refreshable { await model.loadFirstPage() }
        .accessibilityIdentifier("history-empty")
    }

    private var list: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                ForEach(model.sections) { section in
                    VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                        MonacoSectionHeader(section.title)
                            .padding(.horizontal, MonacoTheme.Space.m)
                        MonacoGroupedList {
                            ForEach(section.items) { item in
                                row(item, isLast: item.id == section.items.last?.id)
                                    .task { await model.loadMoreIfNeeded(currentItem: item) }
                            }
                        }
                    }
                }
                footer
            }
            .padding(.top, MonacoTheme.Space.xs)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .refreshable {
            if await !model.loadFirstPage() {
                toast = MonacoToast(message: "Couldn't refresh just now")
            }
        }
        .accessibilityIdentifier("history-list")
    }

    @ViewBuilder
    private func row(_ item: HistoryItemDTO, isLast: Bool) -> some View {
        if let receipt = HistoryRowCopy.receipt(for: item) {
            NavigationLink {
                TransactionDetailView(
                    auth: auth,
                    activityItem: HistoryReceiptItem.activityItem(for: item, receipt: receipt),
                    onRetry: nil,
                    isRetrying: false
                )
            } label: {
                HistoryRow(item: item, isLast: isLast, opensReceipt: true)
            }
            .buttonStyle(.monacoRow)
            .accessibilityIdentifier("history-row-\(item.id)")
        } else {
            HistoryRow(item: item, isLast: isLast, opensReceipt: false)
                .accessibilityIdentifier("history-row-\(item.id)")
        }
    }

    /// The end of the list: loading the next page, a failed page to retry, or nothing.
    @ViewBuilder
    private var footer: some View {
        if model.isLoadingMore {
            ProgressView()
                .tint(MonacoTheme.ink)
                .frame(maxWidth: .infinity, minHeight: 44)
                .accessibilityLabel("Loading more")
                .accessibilityIdentifier("history-loading-more")
        } else if model.loadMoreFailed {
            HStack(spacing: MonacoTheme.Space.s) {
                Text(PortfolioCopy.loadMoreFailed)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                Button(PortfolioCopy.tryAgain) {
                    Task { await model.loadMore() }
                }
                .font(MonacoTheme.Typo.calloutStrong)
                .foregroundStyle(MonacoTheme.brand)
                .frame(minHeight: 44)
                .accessibilityIdentifier("history-load-more-retry")
            }
            .frame(maxWidth: .infinity)
        }
    }
}

/// The filter chips: capsules on the sunken paper, the selected one in ink.
private struct HistoryFilterChips: View {
    let selection: HistoryFilter
    let onSelect: (HistoryFilter) -> Void

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: MonacoTheme.Space.s) {
                ForEach(HistoryFilter.allCases) { filter in
                    let isSelected = filter == selection
                    Button {
                        guard !isSelected else { return }
                        Haptics.selection()
                        onSelect(filter)
                    } label: {
                        Text(filter.title)
                            .font(MonacoTheme.Typo.calloutStrong)
                            .foregroundStyle(isSelected ? MonacoTheme.onBrand : MonacoTheme.ink)
                            .lineLimit(1)
                            .padding(.horizontal, MonacoTheme.Space.m)
                            .frame(minHeight: 36)
                            .background(Capsule().fill(isSelected ? MonacoTheme.brandFill : MonacoTheme.surfaceSunken))
                            .padding(.vertical, 4)
                            .contentShape(Capsule())
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(isSelected ? [.isSelected] : [])
                    .accessibilityIdentifier("history-filter-\(filter.rawValue)")
                }
            }
            .padding(.horizontal, MonacoTheme.Space.m)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Show")
    }
}

/// Mark, what happened, where, and how much. Status only when the money has not moved yet.
struct HistoryRow: View {
    let item: HistoryItemDTO
    let isLast: Bool
    let opensReceipt: Bool

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var titleWidthFloor = MonacoRowLayout.baseMinimumTitleWidth

    private var layout: MonacoRowLayout {
        MonacoRowLayout(dynamicTypeSize: dynamicTypeSize, scaledTitleWidthFloor: titleWidthFloor)
    }

    var body: some View {
        Group {
            if layout.isStacked {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    HStack(spacing: MonacoTheme.Space.sm) {
                        mark
                        labels
                        chevron
                    }
                    figures(alignment: .leading)
                }
            } else {
                HStack(spacing: MonacoTheme.Space.sm) {
                    mark
                    labels
                        .frame(minWidth: layout.minimumTitleWidth, alignment: .leading)
                    figures(alignment: .trailing)
                        .layoutPriority(1)
                    chevron
                }
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, 8)
        .frame(minHeight: 60)
        .contentShape(Rectangle())
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule().padding(.leading, layout.separatorLeadingInset)
            }
        }
        .accessibilityElement(children: .combine)
    }

    /// A coin for money, the stock's own mark for a trade.
    @ViewBuilder
    private var mark: some View {
        Group {
            if item.resolvedKind.isTrade, let symbol = item.symbol {
                StockMark(symbol: symbol, displayName: HistoryRowCopy.stockName(item), assetKind: item.resolvedAssetKind)
            } else {
                StockMark(symbol: "USDC")
            }
        }
        .frame(width: 44, height: 44)
    }

    private var labels: some View {
        VStack(alignment: .leading, spacing: 2) {
            // Two lines, not a truncation: the cabal's name is often the point of the title.
            Text(HistoryRowCopy.title(for: item))
                .font(MonacoTheme.Typo.rowTitle)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(2)
                .fixedSize(horizontal: false, vertical: true)
            // One run of text, so at large sizes it wraps as a sentence instead of as columns.
            captionLine
                .lineLimit(layout.isStacked ? nil : 1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// "Semis or bust · Pending", the status in amber (red when it failed).
    private var captionLine: Text {
        let caption = Text(HistoryRowCopy.caption(for: item))
            .font(MonacoTheme.Typo.caption)
            .foregroundStyle(MonacoTheme.muted)
        guard let status = HistoryRowCopy.statusLabel(for: item) else { return caption }
        return caption
            + Text(" · ").font(MonacoTheme.Typo.caption).foregroundStyle(MonacoTheme.muted)
            + Text(status)
                .font(MonacoTheme.Typo.captionStrong)
                .foregroundStyle(item.resolvedStatus == .failed ? MonacoTheme.loss : MonacoTheme.warning)
    }

    private func figures(alignment: HorizontalAlignment) -> some View {
        VStack(alignment: alignment, spacing: 2) {
            Text(HistoryRowCopy.amountText(for: item) ?? "—")
                .moneyFont(.row)
                .foregroundStyle(amountColor)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
            if let quantity = HistoryRowCopy.quantityText(for: item) {
                Text(quantity)
                    .font(MonacoTheme.Typo.dataCaption)
                    .foregroundStyle(MonacoTheme.muted)
                    .lineLimit(1)
            } else {
                Text(HistoryDayGrouping.timeLabel(item.at))
                    .font(MonacoTheme.Typo.stamp)
                    .foregroundStyle(MonacoTheme.tertiaryText)
                    .lineLimit(1)
            }
        }
    }

    private var amountColor: Color {
        guard HistoryRowCopy.amountText(for: item) != nil else { return MonacoTheme.muted }
        switch HistoryRowCopy.amountTone(for: item) {
        case .moneyIn: return MonacoTheme.profit
        case .plain: return MonacoTheme.ink
        case .failed: return MonacoTheme.muted
        }
    }

    /// A row with no receipt keeps the chevron's width, so every amount sits in one column.
    private var chevron: some View {
        Image(systemName: "chevron.right")
            .font(.footnote.weight(.semibold))
            .foregroundStyle(MonacoTheme.tertiaryText)
            .opacity(opensReceipt ? 1 : 0)
            .accessibilityHidden(true)
    }
}

/// The history's loading shape: a day heading over ledger rows.
private struct HistorySkeleton: View {
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                SkeletonBlock(width: 72, height: 20)
                    .padding(.horizontal, MonacoTheme.Space.m)
                LedgerRowSkeleton(rows: 6)
            }
            .padding(.top, MonacoTheme.Space.xs)
        }
        .scrollDisabled(true)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading your history")
        .accessibilityIdentifier("history-loading")
    }
}

/// The receipt a history row opens is the one the cabal's activity list already has, so the
/// row is handed over in that list's shape.
enum HistoryReceiptItem {
    static func activityItem(for item: HistoryItemDTO, receipt: HistoryReceipt) -> GroupActivityItemDTO {
        let status: String = switch item.resolvedStatus {
        case .done: "confirmed"
        case .pending: "pending"
        case .failed: "failed"
        }
        let micros = PortfolioMath.decimal(item.amountUsd) * 1_000_000
        let amountMicros = (micros as NSDecimalNumber).int64Value
        switch receipt {
        case .transaction(let id):
            let isSell = item.resolvedKind == .sell || item.resolvedKind == .botSell
            return GroupActivityItemDTO(
                id: id,
                kind: isSell ? "sell" : "buy",
                status: status,
                symbol: item.symbol,
                amountMicros: amountMicros,
                createdAt: ISO8601DateFormatter().string(from: item.at),
                txSignature: nil,
                tokenAmount: nil,
                proceedsUsdcMicros: isSell && item.amountUsd != nil ? String(amountMicros) : nil,
                initiatedBy: item.resolvedKind == .botBuy || item.resolvedKind == .botSell ? "agent" : nil,
                agentDisplayName: nil,
                assetKind: item.assetKind
            )
        case .deposit(let id):
            return GroupActivityItemDTO(
                id: id,
                kind: "deposit",
                status: status,
                symbol: "USDC",
                amountMicros: amountMicros,
                createdAt: ISO8601DateFormatter().string(from: item.at),
                txSignature: nil,
                tokenAmount: nil,
                proceedsUsdcMicros: nil,
                initiatedBy: nil,
                agentDisplayName: nil
            )
        }
    }
}

/// A written CSV, identified so the share sheet can be presented for it.
struct ExportedHistoryFile: Identifiable {
    let url: URL
    let id = UUID()
}

/// The system share sheet for a file.
struct ActivityShareSheet: UIViewControllerRepresentable {
    let items: [Any]

    func makeUIViewController(context: Context) -> UIActivityViewController {
        UIActivityViewController(activityItems: items, applicationActivities: nil)
    }

    func updateUIViewController(_ controller: UIActivityViewController, context: Context) {}
}
