import MonacoCore
import SwiftUI

/// Group screen entry to the proposal feed: open-vote count, a preview of the newest open
/// proposals, and a link to the full card feed (open + closed, inline voting, discussion).
struct ProposalHistorySection: View {
    let service: ProposalFeedService
    let groupId: String
    /// Changes when the parent screen refreshes, so the preview reloads with it.
    var refreshToken: String = ""

    private static let previewLimit = 3

    @State private var openProposals: [ProposalDTO] = []
    @State private var isLoading = true
    @State private var errorMessage: String?

    var body: some View {
        Section {
            NavigationLink {
                ProposalFeedView(service: service, groupId: groupId)
            } label: {
                HStack {
                    Label(ProposalFeedCopy.feedLinkTitle, systemImage: "text.bubble")
                    Spacer()
                    if !isLoading, errorMessage == nil {
                        Text(ProposalFeedCopy.openCount(openProposals.count))
                            .font(.footnote)
                            .foregroundStyle(MonacoTheme.secondaryText)
                    }
                }
            }
            .accessibilityIdentifier("group-proposals-feed-link")

            if isLoading {
                ProgressView()
                    .tint(MonacoTheme.accent)
                    .accessibilityIdentifier("group-proposals-loading")
            } else if let errorMessage {
                Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.warning)
                    .accessibilityIdentifier("group-proposals-error")
            } else {
                ForEach(openProposals.prefix(Self.previewLimit)) { proposal in
                    NavigationLink {
                        ProposalDetailView(service: service, proposalId: proposal.id, initialProposal: proposal)
                    } label: {
                        previewRow(proposal)
                    }
                    .accessibilityIdentifier("group-proposal-row-\(proposal.id)")
                }
            }
        } header: {
            Text(ProposalFeedCopy.feedTitle)
        }
        .task(id: "\(groupId)-\(refreshToken)") {
            await load()
        }
    }

    private func previewRow(_ proposal: ProposalDTO) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(ProposalFeedCopy.headline(for: proposal))
                    .font(.body.weight(.semibold))
                Spacer()
                if proposal.showsVoteActions {
                    Text(ProposalFeedCopy.needsYourVote)
                        .font(.caption.bold())
                        .foregroundStyle(MonacoTheme.accent)
                }
            }
            if let thesis = proposal.thesis, !thesis.isEmpty {
                Text(thesis)
                    .font(.caption)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .lineLimit(2)
                    .accessibilityIdentifier("group-proposal-thesis-\(proposal.id)")
            }
            HStack(spacing: 8) {
                if let summary = proposal.voteSummary {
                    Text(ProposalVoteProgress(summary: summary).caption)
                }
                if let count = proposal.commentCount, count > 0 {
                    Text(ProposalFeedCopy.commentCount(count))
                }
            }
            .font(.caption)
            .foregroundStyle(MonacoTheme.secondaryText)
        }
    }

    private func load() async {
        errorMessage = nil
        defer { isLoading = false }
        do {
            openProposals = try await service.listProposals(groupId: groupId, tab: .open)
        } catch is CancellationError {
            return
        } catch {
            errorMessage = ProposalFeedCopy.loadFailed
        }
    }
}
