import MonacoCore
import SwiftUI

/// Buy, step 3 of 3: a receipt of what the cabal will vote on, then "Send to cabal".
///
/// **v3.** The receipt is on ink — the figure, the stock and the cabal as one dark object at the
/// top of the screen, with the checkable detail on paper below it. It is the last thing a member
/// sees before they ask four friends to spend the pot's money, so it reads as a statement rather
/// than as a form they are still filling in.
struct ProposeReviewView: View {
    let groupId: String
    let review: ProposeBuyReview
    let onProposed: (_ proposalId: String) -> Void

    private let service: ProposeService

    @State private var isSending = false
    @State private var errorMessage: String?

    /// The viewer's cabals, for the resolved tint.
    @Environment(AppSessionStore.self) private var session: AppSessionStore?

    init(service: ProposeService, groupId: String, review: ProposeBuyReview, onProposed: @escaping (_ proposalId: String) -> Void) {
        self.service = service
        self.groupId = groupId
        self.review = review
        self.onProposed = onProposed
    }

    /// Resolved against the viewer's cabals, so the wash behind the reason on this receipt is the
    /// colour the cabal wears everywhere else.
    private var tint: MonacoTheme.CabalTint {
        ProposalCabalTint.tint(forGroupId: review.cabalId, in: session)
    }

    var body: some View {
        ScrollView {
            VStack(spacing: MonacoTheme.Space.section) {
                ProposeInkReceipt(
                    symbol: review.stock.symbol,
                    eyebrow: ProposeFlowCopy.youreProposing,
                    stockName: review.stock.name,
                    cabalId: review.cabalId,
                    cabalName: review.cabalName
                ) {
                    MoneyText(micros: review.usdcMicros, style: .hero, color: MonacoTheme.Ink.fgPrimary)
                }
                .accessibilityIdentifier("propose-review-receipt")

                voters

                if hasReceiptRows {
                    MonacoGroupedList {
                        if let price = review.priceMicros {
                            ReceiptRow(label: ProposeFlowCopy.priceRow, isLast: isLastRow(.price)) {
                                Text(ProposeFlowCopy.perShare(UsdAmountFormatter.format(micros: price)))
                                    .font(MonacoTheme.Typo.body.monospacedDigit())
                            }
                        }
                        if let shares = review.sharesLabel {
                            ReceiptRow(label: ProposeFlowCopy.sharesRow, isLast: isLastRow(.shares)) {
                                Text(ProposeFlowCopy.aboutShares(shares))
                                    .font(MonacoTheme.Typo.body.monospacedDigit())
                            }
                        }
                        if !review.thesis.isEmpty {
                            ReceiptReasonRow(text: review.thesis, tint: tint)
                        }
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

    /// The receipt rows that have something to say, in the order they are drawn.
    ///
    /// A quote without an output amount and a member who wrote no reason leaves one row, and the
    /// hairline under the last row has to come off whichever row that turns out to be — otherwise
    /// the card ends on a rule with nothing beneath it. With nothing at all to say there is no
    /// list: an empty grouped card on a receipt reads as a row that failed to load.
    private enum ReceiptRowKind {
        case price, shares, reason
    }

    private var receiptRows: [ReceiptRowKind] {
        var rows: [ReceiptRowKind] = []
        if review.priceMicros != nil { rows.append(.price) }
        if review.sharesLabel != nil { rows.append(.shares) }
        if !review.thesis.isEmpty { rows.append(.reason) }
        return rows
    }

    private var hasReceiptRows: Bool { !receiptRows.isEmpty }

    private func isLastRow(_ kind: ReceiptRowKind) -> Bool {
        receiptRows.last == kind
    }

    /// Who is about to be asked.
    ///
    /// The faces are the cabal's member list — `userId`, `displayName`, `profilePhotoUrl`, all of
    /// it already on the group payload — so this is who will vote, not a picture of a crowd. The
    /// pass rule is **not** stated: `GroupViewDTO` carries no vote threshold, and "3 of 5 yes to
    /// pass" guessed at is a lie about how someone's money gets spent. The count is true; the
    /// sentence the design wants needs `voteThreshold` on the group.
    @ViewBuilder
    private var voters: some View {
        if !review.cabalMembers.isEmpty {
            HStack(spacing: MonacoTheme.Space.sm) {
                MonacoFaceStack(faces: review.cabalMembers.map(\.face), size: 28, maxVisible: 5)
                Text(ProposeReviewCopy.willBeAsked(review.cabalMembers.count))
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.fgMuted)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 0)
            }
            .padding(MonacoTheme.Space.m)
            .monacoElevation(.card)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(ProposeReviewCopy.willBeAsked(review.cabalMembers.count))
            .accessibilityIdentifier("propose-review-voters")
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
                draft: .buy(symbol: review.stock.symbol, usdcMicros: review.usdcMicros, thesis: review.thesis)
            )
            onProposed(id)
        } catch {
            if error.isRequestCancellation { return }
            errorMessage = ProposeErrorCopy.propose(error, stockName: review.stock.name)
            Haptics.warning()
        }
    }
}

/// The ink receipt both propose flows end on: the stock, the figure, and whose pot it comes from,
/// as one dark object at the top of the screen.
///
/// Buy and sell shared a centred `VStack` on paper before v3, which is to say they shared nothing
/// a member would notice. This is the shape Deposit, Withdraw and Propose are meant to have in
/// common — a figure on ink, the checkable detail on paper below it — so the product has one way
/// of saying "here is what is about to happen to your money".
struct ProposeInkReceipt<Amount: View>: View {
    let symbol: String
    let eyebrow: String
    let stockName: String
    var detail: String?
    var cabalId: String?
    var cabalName: String?
    @ViewBuilder let amount: Amount

    var body: some View {
        VStack(spacing: MonacoTheme.Space.m) {
            StockMark(symbol: symbol, size: 56)

            VStack(spacing: MonacoTheme.Space.xs) {
                Text(eyebrow)
                    .displayFont(.eyebrow)
                    .foregroundStyle(MonacoTheme.Ink.fgSubtle)
                amount
                    .dynamicTypeSize(...DynamicTypeSize.accessibility2)
                Text(ProposeFlowCopy.ofStock(stockName))
                    .displayFont(.title)
                    .foregroundStyle(MonacoTheme.Ink.fgPrimary)
                    .multilineTextAlignment(.center)
                if let detail {
                    Text(detail)
                        .font(MonacoTheme.Typo.callout.monospacedDigit())
                        .foregroundStyle(MonacoTheme.Ink.fgMuted)
                }
            }

            // The cabal whose pot this comes out of, in its own colour. `onInk` brightens the
            // mark so the identity survives the dark band.
            if let cabalId, let cabalName, !cabalName.isEmpty {
                HStack(spacing: MonacoTheme.Space.s) {
                    CabalMark(groupId: cabalId, name: cabalName, size: 24, onInk: true)
                    Text(cabalName)
                        .font(MonacoTheme.Typo.callout.weight(.semibold))
                        .foregroundStyle(MonacoTheme.Ink.fgPrimary)
                        .lineLimit(1)
                }
                .padding(.horizontal, MonacoTheme.Space.sm)
                .padding(.vertical, MonacoTheme.Space.s)
                .background(Capsule().fill(MonacoTheme.Ink.line))
            }
        }
        .frame(maxWidth: .infinity)
        .monacoInkBand()
        // `StockMark` is built from the paper ramp and this chunk does not own it: resolved dark
        // it is a quiet tile on the band, and resolved light it is a white rectangle with type
        // the same colour as the ink behind it.
        .monacoInkScheme()
        .accessibilityElement(children: .combine)
    }
}

/// Copy the receipt needs that `ProposeFlowCopy` does not carry yet.
enum ProposeReviewCopy {
    static func willBeAsked(_ count: Int) -> String {
        count == 1 ? "1 member will be asked to vote" : "\(count) members will be asked to vote"
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
                .foregroundStyle(MonacoTheme.fgMuted)
            Spacer(minLength: MonacoTheme.Space.s)
            value
                .foregroundStyle(MonacoTheme.fgPrimary)
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .frame(minHeight: 52)
        .overlay(alignment: .bottom) {
            if !isLast {
                Rectangle().fill(MonacoTheme.line).frame(height: 1).padding(.leading, MonacoTheme.Space.m)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// The reason, quoted, as the last receipt row.
struct ReceiptReasonRow: View {
    let text: String
    var tint: MonacoTheme.CabalTint?

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text(ProposeFlowCopy.reasonRow)
                .displayFont(.eyebrow)
                .foregroundStyle(MonacoTheme.fgMuted)
            ProposalQuoteBlock(text: text, lineLimit: 4, tint: tint)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(MonacoTheme.Space.m)
        .accessibilityElement(children: .combine)
    }
}

/// A proposal's reason, quoted in the proposing cabal's wash behind a 2pt rail in its colour.
///
/// The wash is the one place a cabal's colour touches the *content* of a proposal, and it is the
/// reason a feed of six proposals from three cabals reads as three rooms rather than one list.
/// With no cabal to name it falls back to `inkWash`, which is neutral in both schemes.
struct ProposalQuoteBlock: View {
    let text: String
    var lineLimit: Int?
    var tint: MonacoTheme.CabalTint?

    private var rail: Color { tint?.fill ?? MonacoTheme.fgPrimary }

    private var wash: Color { tint?.soft ?? MonacoTheme.inkWash }

    var body: some View {
        HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
            RoundedRectangle(cornerRadius: 1)
                .fill(rail)
                .frame(width: 2)
            Text(text)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.fgPrimary)
                .lineLimit(lineLimit)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(MonacoTheme.Space.sm)
        .background(wash, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous))
        .fixedSize(horizontal: false, vertical: true)
    }
}
