import MonacoCore
import SwiftUI

/// Sell, step 1 of 3: which holding. Members think in dollars, so step 2 asks for an amount in
/// dollars and converts to token atomics at the holding's mark.
///
/// The holdings read as they do on the cabal screen — largest first, the ticker in the market's
/// voice, the shares and the mark under it, what the position is worth and what it has made on
/// the right — so the thing being sold looks the same here as where the member last saw it.
struct ProposeSellView: View {
    let groupId: String
    var initialSymbol: String?
    var onProposed: ((_ proposalId: String) -> Void)?

    private let service: ProposeService
    private let holdings: [PotRowDTO]

    @State private var pot: ProposePot?
    @State private var picked: PickedHolding?
    @State private var toast: MonacoToast?
    @State private var didApplyInitialSymbol = false

    /// Entry from Stock detail's cabal picker.
    init(auth: PrivyAuthService, groupId: String, holdings: [PotRowDTO], initialSymbol: String? = nil, onProposed: ((_ proposalId: String) -> Void)? = nil) {
        self.service = LiveProposeService(auth: auth)
        self.groupId = groupId
        self.holdings = holdings
        self.initialSymbol = initialSymbol
        self.onProposed = onProposed
    }

    init(service: ProposeService, groupId: String, pot: ProposePot, initialSymbol: String? = nil, onProposed: ((_ proposalId: String) -> Void)? = nil) {
        self.service = service
        self.groupId = groupId
        self.holdings = pot.holdings
        self.initialSymbol = initialSymbol
        self.onProposed = onProposed
        _pot = State(initialValue: pot)
    }

    /// Largest position first, as the cabal screen lists them.
    private var sortedHoldings: [PotRowDTO] {
        holdings.sorted {
            (GroupHeroMath.decimal(from: $0.valueUsd) ?? 0) > (GroupHeroMath.decimal(from: $1.valueUsd) ?? 0)
        }
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                MonacoSectionHeader(ProposeFlowCopy.holdingsTitle)
                    .padding(.horizontal, MonacoTheme.Space.m)
                if holdings.isEmpty {
                    EmptyState(title: ProposeFlowCopy.sellRowEmpty)
                } else {
                    let rows = sortedHoldings
                    MonacoGroupedList {
                        ForEach(rows) { row in
                            Button {
                                Haptics.tap()
                                picked = PickedHolding(row: row)
                            } label: {
                                ProposeHoldingRow(row: row, chevron: true, isLast: row.id == rows.last?.id)
                            }
                            .buttonStyle(.monacoRow)
                            .accessibilityIdentifier("proposal-sell-\(row.symbol)")
                        }
                    }
                }
            }
            .padding(.top, MonacoTheme.Space.s)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .navigationTitle(ProposeFlowCopy.sellTitle)
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(item: $picked) { picked in
            ProposeSellAmountView(service: service, groupId: groupId, holding: picked.row, pot: pot, onProposed: finish)
        }
        .monacoToast($toast)
        .task {
            if !didApplyInitialSymbol, let initialSymbol {
                didApplyInitialSymbol = true
                picked = holdings
                    .first { $0.symbol.caseInsensitiveCompare(initialSymbol) == .orderedSame }
                    .map(PickedHolding.init(row:))
            }
            if pot == nil { pot = try? await service.pot(groupId: groupId) }
        }
        .accessibilityIdentifier("propose-sell")
    }

    private func finish(_ proposalId: String) {
        if let onProposed {
            onProposed(proposalId)
            return
        }
        picked = nil
        Haptics.success()
        toast = MonacoToast(
            message: pot.map { ProposeFlowCopy.proposalSent($0.name) } ?? ProposeFlowCopy.proposalSentGeneric,
            isSuccess: true
        )
    }
}

/// `PotRowDTO` is not Hashable; navigation keys on the symbol.
struct PickedHolding: Hashable, Identifiable {
    let row: PotRowDTO

    var id: String { row.symbol }

    static func == (lhs: PickedHolding, rhs: PickedHolding) -> Bool { lhs.row == rhs.row }
    func hash(into hasher: inout Hasher) { hasher.combine(row.symbol) }
}

/// One holding the cabal could sell: the coin, the ticker over "1.2034 shares · $231.40", and the
/// position's value over what it has made. The cabal screen's holdings row without the day's
/// shape — here the question is how much to sell, not how the stock moved today.
struct ProposeHoldingRow: View {
    let row: PotRowDTO
    var chevron = false
    var isLast = false

    var body: some View {
        MonacoRow(
            title: AssetSymbolFormatter.display(row.symbol, kind: row.resolvedAssetKind),
            titleFont: MonacoTheme.Typo.ticker,
            subtitle: PotSectionView.potSubtitle(row),
            chevron: chevron,
            isLast: isLast
        ) {
            StockMark(
                symbol: row.symbol,
                displayName: AssetCatalogDisplayName.format(catalogName: "", symbol: row.symbol, kind: row.resolvedAssetKind),
                assetKind: row.resolvedAssetKind,
                logoURL: row.logoURL
            )
        } trailing: {
            MoneyText(decimalString: row.valueUsd, style: .row)
            PnLText(dollarPnl: row.dollarPnl, style: .caption)
        }
    }
}

/// Sell, step 2 of 3: dollars (or shares when the holding has no price), and an optional reason.
struct ProposeSellAmountView: View {
    let groupId: String
    let holding: PotRowDTO
    let onProposed: (_ proposalId: String) -> Void

    private let service: ProposeService

    @State private var pot: ProposePot?
    @State private var amountText: String
    @State private var reason: String
    @State private var isQuoting = false
    @State private var errorMessage: String?
    @State private var review: ProposeSellReview?
    @FocusState private var reasonFocused: Bool

    /// `initialAmount` and `initialReason` start the step part-filled; empty by default, which is
    /// how the flow opens it.
    init(
        service: ProposeService,
        groupId: String,
        holding: PotRowDTO,
        pot: ProposePot?,
        initialAmount: String = "",
        initialReason: String = "",
        onProposed: @escaping (_ proposalId: String) -> Void
    ) {
        self.service = service
        self.groupId = groupId
        self.holding = holding
        self.onProposed = onProposed
        _pot = State(initialValue: pot)
        _amountText = State(initialValue: AmountEntryText.sanitize(initialAmount))
        _reason = State(initialValue: initialReason)
    }

    private var name: String {
        AssetCatalogDisplayName.format(catalogName: "", symbol: holding.symbol, kind: holding.resolvedAssetKind)
    }
    private var quantityRowLabel: String {
        holding.resolvedAssetKind == .preIpo ? PreIpoCopy.tokensRowLabel : ProposeFlowCopy.sharesRow
    }
    private var ceilingAtomics: Int64 { Int64(holding.tokenAmount ?? "0") ?? 0 }
    private var markUsd: Decimal? {
        Decimal(string: holding.markUsd, locale: Locale(identifier: "en_US_POSIX")).flatMap { $0 > 0 ? $0 : nil }
    }
    private var valueUsd: Decimal? {
        Decimal(string: holding.valueUsd, locale: Locale(identifier: "en_US_POSIX"))
    }
    /// Dollar entry needs a mark to convert; without one, the member enters shares.
    private var entersDollars: Bool { markUsd != nil && valueUsd != nil }

    private var enteredValue: Decimal? { AmountEntryText.decimal(amountText) }

    private var tokenAmount: Int64? {
        guard let entered = enteredValue, entered > 0 else { return nil }
        if entersDollars, let markUsd, let valueUsd {
            // "All" (the full value, in cents) sells every atomic, never leaving dust behind.
            if entered > valueUsd { return nil }
            if entered >= AmountEntryText.roundDownToCents(valueUsd) { return ceilingAtomics }
            return ProposeMath.atomics(
                forUsd: entered,
                markUsd: markUsd,
                ceiling: ceilingAtomics,
                decimals: holding.resolvedTokenDecimals,
                multiplier: holding.resolvedUiMultiplier
            )
        }
        guard let atomics = ProposeMath.atomics(fromShares: amountText, decimals: holding.resolvedTokenDecimals, multiplier: holding.resolvedUiMultiplier), atomics <= ceilingAtomics else { return nil }
        return atomics
    }

    private var reasonTooLong: Bool {
        ProposeFlowCopy.reasonLength(reason) > ProposeFlowCopy.reasonMax
    }

    private var isOverHoldings: Bool {
        guard let entered = enteredValue else { return false }
        if entersDollars, let valueUsd { return entered > valueUsd }
        return (ProposeMath.atomics(fromShares: amountText, decimals: holding.resolvedTokenDecimals, multiplier: holding.resolvedUiMultiplier) ?? 0) > ceilingAtomics
    }

    var body: some View {
        ScrollViewReader { proxy in
            form
                .revealsWhileActive(ProposeReasonField.scrollID, isActive: reasonFocused, tracking: reason, proxy: proxy)
        }
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .navigationTitle(ProposeFlowCopy.amountTitle)
        .navigationBarTitleDisplayMode(.inline)
        .onChange(of: amountText) { _, _ in errorMessage = nil }
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                Button {
                    Task { await fetchQuote() }
                } label: {
                    ZStack {
                        Text(ProposeFlowCopy.review).opacity(isQuoting ? 0 : 1)
                        if isQuoting { ProgressView().tint(MonacoTheme.primaryButtonLabel) }
                    }
                }
                .buttonStyle(.monacoPrimary)
                .disabled(tokenAmount == nil || isQuoting || reasonTooLong)
                .accessibilityIdentifier("proposal-sell-quote")
            }
        }
        .navigationDestination(item: $review) { review in
            ProposeSellReviewView(service: service, groupId: groupId, review: review, onProposed: onProposed)
        }
        .task {
            if pot == nil { pot = try? await service.pot(groupId: groupId) }
        }
    }

    private var form: some View {
        ScrollView {
            VStack(spacing: 0) {
                MonacoGroupedList {
                    ProposeHoldingRow(row: holding, isLast: true)
                }

                Group {
                    if entersDollars, let valueUsd {
                        VStack(spacing: MonacoTheme.Space.m) {
                            AmountEntry(
                                amountText: $amountText,
                                max: valueUsd,
                                helper: ProposeFlowCopy.sellHelper(UsdAmountFormatter.format(decimal: valueUsd)),
                                overLimitHelper: ProposeFlowCopy.overHoldings
                            )
                            ProposePresetChips(
                                amountText: $amountText,
                                presets: [.fraction(0.25, label: "25%"), .fraction(0.5, label: "50%"), .fraction(1, label: "All")],
                                max: valueUsd
                            )
                        }
                    } else {
                        VStack(spacing: MonacoTheme.Space.s) {
                            MonacoTextField(quantityRowLabel, text: $amountText, keyboard: .decimalPad)
                                .accessibilityIdentifier("proposal-sell-amount")
                            Text(isOverHoldings ? ProposeFlowCopy.overHoldings : ProposalShareFormatter.sharesLabel(
                            fromAtomics: holding.tokenAmount ?? "0",
                            decimals: holding.resolvedTokenDecimals,
                            kind: holding.resolvedAssetKind
                        ))
                                .font(MonacoTheme.Typo.callout)
                                .foregroundStyle(isOverHoldings ? MonacoTheme.loss : MonacoTheme.muted)
                        }
                    }
                }
                .padding(.top, MonacoTheme.Space.xl)
                .padding(.horizontal, MonacoTheme.Space.m)

                ProposeReasonField(
                    placeholder: ProposeFlowCopy.reasonPlaceholderSell,
                    text: $reason,
                    focused: $reasonFocused,
                    lineLimit: 2...6,
                    identifier: "proposal-sell-thesis-field"
                )
                .padding(.top, MonacoTheme.Space.xl)
                .padding(.horizontal, MonacoTheme.Space.m)

                if let errorMessage {
                    Text(errorMessage)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.loss)
                        .multilineTextAlignment(.center)
                        .fixedSize(horizontal: false, vertical: true)
                        .frame(maxWidth: .infinity)
                        .padding(.top, MonacoTheme.Space.l)
                        .padding(.horizontal, MonacoTheme.Space.m)
                }
            }
            .padding(.top, MonacoTheme.Space.s)
            .padding(.bottom, MonacoTheme.Space.l)
        }
        .scrollDismissesKeyboard(.interactively)
    }

    private func fetchQuote() async {
        guard let tokenAmount else { return }
        Haptics.tap()
        isQuoting = true
        errorMessage = nil
        defer { isQuoting = false }
        do {
            let quote = try await service.sellQuote(groupId: groupId, symbol: holding.symbol, tokenAmount: tokenAmount)
            guard quote.routable else {
                errorMessage = ProposeFlowCopy.sellTooSmall
                Haptics.warning()
                return
            }
            let estimate = quote.outputUsdcMicros.flatMap { Int64($0) }
                ?? markUsd.flatMap { mark in
                    ProposeMath.shares(fromAtomics: String(tokenAmount), decimals: holding.resolvedTokenDecimals).flatMap { ProposeMath.micros(fromUsd: $0 * mark) }
                }
            review = ProposeSellReview(
                symbol: holding.symbol,
                name: name,
                tokenAmount: tokenAmount,
                estimateMicros: estimate,
                cabalId: pot?.groupId ?? groupId,
                cabalName: pot?.name,
                thesis: reason.trimmingCharacters(in: .whitespacesAndNewlines),
                heldTokenAmount: ceilingAtomics,
                logoURL: holding.logoURL,
                assetKind: holding.resolvedAssetKind,
                tokenDecimals: holding.resolvedTokenDecimals
            )
        } catch {
            if error.isRequestCancellation { return }
            // Nothing has been sent yet, so this is a failed price check, not a failed proposal —
            // but Review is reachable while the member is over the holding, and the quote endpoint
            // refuses that in the same words the propose endpoint does, so it still gets named.
            errorMessage = ProposeErrorCopy.quote(error, isSell: true)
            Haptics.warning()
        }
    }
}

struct ProposeSellReview: Hashable, Identifiable {
    let symbol: String
    let name: String
    let tokenAmount: Int64
    let estimateMicros: Int64?
    let cabalId: String
    let cabalName: String?
    let thesis: String
    /// What the cabal held when the sale was priced, so the receipt can say what it keeps.
    var heldTokenAmount: Int64?
    /// The company's logo from the holding, for the receipt's coin.
    var logoURL: URL?
    /// A pre-IPO token counts in tokens with its own decimals, not in shares.
    var assetKind: AssetKind = .stock
    var tokenDecimals: Int = AssetCatalogDefaults.decimals

    var id: String { "\(symbol)-\(tokenAmount)" }

    var sharesLabel: String {
        ProposalShareFormatter.sharesLabel(fromAtomics: String(tokenAmount), decimals: tokenDecimals, kind: assetKind)
    }
}

/// Sell, step 3 of 3: the same receipt as a buy. The headline is the number of shares, because
/// that is what the cabal votes to sell; what they raise is the price check's estimate, under it.
struct ProposeSellReviewView: View {
    let groupId: String
    let review: ProposeSellReview
    let onProposed: (_ proposalId: String) -> Void

    private let service: ProposeService

    @State private var isSending = false
    /// Idempotency key for the proposal being sent; a retry after a lost response reuses it.
    @State private var proposeSubmission = IdempotentSubmission()
    @State private var errorMessage: String?

    init(service: ProposeService, groupId: String, review: ProposeSellReview, onProposed: @escaping (_ proposalId: String) -> Void) {
        self.service = service
        self.groupId = groupId
        self.review = review
        self.onProposed = onProposed
    }

    private var ticker: String { AssetSymbolFormatter.display(review.symbol) }

    /// The rows this sale can fill, in order; the last one drops its rule.
    private enum Line: Hashable {
        case raises(Int64)
        case keeps(String)
        case votes(String)
    }

    private var lines: [Line] {
        var lines: [Line] = []
        if let estimate = review.estimateMicros { lines.append(.raises(estimate)) }
        if let held = review.heldTokenAmount {
            lines.append(.keeps(ProposeScreenCopy.keeps(heldAtomics: held, soldAtomics: review.tokenAmount)))
        }
        if let cabalName = review.cabalName { lines.append(.votes(cabalName)) }
        return lines
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                ProposeReceiptHeader(
                    symbol: review.symbol,
                    name: review.name,
                    logoURL: review.logoURL,
                    headline: ProposeScreenCopy.sellHeadline(shares: review.sharesLabel, ticker: ticker)
                )
                .accessibilityElement(children: .combine)
                .accessibilityLabel(
                    ProposeFlowCopy.sellSummary(
                        amount: review.estimateMicros.map(UsdAmountFormatter.format(micros:)) ?? review.sharesLabel,
                        name: review.name,
                        shares: review.sharesLabel
                    )
                )
                .padding(.horizontal, MonacoTheme.Space.m)
                .padding(.bottom, MonacoTheme.Space.l)

                if !lines.isEmpty {
                    MonacoGroupedList {
                        ForEach(lines, id: \.self) { line in
                            row(line, isLast: line == lines.last)
                        }
                    }
                }

                if !review.thesis.isEmpty {
                    ReceiptReasonRow(title: ProposeScreenCopy.reasonTitle(isSell: true), text: review.thesis)
                        .padding(.horizontal, MonacoTheme.Space.m)
                        .padding(.top, MonacoTheme.Space.l)
                }

                if let errorMessage {
                    ReceiptError(message: errorMessage)
                        .padding(.horizontal, MonacoTheme.Space.m)
                        .padding(.top, MonacoTheme.Space.l)
                }
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.l)
        }
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .navigationTitle(ProposeFlowCopy.review)
        .navigationBarTitleDisplayMode(.inline)
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                Button {
                    Task { await send() }
                } label: {
                    ZStack {
                        Text(ProposeFlowCopy.sendToCabal).opacity(isSending ? 0 : 1)
                        if isSending { ProgressView().tint(MonacoTheme.primaryButtonLabel) }
                    }
                }
                .buttonStyle(.monacoPrimary)
                .disabled(isSending)
                .accessibilityIdentifier("proposal-sell-submit")
            }
        }
    }

    @ViewBuilder
    private func row(_ line: Line, isLast: Bool) -> some View {
        switch line {
        case .raises(let micros):
            ReceiptRow(label: ProposeScreenCopy.raisesRow, isLast: isLast) {
                ReceiptFigure(ProposeScreenCopy.about(UsdAmountFormatter.format(micros: micros)))
            }
        case .keeps(let left):
            ReceiptRow(label: ProposeScreenCopy.keepsRow, isLast: isLast) {
                ReceiptFigure(left)
            }
        case .votes(let cabalName):
            ReceiptRow(label: ProposeScreenCopy.whoVotesRow, isLast: isLast) {
                ReceiptCabal(groupId: review.cabalId, name: cabalName)
            }
        }
    }

    private func send() async {
        guard !isSending else { return }
        Haptics.tap()
        isSending = true
        errorMessage = nil
        defer { isSending = false }
        do {
            let id = try await service.propose(
                groupId: groupId,
                draft: .sell(symbol: review.symbol, tokenAmount: review.tokenAmount, thesis: review.thesis),
                submission: proposeSubmission
            )
            onProposed(id)
        } catch {
            if error.isRequestCancellation { return }
            errorMessage = ProposeErrorCopy.propose(error, isSell: true)
            Haptics.warning()
        }
    }
}
