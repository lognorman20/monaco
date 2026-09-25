import MonacoCore
import SwiftUI

/// Buy, step 2 of 3: how much, and optionally why. "Review" checks the price, then pushes the receipt.
///
/// The stock sits at the top as the one line of the ledger this step is about, the amount is the
/// hero, the pot is the line under it, and the presets are quick picks in the market's voice.
struct ProposeAmountView: View {
    let groupId: String
    let stock: ProposeStock
    /// The step is only reached with a pot: the picker above owns that load and its retry, so
    /// "how much" is never asked without a ceiling to check the answer against.
    let pot: ProposePot
    let onProposed: (_ proposalId: String) -> Void

    private let service: ProposeService

    @Environment(AppSessionStore.self) private var session: AppSessionStore?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var priceMicros: Int64?
    @State private var amountText: String
    @State private var reason: String
    @State private var showsReason = false
    @State private var isQuoting = false
    @State private var quoteError: String?
    @State private var review: ProposeBuyReview?
    @State private var referencePremiumBps: Int?
    @FocusState private var reasonFocused: Bool

    /// `initialAmount` and `initialReason` start the step part-filled, as plain decimal text ("25")
    /// and the reason as typed. Empty by default, which is how the flow opens it.
    init(
        service: ProposeService,
        groupId: String,
        stock: ProposeStock,
        pot: ProposePot,
        initialAmount: String = "",
        initialReason: String = "",
        onProposed: @escaping (_ proposalId: String) -> Void
    ) {
        self.service = service
        self.groupId = groupId
        self.stock = stock
        self.pot = pot
        self.onProposed = onProposed
        _priceMicros = State(initialValue: stock.priceMicros)
        _amountText = State(initialValue: AmountEntryText.sanitize(initialAmount))
        _reason = State(initialValue: initialReason)
    }

    private var amountMicros: Int64? {
        ProposeMath.micros(fromAmountText: amountText)
    }

    private var potUsd: Decimal {
        ProposeMath.usd(fromMicros: pot.totalMicros)
    }

    private var isOverPot: Bool {
        guard let amountMicros else { return false }
        return amountMicros > pot.totalMicros
    }

    private var reasonTooLong: Bool {
        ProposeFlowCopy.reasonLength(reason) > ProposeFlowCopy.reasonMax
    }

    private var canReview: Bool {
        amountMicros != nil && !isOverPot && !reasonTooLong && !isQuoting
    }

    /// The stock as the header shows it: the price this step loaded, when the pick carried none.
    private var shownStock: ProposeStock {
        var shown = stock
        shown.priceMicros = priceMicros
        return shown
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
            await loadPrice()
        }
        .accessibilityIdentifier("propose-amount")
    }

    private var form: some View {
        ScrollView {
            VStack(spacing: 0) {
                MonacoGroupedList {
                    ProposeStockRow(
                        stock: shownStock,
                        logoURL: ProposeStockLogo.url(for: stock.symbol, in: session),
                        isLast: true
                    )
                }

                VStack(spacing: MonacoTheme.Space.m) {
                    AmountEntry(
                        amountText: $amountText,
                        max: potUsd,
                        helper: potHelper,
                        overLimitHelper: ProposeFlowCopy.overPot
                    )
                    .onChange(of: amountText) { _, _ in quoteError = nil }

                    ProposePresetChips(
                        amountText: $amountText,
                        presets: [.dollars(25), .dollars(50), .dollars(100), .fraction(1, label: "Max")],
                        max: potUsd
                    )
                }
                .padding(.top, MonacoTheme.Space.xl)
                .padding(.horizontal, MonacoTheme.Space.m)

                premiumNudge
                    .padding(.top, MonacoTheme.Space.m)
                    .padding(.horizontal, MonacoTheme.Space.m)

                reasonField
                    .padding(.top, MonacoTheme.Space.xl)
                    .padding(.horizontal, MonacoTheme.Space.m)

                if let quoteError {
                    Text(quoteError)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.loss)
                        .multilineTextAlignment(.center)
                        .fixedSize(horizontal: false, vertical: true)
                        .frame(maxWidth: .infinity)
                        .padding(.top, MonacoTheme.Space.l)
                        .padding(.horizontal, MonacoTheme.Space.m)
                        .accessibilityIdentifier("proposal-quote-error")
                }
            }
            .padding(.top, MonacoTheme.Space.s)
            .padding(.bottom, MonacoTheme.Space.l)
        }
        .scrollDismissesKeyboard(.interactively)
    }

    private var potHelper: String {
        ProposeFlowCopy.potHelper(UsdAmountFormatter.format(decimal: potUsd))
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
                withAnimation(reduceMotion ? nil : .snappy) { showsReason = true }
                reasonFocused = true
            } label: {
                Label(ProposeFlowCopy.addReason, systemImage: "plus")
                    .font(MonacoTheme.Typo.calloutStrong)
                    .foregroundStyle(MonacoTheme.brand)
                    .frame(minHeight: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .frame(maxWidth: .infinity)
            .accessibilityIdentifier("proposal-add-reason")
        }
    }

    /// A pre-IPO token trading far from its private-market reference says so before the
    /// member commits a number to it.
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
        guard let amountMicros, canReview else { return }
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
                thesis: reason.trimmingCharacters(in: .whitespacesAndNewlines),
                potMicros: pot.totalMicros
            )
        } catch {
            if error.isRequestCancellation { return }
            quoteError = ProposeErrorCopy.quote(error)
            Haptics.warning()
        }
    }
}

/// Quick amounts under the figure, as capsules set in the market's voice — the same chips the
/// cabal hero uses for its ranges, sized to a thumb. The selected one fills with ink.
///
/// `AmountEntry` draws its own presets in the brand's face; this step passes it none and draws
/// these instead, under the pot line. Four chips wrap into two rows rather than squeeze once the
/// text is too large to fit them across.
struct ProposePresetChips: View {
    @Binding var amountText: String
    let presets: [AmountPreset]
    /// What a fraction preset is a share of; nil disables the fractions.
    let max: Decimal?

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: MonacoTheme.Space.s) {
                chips(presets)
            }
            VStack(spacing: MonacoTheme.Space.s) {
                let half = (presets.count + 1) / 2
                HStack(spacing: MonacoTheme.Space.s) { chips(Array(presets.prefix(half))) }
                HStack(spacing: MonacoTheme.Space.s) { chips(Array(presets.dropFirst(half))) }
            }
        }
        .frame(maxWidth: .infinity)
    }

    private func chips(_ items: [AmountPreset]) -> some View {
        ForEach(Array(items.enumerated()), id: \.offset) { _, preset in
            chip(preset)
        }
    }

    private func chip(_ preset: AmountPreset) -> some View {
        let target = ProposePresets.amount(for: preset, max: max)
        let selected = target != nil && AmountEntryText.decimal(amountText) == target
        return Button {
            guard let target else { return }
            Haptics.selection()
            amountText = AmountEntryText.plain(target)
        } label: {
            Text(ProposePresets.label(for: preset))
                .font(MonacoTheme.Typo.dataStrong)
                .lineLimit(1)
                .foregroundStyle(
                    selected ? MonacoTheme.onBrand : (target == nil ? MonacoTheme.disabledLabel : MonacoTheme.ink)
                )
                .padding(.horizontal, MonacoTheme.Space.m)
                .frame(minWidth: 64, minHeight: 44)
                .background(Capsule().fill(selected ? MonacoTheme.brandFill : MonacoTheme.surfaceSunken))
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .disabled(target == nil)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// What a preset chip puts in the field, and what it says. Kept apart from the view so the rules
/// are checked in `ProposeRedesignTests`.
enum ProposePresets {
    /// The amount a chip enters: its dollars, or its share of `max` rounded down to the cent so a
    /// "Max" never asks for a fraction of a cent more than there is. Nil without a `max`.
    static func amount(for preset: AmountPreset, max: Decimal?) -> Decimal? {
        switch preset {
        case .dollars(let dollars):
            return dollars
        case .fraction(let fraction, _):
            guard let max, max > 0 else { return nil }
            return AmountEntryText.roundDownToCents(max * Decimal(fraction))
        }
    }

    /// "$25", "$1,000", or the fraction's own label ("Max", "50%").
    static func label(for preset: AmountPreset) -> String {
        switch preset {
        case .dollars(let dollars):
            return AmountEntryText.display(AmountEntryText.plain(dollars))
        case .fraction(_, let label):
            return label
        }
    }
}

/// The optional reason on a buy or sell amount step: a growing field on `surfaceSunken` with an ink
/// stroke while focused, and a counter once the reason nears the limit. Tagged `scrollID` so the
/// step can keep it above the keyboard and the pinned Review button.
struct ProposeReasonField: View {
    static let scrollID = "propose-reason"

    let placeholder: String
    @Binding var text: String
    var focused: FocusState<Bool>.Binding
    let lineLimit: ClosedRange<Int>
    let identifier: String

    private var length: Int { ProposeFlowCopy.reasonLength(text) }

    var body: some View {
        VStack(alignment: .trailing, spacing: MonacoTheme.Space.s) {
            TextField(
                "",
                text: $text,
                prompt: Text(placeholder).foregroundStyle(MonacoTheme.disabledLabel),
                axis: .vertical
            )
            .font(MonacoTheme.Typo.body)
            .foregroundStyle(MonacoTheme.ink)
            .tint(MonacoTheme.ink)
            .lineLimit(lineLimit)
            .focused(focused)
            .padding(MonacoTheme.Space.m)
            .background(MonacoTheme.surfaceSunken, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
                    .strokeBorder(focused.wrappedValue ? MonacoTheme.ink : .clear, lineWidth: 1)
            }
            .accessibilityLabel(placeholder)
            .accessibilityIdentifier(identifier)
            if length >= ProposeFlowCopy.reasonCounterFrom {
                // A count, so it sets in the market's voice with the other figures that tick.
                Text(ProposeFlowCopy.reasonCounter(length))
                    .font(MonacoTheme.Typo.stamp)
                    .foregroundStyle(length > ProposeFlowCopy.reasonMax ? MonacoTheme.loss : MonacoTheme.tertiaryText)
                    .accessibilityIdentifier("proposal-reason-counter")
            }
        }
        .frame(maxWidth: .infinity)
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
    /// The whole pot when the price was checked, which the receipt measures the buy against.
    var potMicros: Int64 = 0

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
