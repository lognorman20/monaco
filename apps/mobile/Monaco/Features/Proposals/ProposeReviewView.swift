import MonacoCore
import SwiftUI

/// Buy, step 3 of 3: a receipt of what the cabal will vote on, then "Send to cabal".
///
/// Set like a line in the ledger: the stock, the sentence the cabal is asked to agree to, then the
/// figures the price check produced as ruled rows in the market's voice. Every figure here comes
/// from the check or from the pot the member just saw; a row the check did not answer is left out
/// rather than guessed.
struct ProposeReviewView: View {
    let groupId: String
    let review: ProposeBuyReview
    let onProposed: (_ proposalId: String) -> Void

    private let service: ProposeService

    @Environment(AppSessionStore.self) private var session: AppSessionStore?

    @State private var isSending = false
    /// Idempotency key for the proposal being sent; a retry after a lost response reuses it.
    @State private var proposeSubmission = IdempotentSubmission()
    @State private var errorMessage: String?

    init(service: ProposeService, groupId: String, review: ProposeBuyReview, onProposed: @escaping (_ proposalId: String) -> Void) {
        self.service = service
        self.groupId = groupId
        self.review = review
        self.onProposed = onProposed
    }

    private var headline: String {
        ProposeScreenCopy.buyHeadline(amount: UsdAmountFormatter.format(micros: review.usdcMicros), ticker: review.stock.ticker)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                ProposeReceiptHeader(
                    symbol: review.stock.symbol,
                    name: review.stock.name,
                    logoURL: ProposeStockLogo.url(for: review.stock.symbol, in: session),
                    headline: headline
                )
                .padding(.horizontal, MonacoTheme.Space.m)
                .padding(.bottom, MonacoTheme.Space.l)

                MonacoGroupedList {
                    if let shares = review.sharesLabel {
                        ReceiptRow(label: ProposeScreenCopy.getsRow) {
                            ReceiptFigure(ProposeScreenCopy.about(shares))
                        }
                    }
                    if let issuer = review.quote.provider?.issuerName, !issuer.isEmpty {
                        ReceiptRow(label: "Provider") {
                            ReceiptFigure("Best price via \(issuer)")
                        }
                    }
                    if let price = review.priceMicros {
                        ReceiptRow(label: ProposeFlowCopy.priceRow) {
                            ReceiptFigure(ProposeScreenCopy.about(
                                review.stock.assetKind == .preIpo
                                    ? "\(UsdAmountFormatter.format(micros: price)) a \(PreIpoCopy.tokenLabelSingular)"
                                    : ProposeFlowCopy.perShare(UsdAmountFormatter.format(micros: price))
                            ))
                        }
                    }
                    if let share = ProposeScreenCopy.potShare(amountMicros: review.usdcMicros, potMicros: review.potMicros) {
                        ReceiptRow(label: ProposeScreenCopy.potRow) {
                            ReceiptFigure(share)
                        }
                    }
                    ReceiptRow(label: ProposeScreenCopy.whoVotesRow, isLast: true) {
                        ReceiptCabal(groupId: review.cabalId, name: review.cabalName)
                    }
                }

                if !review.thesis.isEmpty {
                    ReceiptReasonRow(title: ProposeScreenCopy.reasonTitle(isSell: false), text: review.thesis)
                        .padding(.horizontal, MonacoTheme.Space.m)
                        .padding(.top, MonacoTheme.Space.l)
                }

                if let errorMessage {
                    ReceiptError(message: errorMessage)
                        .padding(.horizontal, MonacoTheme.Space.m)
                        .padding(.top, MonacoTheme.Space.l)
                        .accessibilityIdentifier("proposal-submit-error")
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
                        if isSending {
                            ProgressView().tint(MonacoTheme.primaryButtonLabel)
                        }
                    }
                }
                .buttonStyle(.monacoPrimary)
                .disabled(isSending)
                .accessibilityIdentifier("proposal-submit-button")
            }
        }
        .accessibilityIdentifier("propose-review")
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
                draft: .buy(symbol: review.quote.symbol.isEmpty ? review.stock.symbol : review.quote.symbol, usdcMicros: review.usdcMicros, thesis: review.thesis),
                submission: proposeSubmission
            )
            onProposed(id)
        } catch {
            if error.isRequestCancellation { return }
            errorMessage = ProposeErrorCopy.propose(error, stockName: review.stock.name)
            Haptics.warning()
        }
    }
}

/// The top of a receipt: the stock's coin and ticker over its name, then what the cabal is asked
/// to agree to as one sentence in the brand's voice — "Buy $25.00 of AAPL".
struct ProposeReceiptHeader: View {
    let symbol: String
    let name: String
    var logoURL: URL?
    let headline: String

    private var ticker: String { AssetSymbolFormatter.display(symbol) }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            HStack(spacing: MonacoTheme.Space.sm) {
                StockMark(symbol: symbol, size: 44, logoURL: logoURL)
                VStack(alignment: .leading, spacing: 2) {
                    Text(ticker)
                        .font(MonacoTheme.Typo.ticker)
                        .foregroundStyle(MonacoTheme.ink)
                    if name != ticker {
                        Text(name)
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.muted)
                    }
                }
            }
            .accessibilityElement(children: .combine)

            Text(headline)
                .moneyFont(.large)
                .foregroundStyle(MonacoTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// One ruled line of a receipt: what it is on the left, the figure on the right. At the
/// accessibility sizes the figure drops under its label instead of squeezing it.
struct ReceiptRow<Value: View>: View {
    let label: String
    var isLast = false
    @ViewBuilder let value: Value

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        Group {
            if dynamicTypeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                    labelText
                    value
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            } else {
                HStack(spacing: MonacoTheme.Space.m) {
                    labelText
                    Spacer(minLength: MonacoTheme.Space.s)
                    value
                }
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, MonacoTheme.Space.sm)
        .frame(minHeight: 52)
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule().padding(.leading, MonacoTheme.Space.m)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var labelText: some View {
        Text(label)
            .font(MonacoTheme.Typo.callout)
            .foregroundStyle(MonacoTheme.muted)
    }
}

/// A receipt figure: the market's voice, ink, and never truncated — it wraps before it hides money.
struct ReceiptFigure: View {
    let text: String

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    init(_ text: String) {
        self.text = text
    }

    var body: some View {
        Text(text)
            .font(MonacoTheme.Typo.data)
            .foregroundStyle(MonacoTheme.ink)
            .multilineTextAlignment(ReceiptLayout.figureAlignment(dynamicTypeSize))
            .fixedSize(horizontal: false, vertical: true)
    }
}

/// Where a receipt figure's lines line up. Beside its label a figure reads from the right edge; once
/// the row stacks at the accessibility sizes it sits under the label, so a figure that wraps — "about
/// $231.40" over "a share" — reads from the left like the label does.
enum ReceiptLayout {
    static func figureAlignment(_ dynamicTypeSize: DynamicTypeSize) -> TextAlignment {
        MonacoRowLayout(dynamicTypeSize: dynamicTypeSize).isStacked ? .leading : .trailing
    }
}

/// The cabal that votes, by its mark and name. The picture comes from the session's list of the
/// member's cabals when it has one; the tinted initials otherwise.
struct ReceiptCabal: View {
    let groupId: String
    let name: String

    @Environment(AppSessionStore.self) private var session: AppSessionStore?
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var pictureUrl: String? {
        session?.joinedCabals.first { $0.groupId == groupId }?.pictureUrl
    }

    var body: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            CabalMark(groupId: groupId, name: name, size: 24, pictureUrl: pictureUrl)
            Text(name)
                .font(MonacoTheme.Typo.calloutStrong)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(2)
                .multilineTextAlignment(ReceiptLayout.figureAlignment(dynamicTypeSize))
        }
    }
}

/// The reason, quoted under the receipt's rows in full, under the header the proposal's own
/// screen gives it — the member wrote it for the cabal, and this is how the cabal will see it.
struct ReceiptReasonRow: View {
    let title: String
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            MonacoSectionHeader(title)
            // Its own element, not combined with the header: the quote is what VoiceOver and the
            // UI tests look for by its words.
            ProposalQuoteBlock(text: text)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Why sending failed, under the receipt, in the loss colour.
struct ReceiptError: View {
    let message: String

    var body: some View {
        Text(message)
            .font(MonacoTheme.Typo.callout)
            .foregroundStyle(MonacoTheme.loss)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Quoted text with a 3pt ink rule, used for a proposal's reason.
struct ProposalQuoteBlock: View {
    let text: String
    var lineLimit: Int?

    var body: some View {
        HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
            RoundedRectangle(cornerRadius: 1.5)
                .fill(MonacoTheme.ink)
                .frame(width: 3)
            Text(text)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(lineLimit)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .fixedSize(horizontal: false, vertical: true)
    }
}
