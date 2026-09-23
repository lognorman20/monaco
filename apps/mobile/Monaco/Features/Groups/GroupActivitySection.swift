import MonacoCore
import SwiftUI

/// Group screen: the latest five things that happened to the pot, with "See all".
struct GroupActivitySection: View {
    @ObservedObject var auth: DynamicAuthService
    let items: [GroupActivityItemDTO]
    let isLoading: Bool
    let errorMessage: String?
    let retryingTransactionIDs: Set<String>
    let onRetry: (GroupActivityItemDTO) async -> RetryTransactionResponse?
    var onSeeAll: () -> Void = {}

    static let previewLimit = 5

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if items.count > Self.previewLimit {
                MonacoSectionHeader("Activity", trailing: "See all", action: onSeeAll)
                    .accessibilityIdentifier("group-activity-see-all")
            } else {
                MonacoSectionHeader("Activity")
            }

            if isLoading && items.isEmpty {
                VStack(spacing: 12) {
                    ForEach(0..<3, id: \.self) { _ in
                        SkeletonBlock(height: 44)
                    }
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("Loading activity")
                .accessibilityIdentifier("group-activity-loading")
            } else if let errorMessage, items.isEmpty {
                Text(errorMessage)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.fgMuted)
                    .accessibilityIdentifier("group-activity-error")
            } else if items.isEmpty {
                Text("Nothing yet. Money in, buys, and sells show up here.")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.fgMuted)
                    .accessibilityIdentifier("group-activity-empty")
            } else {
                GroupActivityList(
                    auth: auth,
                    items: Array(items.prefix(Self.previewLimit)),
                    retryingTransactionIDs: retryingTransactionIDs,
                    onRetry: onRetry
                )
            }
        }
        .accessibilityIdentifier("group-activity")
    }
}

/// Rows of activity inside one surface, each pushing its receipt.
struct GroupActivityList: View {
    @ObservedObject var auth: DynamicAuthService
    let items: [GroupActivityItemDTO]
    let retryingTransactionIDs: Set<String>
    let onRetry: (GroupActivityItemDTO) async -> RetryTransactionResponse?

    var body: some View {
        MonacoGroupedList {
            ForEach(items) { item in
                HStack(spacing: 0) {
                    NavigationLink {
                        destination(for: item)
                    } label: {
                        GroupActivityRow(item: item)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("group-activity-row-\(item.id)")

                    if GroupActivityRules.canRetry(item) {
                        retryControl(item)
                    }
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .overlay(alignment: .bottom) {
                    if item.id != items.last?.id {
                        Rectangle().fill(MonacoTheme.line).frame(height: 1).padding(.leading, 60)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func retryControl(_ item: GroupActivityItemDTO) -> some View {
        if retryingTransactionIDs.contains(item.id) {
            ProgressView()
                .tint(MonacoTheme.controlTint)
                .frame(width: 44, height: 44)
                .accessibilityIdentifier("group-activity-retry-loading-\(item.id)")
        } else {
            Button("Retry") { Task { _ = await onRetry(item) } }
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(MonacoTheme.loss)
                .frame(minWidth: 44, minHeight: 44)
                .padding(.leading, 8)
                .accessibilityIdentifier("group-activity-retry-\(item.id)")
        }
    }

    @ViewBuilder
    private func destination(for item: GroupActivityItemDTO) -> some View {
        if GroupActivityRules.needsProposalFallback(item) {
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
}

/// Glyph · title / time · amount, with status only when something isn't confirmed.
struct GroupActivityRow: View {
    let item: GroupActivityItemDTO

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: GroupActivityRules.glyph(for: item.kind))
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(MonacoTheme.fgPrimary)
                .frame(width: 32, height: 32)
                .background(Circle().fill(MonacoTheme.fillQuiet))
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(GroupActivityRules.title(for: item))
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.fgPrimary)
                    .lineLimit(1)
                HStack(spacing: 4) {
                    Text(GroupActivityRules.timeLabel(item.createdAt))
                        .foregroundStyle(MonacoTheme.fgMuted)
                    if let status = GroupActivityRules.statusLabel(item.status) {
                        Text("·").foregroundStyle(MonacoTheme.fgMuted)
                        Text(status.text)
                            .fontWeight(.semibold)
                            .foregroundStyle(status.isFailure ? MonacoTheme.loss : MonacoTheme.warning)
                    }
                }
                .font(MonacoTheme.Typo.caption)
                .lineLimit(1)
            }
            Spacer(minLength: 8)
            if !GroupActivityRules.canRetry(item) {
                amount
            }
        }
        .padding(.vertical, 10)
        .frame(minHeight: 60)
        .accessibilityElement(children: .combine)
    }

    private var amount: some View {
            Group {
                if let micros = GroupActivityRules.amountMicros(item) {
                    MoneyText(micros: micros, style: .row)
                } else {
                    Text(GroupActivityRules.amountLabel(item))
                        .font(MonacoTheme.Typo.moneyRow)
                        .foregroundStyle(MonacoTheme.fgPrimary)
                }
            }
            .lineLimit(1)
    }
}

/// Activity row rules shared by the group section, the full list, and tests of intent.
enum GroupActivityRules {
    static func canRetry(_ item: GroupActivityItemDTO) -> Bool {
        guard item.status.lowercased() == "failed" else { return false }
        return ["buy", "sell"].contains(item.kind.lowercased())
    }

    static func needsProposalFallback(_ item: GroupActivityItemDTO) -> Bool {
        if isAgentGovernanceKind(item.kind) {
            return true
        }
        return ["buy", "sell"].contains(item.kind.lowercased())
            && item.status.lowercased() == "pending"
            && (item.txHash ?? "").isEmpty
    }

    static func isAgentGovernanceKind(_ kind: String) -> Bool {
        ["add_agent", "pause_agent", "resume_agent", "revoke_agent"].contains(kind.lowercased())
    }

    static func glyph(for kind: String) -> String {
        switch kind.lowercased() {
        case "buy": "arrow.down"
        case "sell": "arrow.up"
        case "deposit": "plus"
        default: isAgentGovernanceKind(kind) ? "cpu" : "circle"
        }
    }

    /// "Bought Apple", "Buying Apple", "Money added"; agent kinds keep the shared formatter's copy.
    static func title(for item: GroupActivityItemDTO) -> String {
        let status = item.status.lowercased()
        let stock = item.symbol.map { AssetDisplayNames.name(forSymbol: $0) ?? AssetSymbolFormatter.display($0) } ?? "stock"
        let byAgent = item.initiatedBy?.lowercased() == "agent"
            ? item.agentDisplayName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            : ""
        let prefix = byAgent.isEmpty ? "" : "\(byAgent) · "
        switch item.kind.lowercased() {
        case "deposit":
            return status == "confirmed" ? "Money added" : "Adding money"
        case "buy":
            return prefix + (status == "confirmed" ? "Bought \(stock)" : status == "failed" ? "Buy \(stock)" : "Buying \(stock)")
        case "sell":
            return prefix + (status == "confirmed" ? "Sold \(stock)" : status == "failed" ? "Sell \(stock)" : "Selling \(stock)")
        default:
            break
        }
        return GroupActivityTitleFormatter.format(
            kind: item.kind,
            symbol: item.symbol,
            agentDisplayName: item.agentDisplayName,
            initiatedBy: item.initiatedBy
        )
    }

    /// Nil when confirmed: a row only says its status when something needs attention.
    static func statusLabel(_ status: String) -> (text: String, isFailure: Bool)? {
        switch status.lowercased() {
        case "confirmed": nil
        case "pending": ("Pending", false)
        case "failed": ("Failed", true)
        default: (status.capitalized, false)
        }
    }

    /// Dollar figure for the row, or nil when a sell has no proceeds yet (shown in shares instead).
    static func amountMicros(_ item: GroupActivityItemDTO) -> Int64? {
        if item.kind.lowercased() == "sell" {
            if let proceeds = item.proceedsUsdcMicros, let micros = Int64(proceeds), micros > 0 {
                return micros
            }
            // A sell's amountMicros is not dollars; without proceeds the row shows shares instead.
            return nil
        }
        return item.amountMicros
    }

    static func amountLabel(_ item: GroupActivityItemDTO) -> String {
        if item.kind.lowercased() == "sell" {
            if let proceeds = item.proceedsUsdcMicros, let micros = Int64(proceeds), micros > 0 {
                return UsdAmountFormatter.format(micros: micros)
            }
            if let tokenAmount = item.tokenAmount, let atomics = Double(tokenAmount) {
                return sharesLabel(atomics / 100_000_000.0)
            }
            return "—"
        }
        return UsdAmountFormatter.format(micros: item.amountMicros)
    }

    static func sharesLabel(_ shares: Double) -> String {
        let trimmed = String(format: "%.4f", shares)
            .replacingOccurrences(of: "0+$", with: "", options: .regularExpression)
            .replacingOccurrences(of: "\\.$", with: "", options: .regularExpression)
        return trimmed == "1" ? "1 share" : "\(trimmed) shares"
    }

    static func timeLabel(_ raw: String) -> String {
        RelativeTimeFormatter.label(iso: raw)
    }

    static func parseDate(_ raw: String) -> Date? {
        SharedFormatters.iso8601Date(from: raw)
    }
}
