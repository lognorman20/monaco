import MonacoCore
import SwiftUI

/// Group screen: up to two open proposals as full cards with inline voting, and "See all" to the
/// feed. Hidden while loading and when nothing is open, so the screen doesn't jump or nag.
struct ProposalHistorySection: View {
    let service: ProposalFeedService
    let groupId: String
    /// Changes when the parent screen refreshes, so the preview reloads with it.
    var refreshToken: String = ""
    /// Tells the group screen whether a vote is in play, which is what sets its refresh cadence.
    var onOpenVotesChange: (Bool) -> Void = { _ in }
    var onSeeAll: () -> Void = {}
    var onToast: (MonacoToast) -> Void = { _ in }

    private static let previewLimit = 2

    @State private var openProposals: [ProposalDTO] = []
    @State private var votingIDs: Set<String> = []

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private let votes = ProposalVoteLedger.shared

    /// Proposals still waiting on this viewer come first.
    private var preview: [ProposalDTO] {
        let waiting = openProposals.filter(\.showsVoteActions)
        let rest = openProposals.filter { !$0.showsVoteActions }
        return Array((waiting + rest).prefix(Self.previewLimit))
    }

    private var title: String {
        openProposals.contains(where: \.showsVoteActions) ? ProposalFeedCopy.needsYourVote : "Open votes"
    }

    /// Always-present zero-height container so `.task` fires even when nothing is open; the parent
    /// stacks this with the next section so an empty preview adds no gap.
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if !openProposals.isEmpty {
                VStack(alignment: .leading, spacing: 12) {
                    MonacoSectionHeader(title, trailing: "See all", action: onSeeAll)
                        .accessibilityIdentifier("group-proposals-feed-link")
                    VStack(spacing: 12) {
                        ForEach(preview) { proposal in
                            ProposalCardView(
                                proposal: proposal,
                                isVoting: votingIDs.contains(proposal.id),
                                onVote: { choice in Task { await vote(choice, on: proposal) } },
                                destination: {
                                    ProposalDetailView(service: service, proposalId: proposal.id, initialProposal: proposal)
                                },
                                thesisIdentifierPrefix: "group-proposal-thesis",
                                viewerChoice: votes.choice(for: proposal, viewerId: service.viewerId)
                            )
                        }
                    }
                }
                .padding(.bottom, 32)
                .accessibilityIdentifier("group-open-votes")
            }
        }
        .task(id: "\(groupId)-\(refreshToken)") {
            await load()
        }
        .pollWhileVisible(every: LiveRefreshCadence.watching(openProposals)) {
            try await poll()
        }
        .onChange(of: openProposals.contains(where: \.isOpen), initial: true) { _, hasOpenVotes in
            onOpenVotesChange(hasOpenVotes)
        }
    }

    /// Keeps the last good list on failure; the section only disappears when the server says nothing is open.
    private func load() async {
        do {
            openProposals = try await service.listProposals(groupId: groupId, tab: .open)
        } catch {
            return
        }
    }

    /// Background re-read, so another member's vote or a new proposal shows up without a pull.
    /// Stands down while the member's own vote is in flight — `vote` reloads when it lands.
    private func poll() async throws {
        guard votingIDs.isEmpty else { return }
        let fresh = try await service.listProposals(groupId: groupId, tab: .open)
        guard votingIDs.isEmpty, !Task.isCancelled else { return }
        QuietUpdate.apply(fresh, over: openProposals) { openProposals = $0 }
    }

    private func vote(_ choice: ProposalVoteChoice, on proposal: ProposalDTO) async {
        guard votingIDs.insert(proposal.id).inserted else { return }
        defer { votingIDs.remove(proposal.id) }
        let result = await ProposalVoting.cast(choice, proposalId: proposal.id, service: service)
        if result.succeeded {
            Haptics.success()
            withAnimation(reduceMotion ? nil : .spring(response: 0.35, dampingFraction: 0.7)) {
                votes.record(choice, for: proposal.id, viewerId: service.viewerId)
            }
        }
        onToast(result.toast)
        await load()
    }
}
