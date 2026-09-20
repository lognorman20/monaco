import MonacoCore
import SwiftUI

/// A cabal's proposals as cards, Open and Closed, with inline voting.
/// Tapping a card opens `ProposalDetailView` with the reason, ballots and discussion.
struct ProposalFeedView: View {
    let service: ProposalFeedService
    let groupId: String
    var title = ProposalFeedCopy.feedTitle
    /// A proposal that just arrived (from the propose flow): its card pulses once.
    var highlightProposalId: String?

    @State private var tab: ProposalFeedTab = .open
    @State private var proposals: [ProposalFeedTab: [ProposalDTO]] = [:]
    @State private var failedTabs: Set<ProposalFeedTab> = []
    @State private var votingIDs: Set<String> = []
    /// Ballots cast from this screen. Feed rows carry no ballots, so this is how a card knows
    /// to say "You voted yes" after the server stops offering the buttons.
    @State private var viewerChoices: [String: String] = [:]
    @State private var toast: MonacoToast?
    /// Bumped by every load the member caused, so a background poll that was already in flight
    /// does not write its older answer over theirs.
    @State private var loadGeneration = 0

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// Votes and swaps move in seconds; a feed with nothing in play only needs to notice new proposals.
    private var pollInterval: Duration {
        LiveRefreshCadence.watching((proposals[.open] ?? []) + (proposals[.closed] ?? []))
    }

    var body: some View {
        VStack(spacing: 0) {
            tabPicker
                .padding(.horizontal, MonacoTheme.Space.gutter)
            ScrollView {
                LazyVStack(spacing: MonacoTheme.Space.sm) {
                    content
                }
                .padding(.horizontal, MonacoTheme.Space.gutter)
                .padding(.top, MonacoTheme.Space.xs)
                .padding(.bottom, MonacoTheme.Space.l)
            }
            .refreshable {
                await load(tab)
            }
        }
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .task {
            // Both tabs up front so the segment counts are real.
            async let open: Void = load(.open)
            async let closed: Void = load(.closed)
            _ = await (open, closed)
        }
        .pollWhileVisible(every: pollInterval) {
            try await pollBothTabs()
        }
        .monacoToast($toast)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("proposal-feed")
    }

    private var visible: [ProposalDTO] {
        proposals[tab] ?? []
    }

    private var tabPicker: some View {
        MonacoSegmented(ProposalFeedTab.allCases, selection: $tab) { tab in
            if let count = proposals[tab]?.count, count > 0 {
                return "\(tab.title) \(count)"
            }
            return tab.title
        }
        .padding(.vertical, MonacoTheme.Space.s)
        .accessibilityIdentifier("proposal-feed-tab-picker")
    }

    @ViewBuilder
    private var content: some View {
        if proposals[tab] == nil && !failedTabs.contains(tab) {
            ForEach(0..<3, id: \.self) { _ in
                ProposalCardSkeleton()
            }
            .accessibilityIdentifier("proposal-feed-loading")
        } else if failedTabs.contains(tab), proposals[tab] == nil {
            EmptyState(title: ProposalFeedCopy.loadFailed, actionTitle: ProposalFeedCopy.tryAgain) {
                Task { await load(tab) }
            }
            .padding(.top, MonacoTheme.Space.xl)
            .accessibilityIdentifier("proposal-feed-error")
        } else if visible.isEmpty {
            EmptyState(title: tab == .open ? ProposalFeedCopy.emptyOpen : ProposalFeedCopy.emptyClosed)
                .padding(.top, MonacoTheme.Space.xl)
                .accessibilityIdentifier("proposal-feed-empty")
        } else {
            ForEach(visible) { proposal in
                ProposalCardView(
                    proposal: proposal,
                    isVoting: votingIDs.contains(proposal.id),
                    onVote: { choice in Task { await vote(choice, on: proposal) } },
                    destination: {
                        ProposalDetailView(service: service, proposalId: proposal.id, initialProposal: proposal)
                    },
                    viewerChoice: viewerChoices[proposal.id],
                    highlight: proposal.id == highlightProposalId
                )
            }
        }
    }

    /// Background re-read of both tabs. Writes only what changed, and never the failure state:
    /// a poll that throws leaves the cards as they are and lets the loop back off.
    private func pollBothTabs() async throws {
        guard votingIDs.isEmpty else { return }
        let generation = loadGeneration
        async let open = service.listProposals(groupId: groupId, tab: .open)
        async let closed = service.listProposals(groupId: groupId, tab: .closed)
        let loaded: [ProposalFeedTab: [ProposalDTO]] = [.open: try await open, .closed: try await closed]
        guard generation == loadGeneration, votingIDs.isEmpty, !Task.isCancelled else { return }
        for (tab, fresh) in loaded {
            QuietUpdate.apply(fresh, over: proposals[tab]) { value in
                withAnimation(reduceMotion ? nil : .snappy) { proposals[tab] = value }
            }
            if failedTabs.contains(tab) { failedTabs.remove(tab) }
        }
    }

    private func load(_ tab: ProposalFeedTab) async {
        loadGeneration += 1
        do {
            let loaded = try await service.listProposals(groupId: groupId, tab: tab)
            withAnimation(reduceMotion ? nil : .snappy) {
                proposals[tab] = loaded
            }
            failedTabs.remove(tab)
        } catch is CancellationError {
            return
        } catch {
            if error.isRequestCancellation { return }
            failedTabs.insert(tab)
        }
    }

    private func vote(_ choice: ProposalVoteChoice, on proposal: ProposalDTO) async {
        guard votingIDs.insert(proposal.id).inserted else { return }
        defer { votingIDs.remove(proposal.id) }
        let result = await ProposalVoting.cast(choice, proposalId: proposal.id, service: service)
        toast = result.toast
        if result.succeeded {
            Haptics.success()
            withAnimation(reduceMotion ? nil : .spring(response: 0.35, dampingFraction: 0.7)) {
                viewerChoices[proposal.id] = choice.rawValue
            }
        }
        // A vote can close the proposal (threshold reached), so refresh both tabs from the server.
        await load(.open)
        if result.succeeded {
            await load(.closed)
        }
    }
}

/// Placeholder in the shape of a proposal card.
struct ProposalCardSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            HStack(spacing: MonacoTheme.Space.sm) {
                SkeletonBlock(width: 40, height: 40, radius: 13)
                VStack(alignment: .leading, spacing: 6) {
                    SkeletonBlock(width: 110, height: 14)
                    SkeletonBlock(width: 70, height: 11)
                }
            }
            SkeletonBlock(width: 120, height: 28)
            SkeletonBlock(height: 12)
            SkeletonBlock(width: 180, height: 12)
        }
        .padding(MonacoTheme.Space.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous))
        .accessibilityLabel("Loading")
    }
}
