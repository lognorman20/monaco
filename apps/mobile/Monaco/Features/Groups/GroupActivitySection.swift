import SwiftUI
import MonacoCore

struct GroupActivitySection: View {
    @ObservedObject var auth: PrivyAuthService
    let items: [GroupActivityItemDTO]
    let isLoading: Bool
    let errorMessage: String?
    let retryingTransactionIDs: Set<String>
    let onRetry: (GroupActivityItemDTO) -> Void

    var body: some View {
        Section("Transaction history") {
            if isLoading {
                HStack(spacing: 12) {
                    ProgressView()
                        .tint(MonacoTheme.accent)
                    Text("Loading activity…")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
                .accessibilityIdentifier("group-activity-loading")
            } else if let errorMessage {
                VStack(alignment: .leading, spacing: 8) {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.warning)
                }
                .accessibilityIdentifier("group-activity-error")
            } else if items.isEmpty {
                Text("No transactions yet.")
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .accessibilityIdentifier("group-activity-empty")
            } else {
                ForEach(items) { item in
                    VStack(alignment: .leading, spacing: 4) {
                        NavigationLink {
                            activityDetailDestination(for: item)
                        } label: {
                            activityRowSummary(item)
                        }
                        .accessibilityIdentifier("group-activity-row-\(item.id)")

                        if canRetry(item) {
                            HStack {
                                Spacer()
                                if retryingTransactionIDs.contains(item.id) {
                                    ProgressView()
                                        .controlSize(.small)
                                        .tint(MonacoTheme.accent)
                                        .accessibilityIdentifier("group-activity-retry-loading-\(item.id)")
                                } else {
                                    Button("Retry") {
                                        onRetry(item)
                                    }
                                    .font(.caption.weight(.semibold))
                                    .buttonStyle(.borderless)
                                    .foregroundStyle(MonacoTheme.accent)
                                    .accessibilityIdentifier("group-activity-retry-\(item.id)")
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func activityDetailDestination(for item: GroupActivityItemDTO) -> some View {
        if needsProposalFallback(item) {
            ActivityDetailDestination(
                auth: auth,
                activityItem: item,
                onRetry: onRetry,
                isRetrying: retryingTransactionIDs.contains(item.id)
            )
        } else {
            TransactionDetailView(
                auth: auth,
                activityItem: item,
                onRetry: onRetry,
                isRetrying: retryingTransactionIDs.contains(item.id)
            )
        }
    }

    private func needsProposalFallback(_ item: GroupActivityItemDTO) -> Bool {
        item.kind.lowercased() == "buy"
            && item.status.lowercased() == "pending"
            && (item.txSignature ?? "").isEmpty
    }

    private func activityRowSummary(_ item: GroupActivityItemDTO) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(activityTitle(item))
                    .font(.body.weight(.semibold))
                    .foregroundStyle(MonacoTheme.primaryText)
                Spacer()
                Text(formatAmount(item))
                    .font(.body.monospacedDigit())
                    .foregroundStyle(MonacoTheme.primaryText)
            }
            HStack {
                Text(formatTimestamp(item.createdAt))
                    .font(.caption)
                    .foregroundStyle(MonacoTheme.secondaryText)
                Spacer()
                Text(statusLabel(item.status))
                    .font(.caption.weight(.medium))
                    .foregroundStyle(statusColor(item.status))
            }
        }
    }

    private func canRetry(_ item: GroupActivityItemDTO) -> Bool {
        guard item.status.lowercased() == "failed" else { return false }
        switch item.kind.lowercased() {
        case "buy", "sell":
            return true
        default:
            return false
        }
    }

    private func activityTitle(_ item: GroupActivityItemDTO) -> String {
        let symbol = AssetSymbolFormatter.format(item.symbol ?? "USDC")
        switch item.kind.lowercased() {
        case "deposit":
            return "Deposit"
        case "buy":
            return "Buy \(symbol)"
        case "sell":
            return "Sell \(symbol)"
        default:
            return item.kind.capitalized
        }
    }

    private func formatAmount(_ item: GroupActivityItemDTO) -> String {
        let dollars = Double(item.amountMicros) / 1_000_000.0
        switch item.kind.lowercased() {
        case "buy":
            return String(format: "$%.2f", dollars)
        case "sell":
            if item.status.lowercased() == "confirmed", item.symbol?.uppercased() == "USDC" {
                return String(format: "$%.2f", dollars)
            }
            if let symbol = item.symbol {
                return "\(symbol) \(formatTokenAmount(dollars))"
            }
            return String(format: "$%.2f", dollars)
        default:
            return String(format: "$%.2f", dollars)
        }
    }

    private func formatTokenAmount(_ amount: Double) -> String {
        if amount >= 1 {
            return String(format: "%.4f", amount)
        }
        return String(format: "%.6f", amount)
    }

    private func formatTimestamp(_ raw: String) -> String {
        guard let date = ISO8601DateFormatter().date(from: raw) else { return raw }
        return date.formatted(date: .abbreviated, time: .shortened)
    }

    private func statusLabel(_ status: String) -> String {
        switch status.lowercased() {
        case "confirmed":
            return "Confirmed"
        case "pending":
            return "Pending"
        case "failed":
            return "Failed"
        default:
            return status.capitalized
        }
    }

    private func statusColor(_ status: String) -> Color {
        switch status.lowercased() {
        case "confirmed":
            return MonacoTheme.success
        case "pending":
            return MonacoTheme.warning
        case "failed":
            return MonacoTheme.warning
        default:
            return MonacoTheme.secondaryText
        }
    }
}
