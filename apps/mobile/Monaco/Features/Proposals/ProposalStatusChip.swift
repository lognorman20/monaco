import MonacoCore
import SwiftUI

/// The one chip a closed proposal shows: Bought / Sold / Didn't pass / Expired / Failed.
/// Open proposals show none — an open vote's state is its tally, and in its last hour the
/// countdown capsule takes this slot.
///
/// **A passed proposal is ink, never green.** Green is profit and nothing else in this product,
/// so the cabal agreeing to spend money reads as a decision made, not as money made. Amber means
/// the swap is still running; red means it did not happen.
struct ProposalStatusChip: View {
    let label: String
    let stage: ProposalExecutionStage?
    let status: String
    var kind: String = "buy"

    var body: some View {
        Text(label)
            .font(MonacoTheme.Typo.micro)
            .foregroundStyle(intent.label)
            .padding(.horizontal, 10)
            .padding(.vertical, 5)
            .background(Capsule().fill(intent.wash))
            .lineLimit(1)
            .fixedSize()
            .accessibilityIdentifier("proposal-status-\(kind.lowercased())-\(status.lowercased())")
    }

    private var intent: ProposalStatusIntent {
        ProposalStatusIntent.of(status: status, stage: stage)
    }
}

/// The four states a chip can be in, and the wash each one takes.
///
/// Split out from the view so the rule — passed is ink, executing is amber, failed is red,
/// everything else is quiet — is one table rather than three ternaries.
enum ProposalStatusIntent {
    /// The vote passed and the trade has landed, or there was no trade to make.
    case settled
    /// The swap is in flight.
    case running
    /// The swap failed.
    case failed
    /// Did not pass, or expired. Nothing went wrong; nothing happened either.
    case quiet

    static func of(status: String, stage: ProposalExecutionStage?) -> ProposalStatusIntent {
        if stage == .failed { return .failed }
        if stage == .executing { return .running }
        return ProposalStatusDisplay.from(status: status) == .passed ? .settled : .quiet
    }

    var wash: Color {
        switch self {
        case .settled: return MonacoTheme.inkWash
        case .running: return MonacoTheme.warningWash
        case .failed: return MonacoTheme.dangerWash
        case .quiet: return MonacoTheme.fillQuiet
        }
    }

    var label: Color {
        switch self {
        case .settled: return MonacoTheme.fgPrimary
        case .running: return MonacoTheme.warningOnWash
        case .failed: return MonacoTheme.dangerOnWash
        case .quiet: return MonacoTheme.fgMuted
        }
    }
}
