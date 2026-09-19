import SwiftUI

struct ProposalStatusChip: View {
    let status: String
    var kind: String = "buy"

    var body: some View {
        Text(label)
            .font(.caption.bold())
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(backgroundColor.opacity(0.15))
            .foregroundStyle(backgroundColor)
            .clipShape(Capsule())
            .accessibilityIdentifier("proposal-status-\(kind.lowercased())-\(status.lowercased())")
    }

    private var chipStyle: ProposalStatusChipStyle? {
        ProposalStatusChipStyle(status: status)
    }

    private var label: String {
        let side = kind.lowercased() == "sell" ? "Sell" : "Buy"
        return "\(side) \(chipStyle?.label ?? status.capitalized)"
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
