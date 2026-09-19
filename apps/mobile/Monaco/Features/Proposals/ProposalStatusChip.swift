import MonacoCore
import SwiftUI

/// The one chip a closed proposal shows: Bought / Sold / Didn't pass / Expired / Failed.
/// Open proposals show none.
struct ProposalStatusChip: View {
    let label: String
    let stage: ProposalExecutionStage?
    let status: String
    var kind: String = "buy"

    var body: some View {
        Text(label)
            .font(MonacoTheme.Typo.micro)
            .foregroundStyle(tint)
            .padding(.horizontal, 10)
            .padding(.vertical, 5)
            .background(Capsule().fill(fill))
            .lineLimit(1)
            .fixedSize()
            .accessibilityIdentifier("proposal-status-\(kind.lowercased())-\(status.lowercased())")
    }

    private var isPositive: Bool {
        ProposalStatusDisplay.from(status: status) == .passed && stage != .failed
    }

    private var tint: Color {
        if stage == .failed { return MonacoTheme.loss }
        if stage == .executing { return MonacoTheme.warning }
        return isPositive ? MonacoTheme.ink : MonacoTheme.muted
    }

    private var fill: Color {
        stage == .failed ? MonacoTheme.lossWash : MonacoTheme.surfaceSunken
    }
}
