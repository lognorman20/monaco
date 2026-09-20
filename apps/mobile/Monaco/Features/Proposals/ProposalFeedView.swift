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
    @State private var toast: MonacoToast?
    /// Bumped by every load the member caused, so a background poll that was already in flight
    /// does not write its older answer over theirs.
    @State private var loadGeneration = 0
    /// A proposal has left the Open list and the Closed tab has not been re-read since. Held
    /// across ticks so a closed read that throws does not lose the one signal that it changed.
    @State private var closedIsStale = false

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private let votes = ProposalVoteLedger.shared

    /// Votes and swaps move in seconds; a feed with nothing in play only needs to notice new
    /// proposals. Only the Open list counts: list rows carry no execution state, so a closed row
    /// can never be the reason to poll fast.
    private var pollInterval: Duration {
        LiveRefreshCadence.watching(proposals[.open] ?? [])
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
            try await pollTick()
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
                    viewerChoice: votes.choice(for: proposal, viewerId: service.viewerId),
                    highlight: proposal.id == highlightProposalId
                )
            }
        }
    }

    /// Background re-read. Writes only what changed, and never the failure state: a poll that
    /// throws leaves the cards as they are and lets the loop back off.
    ///
    /// Open is re-read on every tick. Closed only when it can have changed or is the tab being
    /// read, so an open vote no longer drags the cabal's whole history down the wire every five
    /// seconds for nothing.
    private func pollTick() async throws {
        guard votingIDs.isEmpty else { return }
        let generation = loadGeneration
        let previousOpen = proposals[.open]
        let open = try await service.listProposals(groupId: groupId, tab: .open)
        guard generation == loadGeneration, votingIDs.isEmpty, !Task.isCancelled else { return }
        // Remember the departure before acting on it. The fresh Open list is about to become
        // `previousOpen`, so if the closed read below throws, the next tick can no longer see that
        // a proposal left, and the Closed tab and its count stay wrong until the member switches
        // tabs or pulls to refresh.
        if ProposalFeedPolling.openListLostAProposal(previousOpen: previousOpen, freshOpen: open) {
            closedIsStale = true
        }
        apply(open, to: .open)

        guard ProposalFeedPolling.shouldReloadClosed(
            visibleTab: tab,
            hasClosed: proposals[.closed] != nil,
            closedIsStale: closedIsStale
        ) else { return }
        let closed = try await service.listProposals(groupId: groupId, tab: .closed)
        guard generation == loadGeneration, votingIDs.isEmpty, !Task.isCancelled else { return }
        apply(closed, to: .closed)
        closedIsStale = false
    }

    private func apply(_ fresh: [ProposalDTO], to tab: ProposalFeedTab) {
        QuietUpdate.apply(fresh, over: proposals[tab]) { value in
            withAnimation(reduceMotion ? nil : .snappy) { proposals[tab] = value }
        }
        // Only write when it changes: a mutating call on @State invalidates the view either way.
        if failedTabs.contains(tab) { failedTabs.remove(tab) }
    }

    private func load(_ tab: ProposalFeedTab) async {
        loadGeneration += 1
        do {
            let loaded = try await service.listProposals(groupId: groupId, tab: tab)
            withAnimation(reduceMotion ? nil : .snappy) {
                proposals[tab] = loaded
            }
            failedTabs.remove(tab)
            if tab == .closed { closedIsStale = false }
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
                votes.record(choice, for: proposal.id, viewerId: service.viewerId)
            }
        }
        // A vote can close the proposal (threshold reached), so refresh both tabs from the server.
        await load(.open)
        if result.succeeded {
            await load(.closed)
        }
    }
}

/// When a background tick needs the Closed tab as well as the Open one.
///
/// Closed rows are the cabal's whole history and the list has no cursor, so re-reading them every
/// five seconds while a vote is open costs data and a fan-out of backend queries for nothing. They
/// are worth re-reading when a proposal has just left the Open list — it settled, so the Closed tab
/// and its count are now wrong — and while Closed is the tab the member is reading, where another
/// member's deciding vote or a new comment count shows up.
enum ProposalFeedPolling {
    /// A proposal that was open on the last tick is not on this one: it settled, so the Closed tab
    /// and its count are now wrong. Split out from the decision below so the call site can record
    /// it before it acts on it — the signal only exists for the one tick that sees it.
    static func openListLostAProposal(previousOpen: [ProposalDTO]?, freshOpen: [ProposalDTO]) -> Bool {
        guard let previousOpen else { return false }
        let fresh = Set(freshOpen.map(\.id))
        return previousOpen.contains { !fresh.contains($0.id) }
    }

    static func shouldReloadClosed(visibleTab: ProposalFeedTab, hasClosed: Bool, closedIsStale: Bool) -> Bool {
        if !hasClosed { return true }
        if visibleTab == .closed { return true }
        return closedIsStale
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
