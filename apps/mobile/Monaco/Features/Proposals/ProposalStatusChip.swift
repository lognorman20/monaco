import SwiftUI

struct ProposalStatusChip: View {
    let status: String

    var body: some View {
        Text(chipStyle?.label ?? status.capitalized)
            .font(.caption.bold())
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(backgroundColor.opacity(0.15))
            .foregroundStyle(backgroundColor)
            .clipShape(Capsule())
            .accessibilityIdentifier("proposal-status-\(status.lowercased())")
    }

    private var chipStyle: ProposalStatusChipStyle? {
        ProposalStatusChipStyle(status: status)
    }

    private var backgroundColor: Color {
        switch chipStyle {
        case .open: .blue
        case .passed: .green
        case .failed: .red
        case .expired: .gray
        case .none: .secondary
        }
    }
}
