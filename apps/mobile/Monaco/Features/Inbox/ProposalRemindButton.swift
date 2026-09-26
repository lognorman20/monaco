import MonacoCore
import SwiftUI

/// A proposal service that can send "waiting on your vote" reminders. Separate from
/// `ProposalFeedService` so the feed's other conformers need nothing new.
@MainActor
protocol ProposalNudging: AnyObject {
    func nudge(proposalId: String) async throws -> NudgeResultDTO
}

extension LiveProposalFeedService: ProposalNudging {
    func nudge(proposalId: String) async throws -> NudgeResultDTO {
        try await client.nudgeProposal(id: proposalId)
    }
}

/// "Remind them" under the tally: shown to the proposer, or a member who has voted, while the
/// vote is open and someone else still owes a ballot. Once sent it says so and stands down;
/// the server allows one reminder an hour per proposal.
struct ProposalRemindButton: View {
    let proposal: ProposalDTO
    let service: ProposalFeedService
    let viewerChoice: String?
    @Binding var toast: MonacoToast?

    @State private var isSending = false
    @State private var sentFor: String?

    /// Whether the proposal screen should make room for this row at all. Checked at the call
    /// site, so a screen without it spends no stack spacing on an empty view.
    static func shows(proposal: ProposalDTO, service: ProposalFeedService, viewerChoice: String?) -> Bool {
        service is ProposalNudging
            && ProposalNudgeRule.showsRemind(proposal: proposal, viewerId: service.viewerId, viewerChoice: viewerChoice)
    }

    var body: some View {
        if let nudger = service as? ProposalNudging {
            HStack(spacing: MonacoTheme.Space.s) {
                if sentFor == proposal.id {
                    Label(InboxCopy.remindSent, systemImage: "checkmark")
                        .font(MonacoTheme.Typo.calloutStrong)
                        .foregroundStyle(MonacoTheme.muted)
                        .frame(minHeight: 44)
                        .accessibilityIdentifier("proposal-remind-sent")
                } else {
                    Button {
                        Task { await send(with: nudger) }
                    } label: {
                        Label(InboxCopy.remindThem, systemImage: "bell")
                            .font(MonacoTheme.Typo.calloutStrong)
                            .foregroundStyle(MonacoTheme.brand)
                            .frame(minHeight: 44)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .disabled(isSending)
                    .opacity(isSending ? 0.5 : 1)
                    .accessibilityIdentifier("proposal-remind-them")
                }
                Spacer(minLength: 0)
            }
        }
    }

    private func send(with nudger: ProposalNudging) async {
        guard !isSending else { return }
        isSending = true
        defer { isSending = false }
        do {
            let result = try await nudger.nudge(proposalId: proposal.id)
            if result.reminded > 0 {
                Haptics.success()
                sentFor = proposal.id
            }
            toast = MonacoToast(message: InboxCopy.reminded(result), isSuccess: result.reminded > 0)
        } catch {
            if error.isRequestCancellation { return }
            Haptics.warning()
            let status = (error as? MonacoCore.MonacoAPIError)?.statusCode
            if status == 429 { sentFor = proposal.id }
            toast = MonacoToast(message: InboxCopy.remindError(status: status))
        }
    }
}

#if DEBUG
extension SampleProposalFeedService: ProposalNudging {
    /// Answers like the server would for the sample cabal: everyone the tally says has not voted.
    func nudge(proposalId: String) async throws -> NudgeResultDTO {
        let detail = try await proposal(id: proposalId)
        guard let summary = detail.voteSummary else { return NudgeResultDTO(reminded: 0, waitingOn: 0) }
        let waiting = max(0, summary.eligibleCount - summary.yesCount - summary.noCount)
        return NudgeResultDTO(reminded: waiting, waitingOn: waiting)
    }
}
#endif
