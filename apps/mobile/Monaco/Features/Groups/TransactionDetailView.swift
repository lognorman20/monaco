import SwiftUI
import MonacoCore

/// Full detail for one activity row — deposit, buy, or sell.
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
        Form {
            if isDeposit {
                depositContent
            } else if let transaction {
                swapContent(transaction)
            } else if let errorMessage {
                errorSection(errorMessage)
            } else if isLoading {
                Section {
                    ProgressView("Loading transaction…")
                        .tint(MonacoTheme.accent)
                }
            }
        }
        .monacoFormScreen()
        .navigationTitle(screenTitle)
        .navigationBarTitleDisplayMode(.inline)
        .task(id: loadTaskID) {
            await loadDetail()
        }
    }

    private var loadTaskID: String {
        "\(activityItem.id)-\(activityItem.kind)-\(auth.accessToken ?? "")"
    }

    private var isDeposit: Bool {
        activityItem.kind.lowercased() == "deposit"
    }

    private var screenTitle: String {
        switch activityItem.kind.lowercased() {
        case "deposit": "Deposit"
        case "buy": "Buy"
        case "sell": "Sell"
        default: activityItem.kind.capitalized
        }
    }

    @ViewBuilder
    private var depositContent: some View {
        if let deposit {
            summarySection(
                kindLabel: "Deposit",
                status: deposit.status,
                amountLabel: formatUsdc(deposit.amount)
            )

            Section("Details") {
                LabeledContent("Amount", value: formatUsdc(deposit.amount))
                LabeledContent("Status", value: statusLabel(deposit.status))
                LabeledContent("Created", value: formatTimestamp(deposit.createdAt))
                if deposit.status.lowercased() == "failed" {
                    LabeledContent("Failure reason", value: "Sweep failed")
                }
                if let fromAddress = deposit.fromAddress, !fromAddress.isEmpty {
                    VStack(alignment: .leading, spacing: 6) {
                        Text("From address")
                            .font(.subheadline)
                            .foregroundStyle(MonacoTheme.secondaryText)
                        MonacoWalletAddressText(address: fromAddress, font: .footnote.monospaced())
                    }
                }
                signatureRow(deposit.txSignature)
                if deposit.shareUnits > 0 {
                    LabeledContent("Your share units", value: "\(deposit.shareUnits)")
                }
            }

            Section("Identifiers") {
                LabeledContent("Deposit ID", value: deposit.depositId)
                LabeledContent("Cabal ID", value: deposit.groupId)
            }
        } else if let errorMessage {
            errorSection(errorMessage)
        } else if isLoading {
            Section {
                ProgressView("Loading deposit…")
                    .tint(MonacoTheme.accent)
            }
        }
    }

    @ViewBuilder
    private func swapContent(_ transaction: TransactionDetailDTO) -> some View {
        summarySection(
            kindLabel: screenTitle,
            status: transaction.status,
            amountLabel: formatSwapAmount(transaction)
        )

        Section("Details") {
            LabeledContent("Kind", value: transaction.action.capitalized)
            LabeledContent("Status", value: statusLabel(transaction.status))
            LabeledContent("Created", value: formatTimestamp(transaction.createdAt))
            if let confirmedAt = transaction.confirmedAt {
                LabeledContent("Executed", value: formatTimestamp(confirmedAt))
            } else {
                LabeledContent("Executed", value: "N/A")
            }
            if let inputSymbol = transaction.inputSymbol, !inputSymbol.isEmpty {
                LabeledContent("Input", value: AssetSymbolFormatter.format(inputSymbol))
            }
            if let outputSymbol = transaction.outputSymbol, !outputSymbol.isEmpty {
                LabeledContent("Output", value: AssetSymbolFormatter.format(outputSymbol))
            }
            if let price = transaction.costBasisPrice, price > 0 {
                LabeledContent("Cost basis (USDC)", value: formatUsdc(price))
            }
            if let amount = transaction.costBasisAmount, amount > 0 {
                LabeledContent("Fill amount", value: formatFillAmount(transaction, amount))
            }
            if let reason = transaction.failureReason, !reason.isEmpty {
                LabeledContent("Failure reason", value: reason)
            }
        }

        Section("On chain") {
            signatureRow(transaction.txSignature)
            copyableRow(label: "Execute request", value: transaction.executeRequestId)
        }

        Section("Identifiers") {
            LabeledContent("Transaction ID", value: transaction.transactionId)
            LabeledContent("Cabal ID", value: transaction.groupId)
            copyableRow(label: "Proposal ID", value: transaction.proposalId)
            copyableRow(label: "Input mint", value: transaction.inputMint)
            copyableRow(label: "Output mint", value: transaction.outputMint)
        }

        if canRetry {
            Section {
                if isRetrying {
                    HStack {
                        Spacer()
                        ProgressView()
                            .tint(MonacoTheme.accent)
                        Spacer()
                    }
                    .accessibilityIdentifier("transaction-detail-retry-loading")
                } else {
                    Button("Retry swap") {
                        onRetry?(activityItem)
                    }
                    .monacoFormSecondaryAction()
                    .accessibilityIdentifier("transaction-detail-retry")
                }
            }
        }
    }

    @ViewBuilder
    private func summarySection(kindLabel: String, status: String, amountLabel: String) -> some View {
        Section {
            HStack {
                Text(kindLabel)
                    .font(.title2.bold())
                Spacer()
                TransactionStatusChip(status: status)
            }
            Text(amountLabel)
                .font(.subheadline)
                .foregroundStyle(MonacoTheme.secondaryText)
        }
    }

    @ViewBuilder
    private func signatureRow(_ signature: String?) -> some View {
        if let signature, !signature.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Text("Tx signature")
                    .font(.subheadline)
                    .foregroundStyle(MonacoTheme.secondaryText)
                MonacoWalletAddressText(address: signature, font: .footnote.monospaced())
            }
        } else {
            LabeledContent("Tx signature", value: "N/A")
        }
    }

    @ViewBuilder
    private func copyableRow(label: String, value: String?) -> some View {
        if let value, !value.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Text(label)
                    .font(.subheadline)
                    .foregroundStyle(MonacoTheme.secondaryText)
                MonacoWalletAddressText(address: value, font: .footnote.monospaced())
            }
        } else {
            LabeledContent(label, value: "N/A")
        }
    }

    @ViewBuilder
    private func errorSection(_ message: String) -> some View {
        Section {
            Label(message, systemImage: "exclamationmark.triangle.fill")
                .font(.footnote)
                .foregroundStyle(MonacoTheme.warning)
            Button("Try again") {
                Task { await loadDetail() }
            }
            .monacoFormSecondaryAction()
        }
    }

    private var canRetry: Bool {
        guard activityItem.status.lowercased() == "failed" else { return false }
        switch activityItem.kind.lowercased() {
        case "buy", "sell":
            return onRetry != nil
        default:
            return false
        }
    }

    private func formatSwapAmount(_ transaction: TransactionDetailDTO) -> String {
        switch transaction.action.lowercased() {
        case "buy":
            return "\(formatUsdc(transaction.amountMicros)) USDC → \(AssetSymbolFormatter.format(transaction.outputSymbol ?? "token"))"
        case "sell":
            if transaction.status.lowercased() == "confirmed",
               transaction.outputSymbol?.uppercased() == "USDC" || transaction.inputSymbol?.uppercased() != "USDC" {
                if let proceeds = transaction.costBasisAmount, proceeds > 0 {
                    return "\(AssetSymbolFormatter.format(transaction.inputSymbol ?? "token")) → \(formatUsdc(proceeds))"
                }
            }
            return "\(AssetSymbolFormatter.format(transaction.inputSymbol ?? "token")) → USDC"
        default:
            return formatUsdc(transaction.amountMicros)
        }
    }

    private func formatFillAmount(_ transaction: TransactionDetailDTO, _ micros: Int64) -> String {
        switch transaction.action.lowercased() {
        case "buy":
            if let symbol = transaction.outputSymbol {
                return "\(AssetSymbolFormatter.format(symbol)) \(formatTokenAmount(Double(micros) / 100_000_000.0))"
            }
            return formatTokenAmount(Double(micros) / 100_000_000.0)
        case "sell":
            let proceeds = transaction.proceedsUsdcMicros ?? transaction.costBasisAmount ?? micros
            return formatUsdc(proceeds)
        default:
            return formatUsdc(micros)
        }
    }

    private func formatTokenAmount(_ amount: Double) -> String {
        String(format: "%.8f", amount).replacingOccurrences(of: "0+$", with: "", options: .regularExpression)
    }

    private func formatUsdc(_ micros: Int64) -> String {
        String(format: "$%.2f", Double(micros) / 1_000_000.0)
    }

    private func statusLabel(_ status: String) -> String {
        switch status.lowercased() {
        case "confirmed": "Confirmed"
        case "pending": "Pending"
        case "failed": "Failed"
        default: status.capitalized
        }
    }

    private func formatTimestamp(_ raw: String) -> String {
        guard let date = ISO8601DateFormatter().date(from: raw) else { return raw }
        return date.formatted(date: .abbreviated, time: .shortened)
    }

    private func loadDetail() async {
        guard let token = auth.accessToken else {
            isLoading = false
            errorMessage = "Missing sign-in token."
            return
        }

        isLoading = true
        errorMessage = nil
        defer { isLoading = false }

        do {
            if isDeposit {
                deposit = try await apiClient.getDeposit(accessToken: token, depositId: activityItem.id)
            } else {
                transaction = try await apiClient.getTransactionDetail(accessToken: token, transactionId: activityItem.id)
            }
        } catch is CancellationError {
            return
        } catch MonacoAPIError.httpStatus(let code) {
            errorMessage = "Could not load details (HTTP \(code))."
        } catch {
            errorMessage = "Could not load details."
        }
    }
}

private struct TransactionStatusChip: View {
    let status: String

    var body: some View {
        Text(label)
            .font(.caption.weight(.semibold))
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .foregroundStyle(foreground)
            .background(background, in: Capsule())
    }

    private var label: String {
        switch status.lowercased() {
        case "confirmed": "Confirmed"
        case "pending": "Pending"
        case "failed": "Failed"
        default: status.capitalized
        }
    }

    private var foreground: Color {
        switch status.lowercased() {
        case "confirmed": MonacoTheme.success
        case "pending", "failed": MonacoTheme.warning
        default: MonacoTheme.secondaryText
        }
    }

    private var background: Color {
        foreground.opacity(0.15)
    }
}
