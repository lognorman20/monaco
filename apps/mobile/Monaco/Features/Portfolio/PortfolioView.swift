import MonacoCore
import SwiftUI

/// Everything the member owns across their cabals, by stock: Home's "Your money in cabals"
/// taken apart. The total on top, the mix under it, then each stock with the cabals it sits
/// in, then the cash. A brokerage's investing page, set on the ledger.
struct PortfolioView: View {
    @ObservedObject var auth: PrivyAuthService

    @State private var model: PortfolioModel
    @State private var toast: MonacoToast?
    @State private var showDeposit = false
    private let service: PortfolioService

    @Environment(AppSessionStore.self) private var session: AppSessionStore?

    init(auth: PrivyAuthService, service: PortfolioService? = nil, model: PortfolioModel? = nil) {
        self.auth = auth
        let service = service ?? LivePortfolioService(auth: auth)
        self.service = service
        _model = State(initialValue: model ?? PortfolioModel(service: service))
    }

    var body: some View {
        Group {
            switch model.state {
            case .loading:
                PortfolioSkeleton()
            case .failed(let message):
                failure(message)
            case .loaded(let portfolio):
                if portfolio.isEmpty {
                    empty(portfolio)
                } else {
                    content(portfolio)
                }
            }
        }
        .monacoCanvas()
        .navigationTitle(PortfolioCopy.title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink {
                    HistoryView(auth: auth, service: service)
                } label: {
                    Text(PortfolioCopy.history)
                        .font(MonacoTheme.Typo.calloutStrong)
                        .foregroundStyle(MonacoTheme.brand)
                }
                .accessibilityIdentifier("portfolio-history-link")
            }
        }
        .navigationDestination(isPresented: $showDeposit) {
            DepositView(auth: auth, joinedCabals: session?.joinedCabals ?? [])
        }
        .task {
            if model.portfolio == nil { await model.load() }
        }
        .refreshable {
            if await !model.refresh() {
                toast = MonacoToast(message: "Couldn't refresh just now")
            }
        }
        .pollWhileVisible(every: LiveRefreshCadence.resting) {
            try await model.poll()
        }
        .monacoToast($toast)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("portfolio-root")
        .monacoFrameStats("Portfolio")
    }

    // MARK: States

    private func content(_ portfolio: PortfolioDTO) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                header(portfolio)
                holdingsSection(portfolio)
                cashSection(portfolio)
            }
            .padding(.top, MonacoTheme.Space.s)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
    }

    /// Nothing in a cabal yet. The account balance still shows when there is one: that money
    /// is the next step.
    private func empty(_ portfolio: PortfolioDTO) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                EmptyState(
                    title: PortfolioCopy.emptyTitle,
                    message: PortfolioCopy.emptyMessage,
                    actionTitle: "Add money",
                    action: { showDeposit = true }
                )
                .accessibilityIdentifier("portfolio-empty")

                if PortfolioMath.decimal(portfolio.accountBalanceUsd) > 0 {
                    MonacoGroupedList {
                        accountBalanceRow(portfolio, isLast: true)
                    }
                }
            }
            .padding(.top, MonacoTheme.Space.xl)
        }
    }

    private func failure(_ message: String) -> some View {
        ScrollView {
            VStack(spacing: MonacoTheme.Space.s) {
                EmptyState(
                    title: PortfolioCopy.loadFailed,
                    message: message,
                    actionTitle: PortfolioCopy.tryAgain,
                    action: { Task { await model.load() } }
                )
                .disabled(model.isRetrying)
                if model.isRetrying {
                    ProgressView()
                        .tint(MonacoTheme.ink)
                        .accessibilityLabel("Loading")
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.top, MonacoTheme.Space.xl)
        }
        .scrollBounceBehavior(.always)
        .accessibilityIdentifier("portfolio-error")
    }

    // MARK: Sections

    /// The figure is the title, as on Home; the mix bar is the one picture on the screen.
    private func header(_ portfolio: PortfolioDTO) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                Text(PortfolioCopy.totalCaption)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                Text(UsdAmountFormatter.format(decimalString: portfolio.totalUsd))
                    .font(MonacoTheme.Typo.display)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.5)
                    .contentTransition(.numericText())
                HStack(spacing: MonacoTheme.Space.s) {
                    PnLBadge(dollarPnl: portfolio.dollarPnl, percentReturn: portfolio.percentReturn)
                    Text(PortfolioCopy.allTime)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                }
            }
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("portfolio-total")

            let segments = PortfolioMath.allocation(for: portfolio)
            if !segments.isEmpty {
                PortfolioMixBar(segments: segments)
                    .padding(.top, MonacoTheme.Space.m)
            }
            if let note = PortfolioCopy.unvalued(portfolio.unvaluedCabals) {
                Text(note)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.warning)
                    .accessibilityIdentifier("portfolio-unvalued")
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
    }

    @ViewBuilder
    private func holdingsSection(_ portfolio: PortfolioDTO) -> some View {
        if !portfolio.holdings.isEmpty {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                MonacoSectionHeader(PortfolioCopy.holdings)
                    .padding(.horizontal, MonacoTheme.Space.m)
                MonacoGroupedList {
                    ForEach(portfolio.holdings) { holding in
                        PortfolioHoldingRow(
                            auth: auth,
                            holding: holding,
                            isExpanded: model.expanded.contains(holding.symbol),
                            isLast: holding.id == portfolio.holdings.last?.id,
                            onToggle: { model.toggle(holding.symbol) }
                        )
                    }
                }
            }
            .accessibilityIdentifier("portfolio-holdings")
        }
    }

    private func cashSection(_ portfolio: PortfolioDTO) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(PortfolioCopy.cash)
                .padding(.horizontal, MonacoTheme.Space.m)
            MonacoGroupedList {
                MonacoRow(
                    title: PortfolioCopy.cashInCabals,
                    subtitle: PortfolioCopy.cashInCabalsCaption,
                    leading: { StockMark(symbol: "USDC") },
                    trailing: { MoneyText(decimalString: portfolio.cashUsd, style: .row) }
                )
                .accessibilityIdentifier("portfolio-cash-in-cabals")
                accountBalanceRow(portfolio, isLast: true)
            }
        }
    }

    /// Outside every cabal and not in the total above, which is why it has its own line.
    private func accountBalanceRow(_ portfolio: PortfolioDTO, isLast: Bool) -> some View {
        MonacoRow(
            title: PortfolioCopy.accountBalance,
            subtitle: portfolio.accountBalanceUsd == nil
                ? PortfolioCopy.accountBalanceUnavailable
                : PortfolioCopy.accountBalanceCaption,
            isLast: isLast,
            leading: { StockMark(symbol: "USDC") },
            trailing: {
                if let balance = portfolio.accountBalanceUsd {
                    MoneyText(decimalString: balance, style: .row)
                } else {
                    Text("—")
                        .moneyFont(.row)
                        .foregroundStyle(MonacoTheme.muted)
                        .accessibilityLabel("Unavailable")
                }
            }
        )
        .accessibilityIdentifier("portfolio-account-balance")
    }
}

/// One stock across the member's cabals. Tapping it opens the cabals it sits in, each with the
/// member's slice; tapping a cabal line opens that cabal.
private struct PortfolioHoldingRow: View {
    @ObservedObject var auth: PrivyAuthService
    let holding: PortfolioHoldingDTO
    let isExpanded: Bool
    let isLast: Bool
    let onToggle: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ScaledMetric(relativeTo: .body) private var titleWidthFloor = MonacoRowLayout.baseMinimumTitleWidth

    private var layout: MonacoRowLayout {
        MonacoRowLayout(dynamicTypeSize: dynamicTypeSize, scaledTitleWidthFloor: titleWidthFloor)
    }

    private var cabalsLine: String {
        PortfolioCopy.cabalsSummary(holding.cabals.map(\.name))
    }

    var body: some View {
        VStack(spacing: 0) {
            Button {
                Haptics.selection()
                if reduceMotion {
                    onToggle()
                } else {
                    withAnimation(.easeOut(duration: 0.2)) { onToggle() }
                }
            } label: {
                summary
            }
            .buttonStyle(.monacoRow)
            .accessibilityElement(children: .combine)
            .accessibilityValue(isExpanded ? "Showing cabals" : "")
            .accessibilityHint(isExpanded ? "Hides the cabals" : "Shows the cabals it is in")
            .accessibilityIdentifier("portfolio-holding-\(holding.symbol)")

            if isExpanded {
                ForEach(holding.cabals) { line in
                    NavigationLink {
                        GroupDetailView(auth: auth, groupId: line.groupId, groupName: line.name)
                    } label: {
                        PortfolioCabalLineRow(line: line, kind: holding.kind, layout: layout)
                    }
                    .buttonStyle(.monacoRow)
                    .accessibilityIdentifier("portfolio-holding-\(holding.symbol)-cabal-\(line.groupId)")
                }
                .transition(.opacity)
            }
        }
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule().padding(.leading, layout.separatorLeadingInset)
            }
        }
    }

    private var summary: some View {
        Group {
            if layout.isStacked {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    HStack(spacing: MonacoTheme.Space.sm) {
                        mark
                        labels
                        disclosure
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
                    disclosure
                }
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, 8)
        .frame(minHeight: 60)
        .contentShape(Rectangle())
    }

    private var mark: some View {
        StockMark(
            symbol: holding.symbol,
            displayName: holding.displayName,
            assetKind: holding.kind,
            logoURL: holding.logoURL
        )
        .frame(width: 44, height: 44)
    }

    /// Name in the brand's voice, ticker in the market's.
    private var labels: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(holding.displayName)
                .font(MonacoTheme.Typo.rowTitle)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(layout.titleLineLimit)
            // One run of text: the ticker in the market's voice, where it sits in the brand's.
            (Text(holding.displayTicker).font(MonacoTheme.Typo.dataCaption)
                + Text(cabalsLine.isEmpty ? "" : " · \(cabalsLine)").font(MonacoTheme.Typo.caption))
                .foregroundStyle(MonacoTheme.muted)
                .lineLimit(layout.isStacked ? nil : 1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func figures(alignment: HorizontalAlignment) -> some View {
        VStack(alignment: alignment, spacing: 2) {
            MoneyText(decimalString: holding.valueUsd, style: .row)
            PnLText(dollarPnl: holding.dollarPnl, style: .caption)
                .accessibilityIdentifier("portfolio-holding-pnl-\(holding.symbol)")
        }
    }

    private var disclosure: some View {
        Image(systemName: "chevron.right")
            .font(.footnote.weight(.semibold))
            .foregroundStyle(MonacoTheme.tertiaryText)
            .rotationEffect(.degrees(isExpanded ? 90 : 0))
            .accessibilityHidden(true)
    }
}

/// The member's slice of one holding in one cabal, indented under the stock.
private struct PortfolioCabalLineRow: View {
    let line: PortfolioCabalLineDTO
    let kind: AssetKind
    let layout: MonacoRowLayout

    private var quantity: String {
        PortfolioMath.quantityLabel(line.quantity, kind: kind)
    }

    var body: some View {
        Group {
            if layout.isStacked {
                // At the accessibility sizes the figures drop under the name, as in `MonacoRow`.
                VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
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
                    figures(alignment: .trailing)
                    chevron
                }
            }
        }
        // Under the stock's labels, so the cabals read as the stock's parts.
        .padding(.leading, layout.isStacked ? MonacoTheme.Space.l : layout.separatorLeadingInset)
        .padding(.trailing, MonacoTheme.Space.m)
        .padding(.vertical, MonacoTheme.Space.s)
        .frame(minHeight: 52)
        .contentShape(Rectangle())
        .overlay(alignment: .top) {
            MonacoRule().padding(.leading, layout.isStacked ? MonacoTheme.Space.l : layout.separatorLeadingInset)
        }
        .accessibilityElement(children: .combine)
    }

    private var mark: some View {
        CabalMark(groupId: line.groupId, name: line.name, size: 32, pictureUrl: line.pictureUrl)
    }

    private var labels: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(line.name)
                .font(MonacoTheme.Typo.calloutStrong)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(layout.titleLineLimit)
            Text(quantity)
                .font(MonacoTheme.Typo.dataCaption)
                .foregroundStyle(MonacoTheme.muted)
                .lineLimit(1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func figures(alignment: HorizontalAlignment) -> some View {
        VStack(alignment: alignment, spacing: 2) {
            MoneyText(decimalString: line.valueUsd, style: .caption)
            PnLText(dollarPnl: line.dollarPnl, style: .caption)
        }
    }

    private var chevron: some View {
        Image(systemName: "chevron.right")
            .font(.caption2.weight(.semibold))
            .foregroundStyle(MonacoTheme.tertiaryText)
            .accessibilityHidden(true)
    }
}

/// Each holding's share of the total, then cash — the cabal screen's mix bar, drawn in ink
/// rather than one cabal's tint because the money here spans several.
struct PortfolioMixBar: View {
    let segments: [PortfolioAllocationSegment]

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            GeometryReader { geometry in
                HStack(spacing: 2) {
                    ForEach(segments) { segment in
                        Rectangle()
                            .fill(color(for: segment))
                            .frame(width: max(2, (geometry.size.width - CGFloat(segments.count - 1) * 2) * segment.fraction))
                    }
                }
            }
            .frame(height: 8)
            .clipShape(Capsule())
            legend
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Portfolio mix")
        .accessibilityValue(segments.map { "\($0.label) \(PortfolioMath.percentLabel($0.fraction))" }.joined(separator: ", "))
        .accessibilityIdentifier("portfolio-mix")
    }

    /// The first four slices named, in the market's voice; the rest are on the rows below. In a
    /// row while they fit, one under the other when the text is too large for that.
    private var legend: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: MonacoTheme.Space.sm) {
                legendItems
                Spacer(minLength: 0)
            }
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                legendItems
            }
        }
    }

    private var legendItems: some View {
        ForEach(segments.prefix(4)) { segment in
            HStack(spacing: 5) {
                Circle()
                    .fill(color(for: segment))
                    .overlay { Circle().strokeBorder(MonacoTheme.hairline, lineWidth: segment.isCash ? 1 : 0) }
                    .frame(width: 7, height: 7)
                Text("\(segment.label) \(PortfolioMath.percentLabel(segment.fraction))")
                    .font(MonacoTheme.Typo.dataCaption)
                    .foregroundStyle(MonacoTheme.muted)
                    .lineLimit(1)
                    .fixedSize()
            }
        }
    }

    /// Ink stepping down one holding at a time; cash is the paper.
    private func color(for segment: PortfolioAllocationSegment) -> Color {
        if segment.isCash { return MonacoTheme.surfaceSunken }
        let index = segments.firstIndex(of: segment) ?? 0
        let opacities: [Double] = [1, 0.7, 0.5, 0.36, 0.26]
        return MonacoTheme.ink.opacity(opacities[min(index, opacities.count - 1)])
    }
}

/// The portfolio's loading shape: the figure, the bar, then holding rows between their rules.
private struct PortfolioSkeleton: View {
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    SkeletonBlock(width: 140, height: 13)
                    SkeletonBlock(width: 196, height: 36)
                    SkeletonBlock(width: 150, height: 24, radius: 12)
                    SkeletonBlock(height: 8, radius: 4)
                        .padding(.top, MonacoTheme.Space.m)
                    SkeletonBlock(width: 220, height: 11)
                }
                .padding(.horizontal, MonacoTheme.Space.m)

                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    SkeletonBlock(width: 104, height: 20)
                        .padding(.horizontal, MonacoTheme.Space.m)
                    LedgerRowSkeleton(rows: 4)
                }
            }
            .padding(.top, MonacoTheme.Space.s)
        }
        .scrollDisabled(true)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading your portfolio")
        .accessibilityIdentifier("portfolio-loading")
    }
}

/// Placeholder rows in the shape of a ledger row: a 44pt mark, a name over a caption, a figure
/// over a caption, ruled top and bottom.
struct LedgerRowSkeleton: View {
    var rows: Int = 3

    var body: some View {
        VStack(spacing: 0) {
            ForEach(0..<rows, id: \.self) { index in
                HStack(spacing: MonacoTheme.Space.sm) {
                    SkeletonBlock(width: 44, height: 44, radius: 22)
                    VStack(alignment: .leading, spacing: 6) {
                        SkeletonBlock(width: 120, height: 14)
                        SkeletonBlock(width: 76, height: 11)
                    }
                    Spacer(minLength: MonacoTheme.Space.s)
                    VStack(alignment: .trailing, spacing: 6) {
                        SkeletonBlock(width: 64, height: 14)
                        SkeletonBlock(width: 44, height: 11)
                    }
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .padding(.vertical, 8)
                .frame(minHeight: 60)
                .overlay(alignment: .bottom) {
                    if index < rows - 1 {
                        MonacoRule().padding(.leading, MonacoTheme.Space.m + 44 + MonacoTheme.Space.sm)
                    }
                }
            }
        }
        .overlay(alignment: .top) { MonacoRule() }
        .overlay(alignment: .bottom) { MonacoRule() }
        .accessibilityHidden(true)
    }
}
