import MonacoCore
import SwiftUI

/// Sell, step 1 of 3: which holding. Members think in dollars, so step 2 asks for an amount in
/// dollars and converts to token atomics at the holding's mark.
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
    init(auth: DynamicAuthService, groupId: String, holdings: [PotRowDTO], initialSymbol: String? = nil, onProposed: ((_ proposalId: String) -> Void)? = nil) {
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

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                MonacoSectionHeader(ProposeFlowCopy.holdingsTitle)
                MonacoGroupedList {
                    ForEach(Array(holdings.enumerated()), id: \.element.id) { index, row in
                        Button {
                            Haptics.tap()
                            picked = PickedHolding(row: row)
                        } label: {
                            MonacoRow(
                                title: ProposeStock.displayName(symbol: row.symbol),
                                subtitle: ProposalShareFormatter.sharesLabel(fromAtomics: row.tokenAmount ?? "0"),
                                chevron: true,
                                isLast: index == holdings.count - 1
                            ) {
                                StockMark(symbol: row.symbol)
                            } trailing: {
                                MoneyText(decimalString: row.valueUsd, style: .row)
                            }
                        }
                        .buttonStyle(.monacoRow)
                        .accessibilityIdentifier("proposal-sell-\(row.symbol)")
                    }
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
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

/// Sell, step 2 of 3: dollars (or shares when the holding has no price), and an optional reason.
struct ProposeSellAmountView: View {
    let groupId: String
    let holding: PotRowDTO
    let onProposed: (_ proposalId: String) -> Void

    private let service: ProposeService

    @State private var pot: ProposePot?
    @State private var amountText = ""
    @State private var reason = ""
    @State private var isQuoting = false
    @State private var errorMessage: String?
    @State private var review: ProposeSellReview?
    @FocusState private var reasonFocused: Bool

    init(service: ProposeService, groupId: String, holding: PotRowDTO, pot: ProposePot?, onProposed: @escaping (_ proposalId: String) -> Void) {
        self.service = service
        self.groupId = groupId
        self.holding = holding
        self.onProposed = onProposed
        _pot = State(initialValue: pot)
    }

    private var name: String { ProposeStock.displayName(symbol: holding.symbol) }
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
            return ProposeMath.atomics(forUsd: entered, markUsd: markUsd, ceiling: ceilingAtomics)
        }
        guard let atomics = ProposeMath.atomics(fromShares: amountText), atomics <= ceilingAtomics else { return nil }
        return atomics
    }

    private var reasonTooLong: Bool {
        ProposeFlowCopy.reasonLength(reason) > ProposeFlowCopy.reasonMax
    }

    private var isOverHoldings: Bool {
        guard let entered = enteredValue else { return false }
        if entersDollars, let valueUsd { return entered > valueUsd }
        return (ProposeMath.atomics(fromShares: amountText) ?? 0) > ceilingAtomics
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
            VStack(spacing: MonacoTheme.Space.xl) {
                HStack(spacing: MonacoTheme.Space.sm) {
                    StockMark(symbol: holding.symbol, size: 56)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(name)
                            .font(MonacoTheme.Typo.title)
                            .foregroundStyle(MonacoTheme.ink)
                            .lineLimit(1)
                        Text(ProposalShareFormatter.sharesLabel(fromAtomics: holding.tokenAmount ?? "0"))
                            .font(MonacoTheme.Typo.caption.monospacedDigit())
                            .foregroundStyle(MonacoTheme.muted)
                    }
                    Spacer(minLength: 0)
                }
                .accessibilityElement(children: .combine)

                if entersDollars, let valueUsd {
                    AmountEntry(
                        amountText: $amountText,
                        max: valueUsd,
                        presets: [.fraction(0.25, label: "25%"), .fraction(0.5, label: "50%"), .fraction(1, label: "All")],
                        helper: ProposeFlowCopy.sellHelper(UsdAmountFormatter.format(decimal: valueUsd)),
                        overLimitHelper: ProposeFlowCopy.overHoldings
                    )
                } else {
                    VStack(spacing: MonacoTheme.Space.s) {
                        MonacoTextField(ProposeFlowCopy.sharesRow, text: $amountText, keyboard: .decimalPad)
                            .accessibilityIdentifier("proposal-sell-amount")
                        Text(isOverHoldings ? ProposeFlowCopy.overHoldings : ProposalShareFormatter.sharesLabel(fromAtomics: holding.tokenAmount ?? "0"))
                            .font(MonacoTheme.Typo.callout)
                            .foregroundStyle(isOverHoldings ? MonacoTheme.loss : MonacoTheme.muted)
                    }
                }

                ProposeReasonField(
                    placeholder: ProposeFlowCopy.reasonPlaceholderSell,
                    text: $reason,
                    focused: $reasonFocused,
                    lineLimit: 2...6,
                    identifier: "proposal-sell-thesis-field"
                )

                if let errorMessage {
                    Text(errorMessage)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.loss)
                        .multilineTextAlignment(.center)
                        .frame(maxWidth: .infinity)
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.m)
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
                    ProposeMath.shares(fromAtomics: String(tokenAmount)).flatMap { ProposeMath.micros(fromUsd: $0 * mark) }
                }
            review = ProposeSellReview(
                symbol: holding.symbol,
                name: name,
                tokenAmount: tokenAmount,
                estimateMicros: estimate,
                cabalId: pot?.groupId ?? groupId,
                cabalName: pot?.name,
                thesis: reason.trimmingCharacters(in: .whitespacesAndNewlines)
            )
        } catch {
            if error.isRequestCancellation { return }
            errorMessage = ProposeErrorCopy.sell(error)
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

    var id: String { "\(symbol)-\(tokenAmount)" }

    var sharesLabel: String {
        ProposalShareFormatter.sharesLabel(fromAtomics: String(tokenAmount))
    }
}

/// Sell, step 3 of 3.
struct ProposeSellReviewView: View {
    let groupId: String
    let review: ProposeSellReview
    let onProposed: (_ proposalId: String) -> Void

    private let service: ProposeService

    @State private var isSending = false
    @State private var errorMessage: String?

    init(service: ProposeService, groupId: String, review: ProposeSellReview, onProposed: @escaping (_ proposalId: String) -> Void) {
        self.service = service
        self.groupId = groupId
        self.review = review
        self.onProposed = onProposed
    }

    var body: some View {
        ScrollView {
            VStack(spacing: MonacoTheme.Space.xl) {
                ProposeReceiptHeader(
                    caption: ProposeFlowCopy.youreSelling,
                    amount: Group {
                        if let estimate = review.estimateMicros {
                            MoneyText(micros: estimate, style: .hero)
                        } else {
                            Text(review.sharesLabel).font(MonacoTheme.Typo.moneyHero)
                        }
                    },
                    stockName: review.name,
                    detail: review.estimateMicros == nil ? nil : ProposeFlowCopy.aboutShares(review.sharesLabel)
                )
                .accessibilityLabel(
                    ProposeFlowCopy.sellSummary(
                        amount: review.estimateMicros.map(UsdAmountFormatter.format(micros:)) ?? review.sharesLabel,
                        name: review.name,
                        shares: review.sharesLabel
                    )
                )

                MonacoGroupedList {
                    ReceiptRow(label: ProposeFlowCopy.sharesRow) {
                        Text(review.sharesLabel).font(MonacoTheme.Typo.body.monospacedDigit())
                    }
                    if let cabalName = review.cabalName {
                        ReceiptRow(label: ProposeFlowCopy.cabalRow, isLast: review.thesis.isEmpty) {
                            HStack(spacing: MonacoTheme.Space.s) {
                                CabalMark(groupId: review.cabalId, name: cabalName, size: 28)
                                Text(cabalName).font(MonacoTheme.Typo.body).lineLimit(1)
                            }
                        }
                    }
                    if !review.thesis.isEmpty {
                        ReceiptReasonRow(text: review.thesis)
                    }
                }

                if let errorMessage {
                    Text(errorMessage)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.loss)
                        .multilineTextAlignment(.center)
                        .frame(maxWidth: .infinity)
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.vertical, MonacoTheme.Space.l)
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

    private func send() async {
        guard !isSending else { return }
        Haptics.tap()
        isSending = true
        errorMessage = nil
        defer { isSending = false }
        do {
            let id = try await service.propose(
                groupId: groupId,
                draft: .sell(symbol: review.symbol, tokenAmount: review.tokenAmount, thesis: review.thesis)
            )
            onProposed(id)
        } catch {
            if error.isRequestCancellation { return }
            errorMessage = ProposeErrorCopy.sell(error)
            Haptics.warning()
        }
    }
}
