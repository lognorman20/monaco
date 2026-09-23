import MonacoCore
import SwiftUI

/// Buy, step 3 of 3: a receipt of what the cabal will vote on, then "Send to cabal".
struct ProposeReviewView: View {
    let groupId: String
    let review: ProposeBuyReview
    let onProposed: (_ proposalId: String) -> Void

    private let service: ProposeService

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

    var body: some View {
        ScrollView {
            VStack(spacing: MonacoTheme.Space.xl) {
                ProposeReceiptHeader(
                    caption: ProposeFlowCopy.youreProposing,
                    amount: MoneyText(micros: review.usdcMicros, style: .hero),
                    stockName: review.stock.name
                )

                MonacoGroupedList {
                    if let price = review.priceMicros {
                        ReceiptRow(label: ProposeFlowCopy.priceRow) {
                            Text(
                                review.stock.assetKind == .preIpo
                                    ? "\(UsdAmountFormatter.format(micros: price)) a \(PreIpoCopy.tokenLabelSingular)"
                                    : ProposeFlowCopy.perShare(UsdAmountFormatter.format(micros: price))
                            )
                                .font(MonacoTheme.Typo.body.monospacedDigit())
                        }
                    }
                    if let shares = review.sharesLabel {
                        let rowLabel = review.stock.assetKind == .preIpo ? PreIpoCopy.tokensRowLabel : ProposeFlowCopy.sharesRow
                        ReceiptRow(label: rowLabel) {
                            Text(ProposeFlowCopy.aboutShares(shares))
                                .font(MonacoTheme.Typo.body.monospacedDigit())
                        }
                    }
                    ReceiptRow(label: ProposeFlowCopy.cabalRow, isLast: review.thesis.isEmpty) {
                        HStack(spacing: MonacoTheme.Space.s) {
                            CabalMark(groupId: review.cabalId, name: review.cabalName, size: 28)
                            Text(review.cabalName)
                                .font(MonacoTheme.Typo.body)
                                .lineLimit(1)
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
                        .accessibilityIdentifier("proposal-submit-error")
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.l)
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
                draft: .buy(symbol: review.stock.symbol, usdcMicros: review.usdcMicros, thesis: review.thesis),
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

/// "You're proposing" / big amount / "of Apple", centred.
struct ProposeReceiptHeader<Amount: View>: View {
    let caption: String
    let amount: Amount
    let stockName: String
    var detail: String?

    var body: some View {
        VStack(spacing: MonacoTheme.Space.s) {
            Text(caption)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.muted)
            amount
                .dynamicTypeSize(...DynamicTypeSize.accessibility2)
            Text(ProposeFlowCopy.ofStock(stockName))
                .font(MonacoTheme.Typo.title)
                .foregroundStyle(MonacoTheme.ink)
                .multilineTextAlignment(.center)
            if let detail {
                Text(detail)
                    .font(MonacoTheme.Typo.callout.monospacedDigit())
                    .foregroundStyle(MonacoTheme.muted)
            }
        }
        .frame(maxWidth: .infinity)
        .accessibilityElement(children: .combine)
    }
}

/// Label on the left, value on the right, hairline below.
struct ReceiptRow<Value: View>: View {
    let label: String
    var isLast = false
    @ViewBuilder let value: Value

    var body: some View {
        HStack(spacing: MonacoTheme.Space.m) {
            Text(label)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.muted)
            Spacer(minLength: MonacoTheme.Space.s)
            value
                .foregroundStyle(MonacoTheme.ink)
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .frame(minHeight: 52)
        .overlay(alignment: .bottom) {
            if !isLast {
                Rectangle().fill(MonacoTheme.hairline).frame(height: 1).padding(.leading, MonacoTheme.Space.m)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// The reason, quoted, as the last receipt row.
struct ReceiptReasonRow: View {
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text(ProposeFlowCopy.reasonRow)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.muted)
            ProposalQuoteBlock(text: text, lineLimit: 4)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(MonacoTheme.Space.m)
        .accessibilityElement(children: .combine)
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
