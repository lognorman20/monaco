import MonacoCore
import SwiftUI

/// Receipt for one activity row: deposit, buy, or sell. Loads the detail, then renders `TransactionReceiptView`.
struct TransactionDetailView: View {
    @ObservedObject var auth: DynamicAuthService
    let activityItem: GroupActivityItemDTO
    let onRetry: ((GroupActivityItemDTO) async -> RetryTransactionResponse?)?
    let isRetrying: Bool

    private let apiClient = MonacoAPIClient()

    @State private var transaction: TransactionDetailDTO?
    @State private var deposit: GetDepositResponse?
    @State private var errorMessage: String?
    @State private var isLoading = true
    @State private var loadedTransactionId = ""

    var body: some View {
        Group {
            if let receipt {
                TransactionReceiptView(
                    receipt: receipt,
                    isRetrying: isRetrying,
                    onRetry: canRetry ? { Task { await performRetry() } } : nil
                )
            } else if let errorMessage {
                VStack(spacing: 16) {
                    Text(errorMessage)
                        .font(.body)
                        .foregroundStyle(MonacoTheme.muted)
                        .multilineTextAlignment(.center)
                    Button("Try again") {
                        Task { await loadDetail() }
                    }
                    .buttonStyle(.monacoSecondary)
                }
                .padding(24)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .accessibilityIdentifier("transaction-detail-error")
            } else {
                ProgressView()
                    .tint(MonacoTheme.ink)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .monacoCanvas()
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: loadTaskID) {
            await loadDetail()
        }
        // A buy or sell opened while it is still going through settles a few seconds later, and
        // the receipt is exactly where a member watches for that.
        .pollWhileVisible(every: LiveRefreshCadence.inPlay, isActive: isSwapStillGoingThrough) {
            guard let token = auth.accessToken else { return }
            let latest = try await apiClient.getTransactionDetail(accessToken: token, transactionId: detailTransactionId)
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
        // Keyed on who is signed in, not on the token, so a token rotation does not reload
        // the receipt.
        "\(detailTransactionId)-\(activityItem.kind)-\(auth.sessionIdentity ?? "")"
    }

    private var detailTransactionId: String {
        loadedTransactionId.isEmpty ? activityItem.id : loadedTransactionId
    }

    private var isDeposit: Bool {
        activityItem.kind.lowercased() == "deposit"
    }

    private var canRetry: Bool {
        guard onRetry != nil else { return false }
        if let transaction {
            return transaction.status.lowercased() == "failed"
                && ["buy", "sell"].contains(transaction.action.lowercased())
        }
        return GroupActivityRules.canRetry(activityItem)
    }

    /// A swap the receipt is showing as still on its way. Deposits are read once here.
    private var isSwapStillGoingThrough: Bool {
        guard !isDeposit else { return false }
        let status = transaction?.status ?? activityItem.status
        return GroupDetailCadence.isStillGoingThrough(status: status)
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
                let latest = try await apiClient.getTransactionDetail(accessToken: token, transactionId: detailTransactionId)
                QuietUpdate.apply(latest, over: transaction) { transaction = $0 }
            }
        } catch is CancellationError {
            return
        } catch {
            if error.isRequestCancellation { return }
            // A failed re-read leaves the receipt the member is reading exactly as it was.
            guard receipt == nil else { return }
            errorMessage = "Couldn't load this. Try again"
        }
    }

    private func performRetry() async {
        guard let onRetry else { return }
        if let result = await onRetry(activityItem) {
            loadedTransactionId = result.transactionId
            await loadDetail()
        }
    }
}

/// Everything a receipt shows, derived from the deposit or swap DTO. No ids, no mints.
struct TransactionReceipt: Equatable {
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

    private static let atomicsPerShare = 100_000_000.0

    init(deposit: GetDepositResponse) {
        glyph = "plus"
        headline = "Money added"
        amountMicros = deposit.amount
        fallbackHero = nil
        status = Self.status(deposit.status)
        rows = [Row(label: "Date", value: Self.date(deposit.createdAt))]
        failureMessage = status == .failed ? "The transfer didn't go through" : nil
        signature = deposit.txHash
    }

    init(transaction: TransactionDetailDTO) {
        let action = transaction.action.lowercased()
        status = Self.status(transaction.status)
        signature = transaction.txHash
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
            if let atomics = transaction.costBasisAmount, atomics > 0 {
                let shares = Double(atomics) / Self.atomicsPerShare
                rows.append(Row(label: "Shares", value: GroupActivityRules.sharesLabel(shares)))
                if let spent = transaction.costBasisPrice, spent > 0 {
                    let perShare = Int64((Double(spent) / shares).rounded())
                    rows.append(Row(label: "Price", value: "\(UsdAmountFormatter.format(micros: perShare)) a share"))
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
            let shares = Double(transaction.amountMicros) / Self.atomicsPerShare
            if let proceeds, proceeds > 0 {
                amountMicros = proceeds
                fallbackHero = nil
            } else {
                amountMicros = nil
                fallbackHero = GroupActivityRules.sharesLabel(shares)
            }
            var rows: [Row] = []
            // When the hero already shows shares, don't repeat them as a row.
            if shares > 0, fallbackHero == nil {
                rows.append(Row(label: "Shares", value: GroupActivityRules.sharesLabel(shares)))
                if let proceeds, proceeds > 0 {
                    let perShare = Int64((Double(proceeds) / shares).rounded())
                    rows.append(Row(label: "Price", value: "\(UsdAmountFormatter.format(micros: perShare)) a share"))
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

    var statusLabel: String {
        switch status {
        case .confirmed: "Confirmed"
        case .pending: "Pending"
        case .failed: "Failed"
        case .other(let raw): raw.capitalized
        }
    }

    private static func status(_ raw: String) -> Status {
        switch raw.lowercased() {
        case "confirmed": .confirmed
        case "pending": .pending
        case "failed": .failed
        default: .other(raw)
        }
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

/// Receipt layout: glyph, what happened, the amount, status, a few facts.
struct TransactionReceiptView: View {
    let receipt: TransactionReceipt
    var isRetrying = false
    var onRetry: (() -> Void)?

    var body: some View {
        ScrollView {
            VStack(spacing: 28) {
                VStack(spacing: 12) {
                    Image(systemName: receipt.glyph)
                        .font(.system(size: 20, weight: .semibold))
                        .foregroundStyle(MonacoTheme.ink)
                        .frame(width: 56, height: 56)
                        .background(Circle().fill(MonacoTheme.surfaceSunken))
                        .accessibilityHidden(true)
                    Text(receipt.headline)
                        .font(MonacoTheme.Typo.title)
                        .foregroundStyle(MonacoTheme.ink)
                        .multilineTextAlignment(.center)
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
                    Text(receipt.statusLabel)
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(statusColor)
                        .accessibilityIdentifier("transaction-detail-status")
                }
                .frame(maxWidth: .infinity)
                .accessibilityElement(children: .combine)

                if let failure = receipt.failureMessage {
                    Text(failure)
                        .font(.subheadline)
                        .foregroundStyle(MonacoTheme.muted)
                        .multilineTextAlignment(.center)
                        .frame(maxWidth: .infinity)
                }

                MonacoGroupedList {
                    ForEach(receipt.rows) { row in
                        HStack {
                            Text(row.label)
                                .foregroundStyle(MonacoTheme.muted)
                            Spacer(minLength: 12)
                            Text(row.value)
                                .fontWeight(.semibold)
                                .monospacedDigit()
                                .foregroundStyle(MonacoTheme.ink)
                                .multilineTextAlignment(.trailing)
                        }
                        .font(MonacoTheme.Typo.body)
                        .padding(.horizontal, MonacoTheme.Space.m)
                        .frame(minHeight: 52)
                        .overlay(alignment: .bottom) {
                            if row.id != receipt.rows.last?.id {
                                Rectangle().fill(MonacoTheme.hairline).frame(height: 1).padding(.leading, MonacoTheme.Space.m)
                            }
                        }
                        .accessibilityElement(children: .combine)
                    }
                }

                if let onRetry {
                    Group {
                        if isRetrying {
                            ProgressView()
                                .tint(MonacoTheme.ink)
                                .frame(minHeight: 50)
                                .accessibilityIdentifier("transaction-detail-retry-loading")
                        } else {
                            Button(action: onRetry) {
                                Text("Try again").frame(maxWidth: .infinity)
                            }
                            .buttonStyle(.monacoPrimary)
                                .accessibilityIdentifier("transaction-detail-retry")
                        }
                    }
                    .frame(maxWidth: .infinity)
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .accessibilityIdentifier("transaction-receipt")
    }

    private var statusColor: Color {
        switch receipt.status {
        case .confirmed: MonacoTheme.muted
        case .pending: MonacoTheme.warning
        case .failed: MonacoTheme.loss
        case .other: MonacoTheme.muted
        }
    }
}
