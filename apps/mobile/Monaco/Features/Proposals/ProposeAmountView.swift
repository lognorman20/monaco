import MonacoCore
import SwiftUI

/// Buy, step 2 of 3: how much, and optionally why. "Review" checks the price, then pushes the receipt.
struct ProposeAmountView: View {
    let groupId: String
    let stock: ProposeStock
    let onProposed: (_ proposalId: String) -> Void

    private let service: ProposeService

    @State private var pot: ProposePot?
    @State private var potLoadFailed = false
    @State private var priceMicros: Int64?
    @State private var amountText = ""
    @State private var reason = ""
    @State private var showsReason = false
    @State private var isQuoting = false
    @State private var quoteError: String?
    @State private var review: ProposeBuyReview?
    @State private var referencePremiumBps: Int?
    @FocusState private var reasonFocused: Bool

    init(service: ProposeService, groupId: String, stock: ProposeStock, pot: ProposePot?, onProposed: @escaping (_ proposalId: String) -> Void) {
        self.service = service
        self.groupId = groupId
        self.stock = stock
        self.onProposed = onProposed
        _pot = State(initialValue: pot)
        _priceMicros = State(initialValue: stock.priceMicros)
    }

    private var amountMicros: Int64? {
        ProposeMath.micros(fromAmountText: amountText)
    }

    private var potUsd: Decimal? {
        pot.map { ProposeMath.usd(fromMicros: $0.totalMicros) }
    }

    private var isOverPot: Bool {
        guard let amountMicros, let pot else { return false }
        return amountMicros > pot.totalMicros
    }

    private var reasonTooLong: Bool {
        ProposeFlowCopy.reasonLength(reason) > ProposeFlowCopy.reasonMax
    }

    private var canReview: Bool {
        amountMicros != nil && pot != nil && !isOverPot && !reasonTooLong && !isQuoting
    }

    var body: some View {
        ScrollViewReader { proxy in
            form
                .revealsWhileActive(ProposeReasonField.scrollID, isActive: reasonFocused, tracking: reason, proxy: proxy)
        }
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .navigationTitle(ProposeFlowCopy.amountTitle)
        .navigationBarTitleDisplayMode(.inline)
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                Button {
                    Task { await fetchQuote() }
                } label: {
                    ZStack {
                        Text(ProposeFlowCopy.review).opacity(isQuoting ? 0 : 1)
                        if isQuoting {
                            ProgressView().tint(MonacoTheme.primaryButtonLabel)
                        }
                    }
                }
                .buttonStyle(.monacoPrimary)
                .disabled(!canReview)
                .accessibilityIdentifier("proposal-quote-button")
            }
        }
        .navigationDestination(item: $review) { review in
            ProposeReviewView(service: service, groupId: groupId, review: review, onProposed: onProposed)
        }
        .task {
            await loadPot()
            await loadPrice()
        }
        .accessibilityIdentifier("propose-amount")
    }

    private var form: some View {
        ScrollView {
            VStack(spacing: MonacoTheme.Space.l) {
                header
                AmountEntry(
                    amountText: $amountText,
                    max: potUsd,
                    presets: [.dollars(25), .dollars(50), .dollars(100), .fraction(1, label: "Max")],
                    helper: potHelper,
                    overLimitHelper: ProposeFlowCopy.overPot
                )
                .onChange(of: amountText) { _, _ in quoteError = nil }
                premiumNudge
                reasonField
                if let quoteError {
                    Text(quoteError)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.loss)
                        .multilineTextAlignment(.center)
                        .frame(maxWidth: .infinity)
                        .accessibilityIdentifier("proposal-quote-error")
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.l)
        }
        .scrollDismissesKeyboard(.interactively)
    }

    private var potHelper: String? {
        if let potUsd { return ProposeFlowCopy.potHelper(UsdAmountFormatter.format(decimal: potUsd)) }
        return potLoadFailed ? ProposeFlowCopy.potLoadFailed : nil
    }

    @ViewBuilder
    private var premiumNudge: some View {
        if let bps = referencePremiumBps, PreIpoCopy.showsPremiumNudge(premiumBps: bps, assetKind: stock.assetKind) {
            Text(PreIpoCopy.tradingPremiumNudge(bps: bps))
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.muted)
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity)
                .accessibilityIdentifier("proposal-premium-nudge")
        }
    }

    private var header: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            StockMark(symbol: stock.symbol, displayName: stock.name, assetKind: stock.assetKind, size: 56)
            VStack(alignment: .leading, spacing: 2) {
                Text(stock.name)
                    .font(MonacoTheme.Typo.title)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
                HStack(spacing: 6) {
                    Text(stock.ticker)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                    if let priceMicros {
                        Text("·").font(MonacoTheme.Typo.caption).foregroundStyle(MonacoTheme.tertiaryText)
                        MoneyText(micros: priceMicros, style: .caption, color: MonacoTheme.muted)
                    }
                }
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var reasonField: some View {
        if showsReason || !reason.isEmpty {
            ProposeReasonField(
                placeholder: ProposeFlowCopy.reasonPlaceholderBuy,
                text: $reason,
                focused: $reasonFocused,
                lineLimit: 3...8,
                identifier: "proposal-thesis-field"
            )
        } else {
            Button {
                withAnimation(.snappy) { showsReason = true }
                reasonFocused = true
            } label: {
                Label(ProposeFlowCopy.addReason, systemImage: "plus")
                    .font(MonacoTheme.Typo.callout.weight(.semibold))
                    .foregroundStyle(MonacoTheme.ink)
                    .frame(minHeight: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .frame(maxWidth: .infinity)
            .accessibilityIdentifier("proposal-add-reason")
        }
    }

    private func loadPot() async {
        guard pot == nil else { return }
        potLoadFailed = false
        do {
            pot = try await service.pot(groupId: groupId)
        } catch {
            if !error.isRequestCancellation { potLoadFailed = true }
        }
    }

    private func loadPrice() async {
        guard priceMicros == nil else { return }
        if let detail = try? await service.assetDetail(symbol: stock.symbol) {
            priceMicros = detail.priceUsdcMicros
            referencePremiumBps = detail.premiumBps
        } else {
            priceMicros = try? await service.priceMicros(symbol: stock.symbol)
        }
    }

    private func fetchQuote() async {
        guard let amountMicros, let pot, canReview else { return }
        Haptics.tap()
        isQuoting = true
        quoteError = nil
        defer { isQuoting = false }
        do {
            let quote = try await service.buyQuote(groupId: groupId, symbol: stock.symbol, usdcMicros: amountMicros)
            guard quote.routable else {
                quoteError = ProposeFlowCopy.cantBuyStock(stock.name)
                Haptics.warning()
                return
            }
            if let bps = quote.premiumBps {
                referencePremiumBps = bps
            }
            review = ProposeBuyReview(
                stock: stock,
                usdcMicros: amountMicros,
                quote: quote,
                fallbackPriceMicros: priceMicros,
                cabalId: pot.groupId.isEmpty ? groupId : pot.groupId,
                cabalName: pot.name,
                thesis: reason.trimmingCharacters(in: .whitespacesAndNewlines)
            )
        } catch {
            if error.isRequestCancellation { return }
            quoteError = ProposeErrorCopy.quote(error)
            Haptics.warning()
        }
    }
}

/// The optional reason on a buy or sell amount step: a growing field on `surfaceSunken` with an ink
/// stroke while focused, and a counter once the reason nears the limit. Tagged `scrollID` so the step
/// can keep it above the keyboard and the pinned Review button.
struct ProposeReasonField: View {
    static let scrollID = "propose-reason"

    let placeholder: String
    @Binding var text: String
    var focused: FocusState<Bool>.Binding
    let lineLimit: ClosedRange<Int>
    let identifier: String

    private var length: Int { ProposeFlowCopy.reasonLength(text) }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            TextField(placeholder, text: $text, axis: .vertical)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(lineLimit)
                .focused(focused)
                .padding(MonacoTheme.Space.m)
                .background(MonacoTheme.surfaceSunken, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
                        .strokeBorder(focused.wrappedValue ? MonacoTheme.ink : .clear, lineWidth: 1)
                }
                .accessibilityIdentifier(identifier)
            if length >= ProposeFlowCopy.reasonCounterFrom {
                Text(ProposeFlowCopy.reasonCounter(length))
                    .font(MonacoTheme.Typo.caption.monospacedDigit())
                    .foregroundStyle(length > ProposeFlowCopy.reasonMax ? MonacoTheme.loss : MonacoTheme.muted)
                    .frame(maxWidth: .infinity, alignment: .trailing)
            }
        }
        .id(Self.scrollID)
    }
}

/// Everything the receipt shows, fixed at the moment the price was checked.
struct ProposeBuyReview: Hashable, Identifiable {
    let stock: ProposeStock
    let usdcMicros: Int64
    let quote: BuyQuoteDTO
    let fallbackPriceMicros: Int64?
    let cabalId: String
    let cabalName: String
    let thesis: String

    var id: String { "\(stock.symbol)-\(usdcMicros)-\(thesis.hashValue)" }

    /// Price per share: the quote's own price, else amount ÷ shares out, else the market price.
    var priceMicros: Int64? {
        if let raw = quote.priceUsdcMicros, let micros = Int64(raw), micros > 0 { return micros }
        if let shares = shares, shares > 0 {
            return ProposeMath.micros(fromUsd: ProposeMath.usd(fromMicros: usdcMicros) / shares)
        }
        return fallbackPriceMicros
    }

    var shares: Decimal? {
        quote.outputAmount.flatMap { ProposeMath.shares(fromAtomics: $0, decimals: quote.resolvedDecimals, multiplier: quote.resolvedUiMultiplier) }
    }

    var sharesLabel: String? {
        quote.outputAmount.map {
            ProposalShareFormatter.sharesLabel(fromAtomics: $0, decimals: quote.resolvedDecimals, kind: quote.resolvedAssetKind)
        }
    }
}

extension BuyQuoteDTO: Hashable {
    public func hash(into hasher: inout Hasher) {
        hasher.combine(symbol)
        hasher.combine(usdcMicros)
        hasher.combine(tokenAmount)
        hasher.combine(outputAmount)
    }
}
