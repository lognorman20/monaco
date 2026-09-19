import MonacoCore
import SwiftUI

/// Group screen: up to two open proposals as full cards with inline voting, and "See all" to the
/// feed. Hidden while loading and when nothing is open, so the screen doesn't jump or nag.
struct ProposalHistorySection: View {
    let service: ProposalFeedService
    let groupId: String
    /// Changes when the parent screen refreshes, so the preview reloads with it.
    var refreshToken: String = ""
    var onSeeAll: () -> Void = {}
    var onToast: (MonacoToast) -> Void = { _ in }

    private static let previewLimit = 2

    @State private var openProposals: [ProposalDTO] = []
    @State private var votingIDs: Set<String> = []

    /// Proposals still waiting on this viewer come first.
    private var preview: [ProposalDTO] {
        let waiting = openProposals.filter(\.showsVoteActions)
        let rest = openProposals.filter { !$0.showsVoteActions }
        return Array((waiting + rest).prefix(Self.previewLimit))
    }

    private var title: String {
        openProposals.contains(where: \.showsVoteActions) ? ProposalFeedCopy.needsYourVote : "Open votes"
    }

    var body: some View {
        Group {
            if !openProposals.isEmpty {
                VStack(alignment: .leading, spacing: 12) {
                    HStack(alignment: .firstTextBaseline) {
                        Text(title)
                            .font(MonacoTheme.TypeRole.title)
                            .foregroundStyle(MonacoTheme.ink)
                        Spacer()
                        Button("See all", action: onSeeAll)
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(MonacoTheme.ink)
                            .frame(minHeight: 44)
                            .accessibilityIdentifier("group-proposals-feed-link")
                    }
                    VStack(spacing: 12) {
                        ForEach(preview) { proposal in
                            ProposalCardView(
                                proposal: proposal,
                                isVoting: votingIDs.contains(proposal.id),
                                onVote: { choice in Task { await vote(choice, on: proposal) } },
                                destination: {
                                    ProposalDetailView(service: service, proposalId: proposal.id, initialProposal: proposal)
                                },
                                thesisIdentifierPrefix: "group-proposal-thesis"
                            )
                        }
                    }
                }
                .accessibilityIdentifier("group-open-votes")
            }
        }
        .task(id: "\(groupId)-\(refreshToken)") {
            await load()
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

    private func vote(_ choice: ProposalVoteChoice, on proposal: ProposalDTO) async {
        guard votingIDs.insert(proposal.id).inserted else { return }
        defer { votingIDs.remove(proposal.id) }
        let result = await ProposalVoting.cast(choice, proposalId: proposal.id, service: service)
        onToast(result.toast)
        await load()
    }
}
