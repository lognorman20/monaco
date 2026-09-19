import MonacoCore
import SwiftUI

/// Scrollable card feed of a cabal's proposals with open/closed tabs and inline voting.
/// Tapping a card opens `ProposalDetailView` with the discussion thread.
struct ProposalFeedView: View {
    let service: ProposalFeedService
    let groupId: String
    var title = ProposalFeedCopy.feedTitle

    @State private var tab: ProposalFeedTab = .open
    @State private var proposals: [ProposalFeedTab: [ProposalDTO]] = [:]
    @State private var isLoading = false
    @State private var errorMessage: String?
    @State private var votingIDs: Set<String> = []
    @State private var toast: MonacoToast?

    var body: some View {
        ScrollView {
            LazyVStack(spacing: 12, pinnedViews: [.sectionHeaders]) {
                Section {
                    content
                } header: {
                    tabPicker
                }
            }
            .padding(.bottom, 24)
        }
        .background(MonacoTheme.background)
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .task(id: tab) {
            await load(tab)
        }
        .refreshable {
            await load(tab)
        }
        .monacoToast($toast)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("proposal-feed")
    }

    private var visible: [ProposalDTO] {
        proposals[tab] ?? []
    }

    private var tabPicker: some View {
        Picker("Proposal status", selection: $tab) {
            ForEach(ProposalFeedTab.allCases) { tab in
                Text(tab.title).tag(tab)
            }
        }
        .pickerStyle(.segmented)
        .monacoSegmentedBoardPicker()
        .accessibilityIdentifier("proposal-feed-tab-picker")
    }

    @ViewBuilder
    private var content: some View {
        if isLoading && proposals[tab] == nil {
            ProgressView()
                .tint(MonacoTheme.accent)
                .padding(.top, 32)
                .accessibilityIdentifier("proposal-feed-loading")
        } else if let errorMessage, proposals[tab] == nil {
            VStack(spacing: 12) {
                Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.warning)
                Button("Try again") { Task { await load(tab) } }
                    .buttonStyle(.monacoSecondary)
            }
            .padding(.top, 32)
            .accessibilityIdentifier("proposal-feed-error")
        } else if visible.isEmpty {
            Text(tab == .open ? ProposalFeedCopy.emptyOpen : ProposalFeedCopy.emptyClosed)
                .font(.subheadline)
                .foregroundStyle(MonacoTheme.secondaryText)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 32)
                .padding(.top, 32)
                .accessibilityIdentifier("proposal-feed-empty")
        } else {
            ForEach(visible) { proposal in
                ProposalCardView(
                    proposal: proposal,
                    isVoting: votingIDs.contains(proposal.id),
                    onVote: { choice in Task { await vote(choice, on: proposal) } },
                    destination: {
                        ProposalDetailView(service: service, proposalId: proposal.id, initialProposal: proposal)
                    }
                )
                .padding(.horizontal, 16)
            }
        }
    }

    private func load(_ tab: ProposalFeedTab) async {
        isLoading = true
        errorMessage = nil
        defer { isLoading = false }
        do {
            proposals[tab] = try await service.listProposals(groupId: groupId, tab: tab)
        } catch is CancellationError {
            return
        } catch {
            errorMessage = ProposalFeedCopy.loadFailed
        }
    }

    private func vote(_ choice: ProposalVoteChoice, on proposal: ProposalDTO) async {
        guard votingIDs.insert(proposal.id).inserted else { return }
        defer { votingIDs.remove(proposal.id) }
        let result = await ProposalVoting.cast(choice, proposalId: proposal.id, service: service)
        toast = result.toast
        // A vote can close the proposal (threshold reached), so refresh both tabs from the server.
        await load(.open)
        if result.succeeded, proposals[.closed] != nil {
            await load(.closed)
        }
    }
}
