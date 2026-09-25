import MonacoCore
import SwiftUI

/// Receipt for one activity row: deposit, buy, or sell. Loads the detail, then renders `TransactionReceiptView`.
struct TransactionDetailView: View {
    @ObservedObject var auth: PrivyAuthService
    let activityItem: GroupActivityItemDTO
    let onRetry: ((GroupActivityItemDTO) -> Void)?
    let isRetrying: Bool

    private let apiClient = MonacoAPIClient()

    @State private var transaction: TransactionDetailDTO?
    @State private var deposit: GetDepositResponse?
    @State private var errorMessage: String?
    @State private var isLoading = true

    var body: some View {
        Group {
            if let receipt {
                TransactionReceiptView(
                    receipt: receipt,
                    isRetrying: isRetrying,
                    onRetry: canRetry ? { onRetry?(activityItem) } : nil
                )
            } else if let errorMessage {
                EmptyState(
                    title: errorMessage,
                    actionTitle: "Try again",
                    action: { Task { await loadDetail() } }
                )
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .accessibilityIdentifier("transaction-detail-error")
            } else {
                TransactionReceiptSkeleton()
            }
        }
        .monacoCanvas()
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: loadTaskID) {
            await loadDetail()
            await pollDepositDetailWhilePending()
        }
        // A buy or sell opened while it is still going through settles a few seconds later, and
        // the receipt is exactly where a member watches for that.
        .pollWhileVisible(every: LiveRefreshCadence.inPlay, isActive: isSwapStillGoingThrough) {
            guard let token = auth.accessToken else { return }
            let latest = try await apiClient.getTransactionDetail(accessToken: token, transactionId: activityItem.id)
            QuietUpdate.apply(latest, over: transaction) { transaction = $0 }
        }
        .refreshable {
            await loadDetail(showLoadingIndicator: false)
        }
    }

    private var receipt: TransactionReceipt? {
        if let deposit { return TransactionReceipt(deposit: deposit) }
        if let transaction { return TransactionReceipt(transaction: transaction) }
        return nil
    }

    private var loadTaskID: String {
        "\(activityItem.id)-\(activityItem.kind)-\(auth.accessToken ?? "")"
    }

    private var isDeposit: Bool {
        activityItem.kind.lowercased() == "deposit"
    }

    private var canRetry: Bool {
        onRetry != nil && GroupActivityRules.canRetry(activityItem)
    }

    /// A swap the receipt is showing as still on its way. Deposits have their own state machine.
    private var isSwapStillGoingThrough: Bool {
        guard !isDeposit else { return false }
        guard let transaction else { return DepositStatusNormalizer.isPending(activityItem.status) }
        return DepositStatusNormalizer.isPending(transaction.status)
    }

    private func loadDetail(showLoadingIndicator: Bool = true) async {
        guard let token = auth.accessToken else {
            isLoading = false
            errorMessage = "Sign in again to see this."
            return
        }

        if showLoadingIndicator { isLoading = true }
        errorMessage = nil
        defer {
            if showLoadingIndicator { isLoading = false }
        }

        do {
            if isDeposit {
                deposit = try await apiClient.getDeposit(accessToken: token, depositId: activityItem.id)
            } else {
                let latest = try await apiClient.getTransactionDetail(accessToken: token, transactionId: activityItem.id)
                QuietUpdate.apply(latest, over: transaction) { transaction = $0 }
            }
        } catch is CancellationError {
            return
        } catch {
            if error.isRequestCancellation { return }
            // A failed re-read leaves the receipt the member is reading exactly as it was.
            guard receipt == nil else { return }
            errorMessage = "Couldn't load this receipt"
        }
    }

    private func pollDepositDetailWhilePending() async {
        guard isDeposit else { return }
        guard let token = auth.accessToken else { return }
        var machine = DepositPollStateMachine()
        if let status = deposit?.status {
            machine.apply(status: status)
        } else if DepositStatusNormalizer.isPending(activityItem.status) {
            machine.apply(status: activityItem.status)
        }
        guard !machine.isTerminal else { return }

        _ = await machine.pollUntilTerminal {
            let latest = try await apiClient.getDeposit(accessToken: token, depositId: activityItem.id)
            deposit = latest
            return latest.status
        }
    }
}

/// Everything a receipt shows, derived from the deposit or swap DTO. No ids, no mints.
struct TransactionReceipt: Equatable {
    /// A fact under the receipt's header. The label says what it is ("Shares"), so the value
    /// is only the figure ("1.0803").
    struct Row: Equatable, Identifiable {
        let label: String
        let value: String
        var id: String { label }
    }

    enum Status: Equatable { case confirmed, pending, failed, other(String) }

    let glyph: String
    let headline: String
    /// Hero figure in USDC micros; nil when there's no dollar amount yet (pending sell).
    let amountMicros: Int64?
    /// Shown in place of the hero figure when there's no dollar amount.
    let fallbackHero: String?
    let status: Status
    let rows: [Row]
    let failureMessage: String?
    let signature: String?

    init(deposit: GetDepositResponse) {
        glyph = "plus"
        status = Self.status(deposit.status)
        // The same words the activity row uses, so a deposit still on its way into the pot is
        // not headed "Money added" above a Pending chip.
        headline = switch status {
        case .confirmed: "Money added"
        case .failed: "Couldn't add money"
        default: "Adding money"
        }
        amountMicros = deposit.amount
        fallbackHero = nil
        rows = [Row(label: "Date", value: Self.date(deposit.createdAt))]
        failureMessage = status == .failed ? "The transfer didn't go through" : nil
        signature = deposit.txSignature
    }

    init(transaction: TransactionDetailDTO) {
        let action = transaction.action.lowercased()
        status = Self.status(transaction.status)
        signature = transaction.txSignature
        let dateRow = Row(label: "Date", value: Self.date(transaction.confirmedAt ?? transaction.createdAt))

        switch action {
        case "buy":
            let name = Self.stockName(transaction.outputSymbol)
            glyph = "arrow.down"
            headline = switch status {
            case .confirmed: "Bought \(name)"
            case .failed: "Couldn't buy \(name)"
            default: "Buying \(name)"
            }
            amountMicros = transaction.amountMicros
            fallbackHero = nil
            var rows: [Row] = []
            if let atomics = transaction.costBasisAmount, atomics > 0,
               let quantity = TokenQuantityFormatter.quantity(fromAtomics: String(atomics), decimals: transaction.resolvedTokenDecimals),
               quantity > 0 {
                let isToken = transaction.resolvedAssetKind == .preIpo
                rows.append(Row(label: isToken ? PreIpoCopy.tokensRowLabel : "Shares", value: Self.quantityFigure(quantity)))
                if let spent = transaction.costBasisPrice, spent > 0 {
                    rows.append(Row(
                        label: isToken ? "Price a \(PreIpoCopy.tokenLabelSingular)" : "Price a share",
                        value: UsdAmountFormatter.format(micros: Self.perUnitMicros(spent, quantity: quantity))
                    ))
                }
            }
            rows.append(dateRow)
            self.rows = rows
        case "sell":
            let name = Self.stockName(transaction.inputSymbol)
            glyph = "arrow.up"
            headline = switch status {
            case .confirmed: "Sold \(name)"
            case .failed: "Couldn't sell \(name)"
            default: "Selling \(name)"
            }
            let proceeds = transaction.proceedsUsdcMicros ?? transaction.costBasisAmount
            let sold = String(transaction.amountMicros)
            let quantity = TokenQuantityFormatter.quantity(fromAtomics: sold, decimals: transaction.resolvedTokenDecimals) ?? 0
            let isToken = transaction.resolvedAssetKind == .preIpo
            if let proceeds, proceeds > 0 {
                amountMicros = proceeds
                fallbackHero = nil
            } else {
                amountMicros = nil
                fallbackHero = TokenQuantityFormatter.label(fromAtomics: sold, decimals: transaction.resolvedTokenDecimals, kind: transaction.resolvedAssetKind)
            }
            var rows: [Row] = []
            // When the hero already shows the count, don't repeat it as a row.
            if quantity > 0, fallbackHero == nil {
                rows.append(Row(label: isToken ? PreIpoCopy.tokensRowLabel : "Shares", value: Self.quantityFigure(quantity)))
                if let proceeds, proceeds > 0 {
                    rows.append(Row(
                        label: isToken ? "Price a \(PreIpoCopy.tokenLabelSingular)" : "Price a share",
                        value: UsdAmountFormatter.format(micros: Self.perUnitMicros(proceeds, quantity: quantity))
                    ))
                }
            }
            rows.append(dateRow)
            self.rows = rows
        default:
            glyph = "circle"
            headline = transaction.action.capitalized
            amountMicros = transaction.amountMicros
            fallbackHero = nil
            rows = [dateRow]
        }
        failureMessage = status == .failed ? "It didn't go through. Nothing left the pot." : nil
    }

    /// Solscan only for real signatures (base58); seeded rows carry placeholders.
    var solscanURL: URL? {
        guard let signature, !signature.isEmpty,
              signature.allSatisfy({ $0.isLetter || $0.isNumber }) else { return nil }
        return URL(string: "https://solscan.io/tx/\(signature)")
    }

    var statusLabel: String {
        switch status {
        case .confirmed: "Confirmed"
        case .pending: "Pending"
        case .failed: "Failed"
        case .other(let raw): raw.capitalized
        }
    }

    /// "1.0803": up to four decimals, trailing zeros dropped, no unit — the row is labelled.
    /// A count of shares or tokens with the token's own decimals, trimmed like `sharesFigure`.
    static func quantityFigure(_ quantity: Decimal) -> String {
        sharesFigure(NSDecimalNumber(decimal: quantity).doubleValue)
    }

    /// What one share or token cost, from the money and the count, rounded to the micro.
    static func perUnitMicros(_ micros: Int64, quantity: Decimal) -> Int64 {
        var source = Decimal(micros) / quantity
        var rounded = Decimal()
        NSDecimalRound(&rounded, &source, 0, .plain)
        return NSDecimalNumber(decimal: rounded).int64Value
    }

    static func sharesFigure(_ shares: Double) -> String {
        var figure = String(format: "%.4f", shares)
        while figure.hasSuffix("0") { figure.removeLast() }
        if figure.hasSuffix(".") { figure.removeLast() }
        return figure
    }

    private static func status(_ raw: String) -> Status {
        if DepositStatusNormalizer.isConfirmed(raw) { return .confirmed }
        if DepositStatusNormalizer.isPending(raw) { return .pending }
        if DepositStatusNormalizer.isFailed(raw) { return .failed }
        return .other(raw)
    }

    private static func stockName(_ symbol: String?) -> String {
        guard let symbol, !symbol.isEmpty else { return "stock" }
        return AssetDisplayNames.name(forSymbol: symbol) ?? AssetSymbolFormatter.display(symbol)
    }

    /// Local time on display; the API stores UTC.
    private static func date(_ raw: String) -> String {
        guard let date = GroupActivityRules.parseDate(raw) else { return raw }
        return date.formatted(date: .abbreviated, time: .shortened)
    }
}

/// A receipt as a ledger entry: what happened and for how much, its state, then the facts as
/// ruled lines and the way to check it on Solscan.
///
/// Set on the paper and left-aligned to the margin the lines under it share. The amount is in
/// Avenir because it is the cabal's own money; every fact under it is in the market's voice.
struct TransactionReceiptView: View {
    let receipt: TransactionReceipt
    var isRetrying = false
    var onRetry: (() -> Void)?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                header
                    .padding(.horizontal, MonacoTheme.Space.m)

                MonacoGroupedList {
                    ForEach(receipt.rows) { row in
                        ReceiptLine(
                            label: row.label,
                            value: .data(row.value),
                            isLast: row.id == receipt.rows.last?.id && receipt.solscanURL == nil
                        )
                    }
                    if let url = receipt.solscanURL {
                        solscanLine(url)
                    }
                }

                // A failed receipt keeps its way to try again, under the facts it is about.
                if let onRetry {
                    retry(onRetry)
                        .padding(.horizontal, MonacoTheme.Space.m)
                }
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .accessibilityIdentifier("transaction-receipt")
    }

    /// The same glyph as the activity row it was opened from, then the headline, the amount
    /// and its state — read by VoiceOver as one sentence.
    private var header: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            Image(systemName: receipt.glyph)
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(MonacoTheme.ink)
                .frame(width: 44, height: 44)
                .background(Circle().fill(MonacoTheme.surfaceSunken))
                .accessibilityHidden(true)

            VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                Text(receipt.headline)
                    .font(MonacoTheme.Typo.title)
                    .foregroundStyle(MonacoTheme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                amount
            }

            ReceiptStatusChip(status: receipt.status, label: receipt.statusLabel)
                .accessibilityIdentifier("transaction-detail-status")

            if let failure = receipt.failureMessage {
                Text(failure)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    private var amount: some View {
        Group {
            if let micros = receipt.amountMicros {
                MoneyText(micros: micros, style: .hero)
            } else {
                Text(receipt.fallbackHero ?? "—")
                    .moneyFont(.hero)
                    .foregroundStyle(MonacoTheme.ink)
            }
        }
        .lineLimit(1)
        .minimumScaleFactor(0.6)
        .dynamicTypeSize(...DynamicTypeSize.accessibility2)
    }

    private func solscanLine(_ url: URL) -> some View {
        Link(destination: url) {
            HStack(spacing: MonacoTheme.Space.sm) {
                Text("View on Solscan")
                    .font(MonacoTheme.Typo.bodyStrong)
                    .foregroundStyle(MonacoTheme.ink)
                Spacer(minLength: MonacoTheme.Space.sm)
                Image(systemName: "arrow.up.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(MonacoTheme.tertiaryText)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            .frame(minHeight: 52)
            .contentShape(Rectangle())
        }
        .buttonStyle(.monacoRow)
        .accessibilityIdentifier("transaction-detail-solscan")
    }

    @ViewBuilder
    private func retry(_ action: @escaping () -> Void) -> some View {
        if isRetrying {
            ProgressView()
                .tint(MonacoTheme.ink)
                .frame(maxWidth: .infinity, minHeight: MonacoButtonMetrics.minimumHeight)
                .accessibilityIdentifier("transaction-detail-retry-loading")
        } else {
            Button("Try again", action: action)
                .buttonStyle(.monacoPrimary)
                .monacoFullWidthButtons()
                .accessibilityIdentifier("transaction-detail-retry")
        }
    }
}

/// One line of a receipt: what it is on the left in the brand's voice, the value on the right.
/// Figures, dates and addresses set in the market's voice; words stay in the brand's. An
/// address — and any line at the accessibility text sizes — puts its value under the label
/// instead of squeezing either.
struct ReceiptLine: View {
    enum Value: Equatable {
        /// A figure or a date, in SF Mono.
        case data(String)
        /// A word or a phrase, in Avenir Next.
        case words(String)
        /// An address, in SF Mono, wrapped by character and never hyphenated.
        case address(String)
    }

    let label: String
    let value: Value
    var isLast = false

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var isStacked: Bool {
        if case .address = value { return true }
        return dynamicTypeSize.isAccessibilitySize
    }

    var body: some View {
        Group {
            if isStacked {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                    labelText
                    valueView(alignment: .leading)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            } else {
                HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.sm) {
                    labelText
                    Spacer(minLength: MonacoTheme.Space.sm)
                    valueView(alignment: .trailing)
                }
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, MonacoTheme.Space.sm)
        .frame(minHeight: 52)
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule()
                    .padding(.leading, MonacoTheme.Space.m)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var labelText: some View {
        Text(label)
            .font(MonacoTheme.Typo.body)
            .foregroundStyle(MonacoTheme.muted)
    }

    @ViewBuilder
    private func valueView(alignment: TextAlignment) -> some View {
        switch value {
        case .data(let text):
            Text(text)
                .font(MonacoTheme.Typo.data)
                .foregroundStyle(MonacoTheme.ink)
                .multilineTextAlignment(alignment)
        case .words(let text):
            Text(text)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.ink)
                .multilineTextAlignment(alignment)
        case .address(let address):
            MonacoWalletAddressText(address: address, textStyle: .subheadline)
        }
    }
}

/// A receipt's state as a chip: amber while it is on its way, the loss red when it failed,
/// muted once it is done — done is the normal case and should not shout.
struct ReceiptStatusChip: View {
    let status: TransactionReceipt.Status
    let label: String

    var body: some View {
        Text(label)
            .font(MonacoTheme.Typo.micro)
            .foregroundStyle(tint)
            .lineLimit(1)
            .padding(.horizontal, 10)
            .padding(.vertical, 5)
            .background(Capsule().fill(fill))
            .fixedSize()
    }

    private var tint: Color {
        switch status {
        case .pending: MonacoTheme.warning
        case .failed: MonacoTheme.lossOnWash
        case .confirmed, .other: MonacoTheme.muted
        }
    }

    private var fill: Color {
        status == .failed ? MonacoTheme.lossWash : MonacoTheme.surfaceSunken
    }
}

/// A receipt before its detail has loaded, in the receipt's own shape: the glyph, the headline,
/// the amount, the chip, and three ruled lines.
struct TransactionReceiptSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                SkeletonBlock(width: 44, height: 44, radius: 22)
                SkeletonBlock(width: 180, height: 24)
                SkeletonBlock(width: 160, height: 44)
                SkeletonBlock(width: 76, height: 22, radius: 11)
            }
            .padding(.horizontal, MonacoTheme.Space.m)

            MonacoGroupedList {
                ForEach(0..<3, id: \.self) { index in
                    HStack(spacing: MonacoTheme.Space.sm) {
                        SkeletonBlock(width: 72, height: 14)
                        Spacer(minLength: MonacoTheme.Space.sm)
                        SkeletonBlock(width: 120, height: 14)
                    }
                    .padding(.horizontal, MonacoTheme.Space.m)
                    .frame(minHeight: 52)
                    .overlay(alignment: .bottom) {
                        if index < 2 {
                            MonacoRule()
                                .padding(.leading, MonacoTheme.Space.m)
                        }
                    }
                }
            }
        }
        .padding(.top, MonacoTheme.Space.m)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
        .accessibilityIdentifier("transaction-detail-loading")
    }
}
