import SwiftUI
import MonacoCore

struct GroupActivitySection: View {
    @ObservedObject var auth: PrivyAuthService
    let items: [GroupActivityItemDTO]
    let isLoading: Bool
    let errorMessage: String?
    let retryingTransactionIDs: Set<String>
    let onRetry: (GroupActivityItemDTO) -> Void

    @State private var isExpanded = false

    private let collapsedItemLimit = 4

    private var visibleItems: [GroupActivityItemDTO] {
        isExpanded ? items : Array(items.prefix(collapsedItemLimit))
    }

    private var hasMoreThanCollapsedLimit: Bool {
        items.count > collapsedItemLimit
    }

    var body: some View {
        Section {
            header

            content

            if !isLoading, errorMessage == nil, hasMoreThanCollapsedLimit {
                Button {
                    withAnimation(.easeInOut(duration: 0.2)) {
                        isExpanded.toggle()
                    }
                } label: {
                    HStack {
                        Text(isExpanded ? "Show less" : "Show all")
                        Spacer()
                        Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                            .font(.caption.weight(.semibold))
                    }
                    .foregroundStyle(MonacoTheme.accent)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("group-activity-toggle")
            }
        }
    }

    private var header: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            Text("Transaction history")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.primaryText)
            Spacer(minLength: 8)
            if !isLoading, errorMessage == nil, !items.isEmpty {
                Text("\(items.count)")
                    .font(.caption.weight(.semibold).monospacedDigit())
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(MonacoTheme.surface, in: Capsule())
                    .overlay {
                        Capsule().strokeBorder(MonacoTheme.hairline, lineWidth: 1)
                    }
                    .accessibilityIdentifier("group-activity-count")
            }
        }
    }

    @ViewBuilder
    private var content: some View {
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
            ForEach(visibleItems) { item in
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
        if isAgentGovernanceKind(item.kind) {
            return true
        }
        return ["buy", "sell"].contains(item.kind.lowercased())
            && item.status.lowercased() == "pending"
            && (item.txSignature ?? "").isEmpty
    }

    private func isAgentGovernanceKind(_ kind: String) -> Bool {
        ["add_agent", "pause_agent", "resume_agent", "revoke_agent"].contains(kind.lowercased())
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
        GroupActivityTitleFormatter.format(
            kind: item.kind,
            symbol: item.symbol,
            agentDisplayName: item.agentDisplayName,
            initiatedBy: item.initiatedBy
        )
    }

    private func formatAmount(_ item: GroupActivityItemDTO) -> String {
        if item.kind.lowercased() == "sell" {
            if let proceeds = item.proceedsUsdcMicros, let micros = Int64(proceeds), micros > 0 {
                return String(format: "$%.2f", Double(micros) / 1_000_000.0)
            }
            if let tokenAmount = item.tokenAmount, let atomics = Double(tokenAmount) {
                return "\(AssetSymbolFormatter.format(item.symbol ?? "")) \(formatTokenAmount(atomics / 100_000_000.0))"
            }
        }
        let dollars = Double(item.amountMicros) / 1_000_000.0
        switch item.kind.lowercased() {
        case "buy":
            return String(format: "$%.2f", dollars)
        case "sell":
            if let symbol = item.symbol {
                return "\(AssetSymbolFormatter.format(symbol)) \(formatTokenAmount(dollars))"
            }
            return String(format: "$%.2f", dollars)
        default:
            return String(format: "$%.2f", dollars)
        }
    }

    private func formatTokenAmount(_ amount: Double) -> String {
        String(format: "%.8f", amount).replacingOccurrences(of: "0+$", with: "", options: .regularExpression)
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
