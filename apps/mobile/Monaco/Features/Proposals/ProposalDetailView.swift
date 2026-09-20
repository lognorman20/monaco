import MonacoCore
import SwiftUI

/// Proposal detail: the card as a header with voting, the reason, a Voting → Buying → Done tracker,
/// ballots, and the discussion with a composer pinned to the bottom.
///
/// While the proposal is open for voting or its swap is in flight, the screen quietly re-reads
/// the proposal and its discussion every 5 seconds, so another member's vote or comment shows up
/// without a pull. It drops to the resting cadence once the proposal is settled.
struct ProposalDetailView: View {
    let service: ProposalFeedService
    let proposalId: String

    @State private var proposal: ProposalDTO?
    @State private var loadFailed = false
    @State private var isVoting = false
    /// Ballot cast from this screen, so the header flips to "You voted yes" before the reload lands.
    @State private var localChoice: String?
    @State private var comments: [ProposalCommentDTO] = []
    @State private var commentsLoading = true
    @State private var commentsError: String?
    @State private var draft = ""
    @State private var replyTarget: ProposalCommentDTO?
    @State private var isPosting = false
    @State private var toast: MonacoToast?

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(service: ProposalFeedService, proposalId: String, initialProposal: ProposalDTO? = nil) {
        self.service = service
        self.proposalId = proposalId
        _proposal = State(initialValue: initialProposal)
    }

    /// Entry point for screens outside the feed (Home "Needs your vote", activity rows).
    init(auth: PrivyAuthService, proposalId: String) {
        self.init(service: LiveProposalFeedService(auth: auth), proposalId: proposalId)
    }

    private var viewerChoice: String? {
        localChoice ?? proposal?.viewerChoice(viewerId: service.viewerId)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                if let proposal {
                    ProposalCardView(
                        proposal: proposal,
                        isVoting: isVoting,
                        onVote: { choice in Task { await vote(choice) } },
                        viewerChoice: viewerChoice,
                        showsChrome: false
                    )
                    reasonSection(proposal)
                    trackerSection(proposal)
                    ballotsSection(proposal)
                    agentSection(proposal)
                    CommentThreadView(
                        comments: comments,
                        isLoading: commentsLoading,
                        errorMessage: commentsError,
                        onRetry: { Task { await loadComments() } },
                        onReply: { replyTarget = $0 }
                    )
                } else if loadFailed {
                    EmptyState(title: ProposalFeedCopy.detailLoadFailed, actionTitle: ProposalFeedCopy.tryAgain) {
                        Task { await loadProposal() }
                    }
                    .padding(.top, MonacoTheme.Space.xl)
                } else {
                    ProposalCardSkeleton()
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.l)
        }
        .scrollDismissesKeyboard(.interactively)
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .safeAreaInset(edge: .bottom) {
            if proposal != nil {
                CommentComposer(
                    text: $draft,
                    replyTarget: replyTarget,
                    isPosting: isPosting,
                    onCancelReply: { replyTarget = nil },
                    onPost: { body in Task { await postComment(body) } }
                )
            }
        }
        .monacoToast($toast, bottomInset: 72)
        .navigationTitle(proposal.map(ProposalFeedCopy.title(for:)) ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: proposalId) {
            // Feed items carry no ballots; always fetch detail so votes and canVote are current.
            async let detail: Void = loadProposal()
            async let thread: Void = loadComments()
            _ = await (detail, thread)
        }
        .pollWhileVisible(every: LiveRefreshCadence.watching(proposal.map { [$0] } ?? [])) {
            try await pollProposalAndComments()
        }
        .onChange(of: proposal.flatMap(ProposalExecutionStage.of)) { old, new in
            if old == .executing, new == .done { Haptics.success() }
        }
        .refreshable {
            await loadProposal()
            await loadComments()
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("proposal-detail")
    }

    // MARK: Sections

    @ViewBuilder
    private func reasonSection(_ proposal: ProposalDTO) -> some View {
        if let thesis = proposal.thesis?.trimmingCharacters(in: .whitespacesAndNewlines), !thesis.isEmpty {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                MonacoSectionHeader(ProposalFeedCopy.reasonTitle(for: proposal))
                ProposalQuoteBlock(text: thesis)
                    .textSelection(.enabled)
                    .accessibilityIdentifier("proposal-detail-thesis")
            }
        }
    }

    @ViewBuilder
    private func trackerSection(_ proposal: ProposalDTO) -> some View {
        if let stage = ProposalExecutionStage.of(proposal) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                MonacoSectionHeader(ProposalFeedCopy.statusTitle)
                ProposalExecutionTracker(stage: stage, isSell: proposal.isSell)
                switch stage {
                case .failed:
                    Text(ProposalFeedCopy.executionFailed(isSell: proposal.isSell))
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.loss)
                case .executing:
                    Text(ProposalFeedCopy.executionPending(isSell: proposal.isSell))
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.muted)
                case .done:
                    if let executedAt = proposal.execution?.executedAt, let date = ProposalTimeFormatter.parse(executedAt) {
                        Text(date.formatted(date: .abbreviated, time: .shortened))
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.muted)
                    }
                case .voting:
                    EmptyView()
                }
            }
            .accessibilityIdentifier("proposal-detail-buy-status")
        }
    }

    @ViewBuilder
    private func ballotsSection(_ proposal: ProposalDTO) -> some View {
        let votes = proposal.votes ?? []
        let viewerWaiting = proposal.showsVoteActions && viewerChoice == nil
        let progress = proposal.voteSummary.map(ProposalVoteProgress.init(summary:))
        // Ballots we can name, the viewer if they still owe a vote, and a count for everyone else.
        let othersWaiting = max((progress?.pendingCount ?? 0) - (viewerWaiting ? 1 : 0), 0)
        if !votes.isEmpty || viewerWaiting || (proposal.isOpen && othersWaiting > 0) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                MonacoSectionHeader(ProposalFeedCopy.votesTitle)
                MonacoGroupedList {
                    ForEach(Array(votes.enumerated()), id: \.element.id) { index, vote in
                        BallotRow(
                            name: vote.displayName,
                            choice: vote.choice.lowercased() == "yes" ? ProposalFeedCopy.ballotYes : ProposalFeedCopy.ballotNo,
                            isNo: vote.choice.lowercased() != "yes",
                            isLast: index == votes.count - 1 && !viewerWaiting && !(proposal.isOpen && othersWaiting > 0)
                        )
                    }
                    if viewerWaiting {
                        BallotRow(name: "You", choice: ProposalFeedCopy.ballotWaiting, isWaiting: true, isLast: !(proposal.isOpen && othersWaiting > 0))
                    }
                    if proposal.isOpen, othersWaiting > 0 {
                        BallotRow(
                            name: othersWaiting == 1 ? "1 more member" : "\(othersWaiting) more members",
                            choice: ProposalFeedCopy.ballotWaiting,
                            isWaiting: true,
                            showsAvatar: false,
                            isLast: true
                        )
                    }
                }
            }
            .accessibilityIdentifier("proposal-detail-votes")
        }
    }

    @ViewBuilder
    private func agentSection(_ proposal: ProposalDTO) -> some View {
        if proposal.resolvedKind == "add_agent", let key = proposal.mintedAgentKey, !key.isEmpty {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                MonacoSectionHeader(ProposeFlowCopy.copyKey)
                AgentKeyRevealView(apiKey: key) {
                    toast = MonacoToast(message: ProposeFlowCopy.keyCopied, isSuccess: true)
                }
            }
            .accessibilityIdentifier("proposal-detail-agent")
        }
    }

    // MARK: Loading

    private func loadProposal() async {
        do {
            let loaded = try await service.proposal(id: proposalId)
            withAnimation(reduceMotion ? nil : .snappy) { proposal = loaded }
            loadFailed = false
        } catch is CancellationError {
            return
        } catch {
            if error.isRequestCancellation { return }
            if proposal == nil { loadFailed = true }
        }
    }

    /// Background re-read of the proposal and its thread. Writes only what changed and never a
    /// loading or error state; a throw leaves the screen as it is and lets the loop back off.
    private func pollProposalAndComments() async throws {
        guard !isVoting, !isPosting else { return }
        async let detail = service.proposal(id: proposalId)
        async let thread = service.comments(proposalId: proposalId)
        let loaded = try await detail
        let loadedComments = try? await thread
        // A vote or comment the member started while this was in flight reloads on its own.
        guard !isVoting, !isPosting, !Task.isCancelled else { return }
        QuietUpdate.apply(loaded, over: proposal) { value in
            withAnimation(reduceMotion ? nil : .snappy) { proposal = value }
        }
        if let loadedComments {
            QuietUpdate.apply(loadedComments, over: comments) { comments = $0 }
            if commentsError != nil { commentsError = nil }
        }
    }

    private func loadComments() async {
        commentsLoading = true
        commentsError = nil
        defer { commentsLoading = false }
        do {
            comments = try await service.comments(proposalId: proposalId)
        } catch is CancellationError {
            return
        } catch {
            if error.isRequestCancellation { return }
            commentsError = ProposalFeedCopy.commentsLoadFailed
        }
    }

    private func vote(_ choice: ProposalVoteChoice) async {
        guard !isVoting else { return }
        isVoting = true
        defer { isVoting = false }
        let result = await ProposalVoting.cast(choice, proposalId: proposalId, service: service)
        toast = result.toast
        if result.succeeded {
            Haptics.success()
            withAnimation(reduceMotion ? nil : .spring(response: 0.35, dampingFraction: 0.7)) {
                localChoice = choice.rawValue
            }
        }
        await loadProposal()
    }

    private func postComment(_ body: String) async {
        guard !isPosting else { return }
        isPosting = true
        defer { isPosting = false }
        let parent = replyTarget
        do {
            _ = try await service.postComment(proposalId: proposalId, body: body, parentId: parent?.id)
            draft = ""
            replyTarget = nil
            Haptics.success()
            toast = MonacoToast(
                message: parent == nil ? ProposalFeedCopy.commentPosted : ProposalFeedCopy.replyPosted,
                isSuccess: true
            )
            await loadComments()
            await loadProposal()
        } catch {
            toast = MonacoToast(message: ProposalFeedErrorCopy.comment(error))
        }
    }
}

/// Voting → Buying → Done as three connected steps. A failed swap stops on the middle step in loss.
struct ProposalExecutionTracker: View {
    let stage: ProposalExecutionStage
    let isSell: Bool

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var steps: [String] {
        ProposalFeedCopy.trackerSteps(isSell: isSell)
    }

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            ForEach(Array(steps.enumerated()), id: \.offset) { index, title in
                VStack(spacing: MonacoTheme.Space.s) {
                    HStack(spacing: 0) {
                        connector(visible: index > 0, reached: index <= stage.stepIndex)
                        node(index)
                        connector(visible: index < steps.count - 1, reached: index < stage.stepIndex)
                    }
                    Text(title)
                        .font(MonacoTheme.Typo.caption.weight(index == stage.stepIndex ? .semibold : .regular))
                        .foregroundStyle(labelColor(index))
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                }
                .frame(maxWidth: .infinity)
            }
        }
        .animation(reduceMotion ? nil : .spring(response: 0.4, dampingFraction: 0.8), value: stage)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityText)
        .accessibilityIdentifier("proposal-tracker")
    }

    private func node(_ index: Int) -> some View {
        let reached = index <= stage.stepIndex
        let failedHere = stage == .failed && index == stage.stepIndex
        let current = index == stage.stepIndex && stage != .done
        return ZStack {
            Circle()
                .fill(reached ? (failedHere ? MonacoTheme.loss : MonacoTheme.ink) : MonacoTheme.surfaceSunken)
            if failedHere {
                Image(systemName: "xmark")
                    .font(.system(size: 10, weight: .bold))
                    .foregroundStyle(MonacoTheme.primaryButtonLabel)
            } else if reached && !current {
                Image(systemName: "checkmark")
                    .font(.system(size: 10, weight: .bold))
                    .foregroundStyle(MonacoTheme.primaryButtonLabel)
            } else if current {
                Circle()
                    .fill(MonacoTheme.primaryButtonLabel)
                    .frame(width: 8, height: 8)
                    .modifier(TrackerPulse(active: stage == .executing && !reduceMotion))
            }
        }
        .frame(width: 22, height: 22)
    }

    private func connector(visible: Bool, reached: Bool) -> some View {
        Rectangle()
            .fill(visible ? (reached ? MonacoTheme.ink : MonacoTheme.hairline) : Color.clear)
            .frame(height: 2)
            .frame(maxWidth: .infinity)
    }

    private func labelColor(_ index: Int) -> Color {
        if stage == .failed && index == stage.stepIndex { return MonacoTheme.loss }
        return index <= stage.stepIndex ? MonacoTheme.ink : MonacoTheme.tertiaryText
    }

    private var accessibilityText: String {
        let current = steps[min(stage.stepIndex, steps.count - 1)]
        switch stage {
        case .failed: return "\(current) failed"
        case .done: return "Done"
        default: return "\(current), step \(stage.stepIndex + 1) of \(steps.count)"
        }
    }
}

/// Slow breathing on the current step while the swap runs.
private struct TrackerPulse: ViewModifier {
    let active: Bool

    func body(content: Content) -> some View {
        content.opacityLoop(to: 0.35, halfPeriod: 0.8, active: active)
    }
}

/// One ballot: avatar, name, Yes / No / Waiting.
private struct BallotRow: View {
    let name: String
    let choice: String
    var isNo = false
    var isWaiting = false
    var showsAvatar = true
    var isLast = false

    var body: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            if showsAvatar {
                MonacoAvatar(photoURL: nil, displayName: name, size: 28)
            } else {
                Circle()
                    .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
                    .frame(width: 28, height: 28)
            }
            Text(name)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(isWaiting && !showsAvatar ? MonacoTheme.muted : MonacoTheme.ink)
                .lineLimit(1)
            Spacer(minLength: MonacoTheme.Space.s)
            Text(choice)
                .font(MonacoTheme.Typo.callout.weight(.semibold))
                .foregroundStyle(isWaiting ? MonacoTheme.tertiaryText : (isNo ? MonacoTheme.loss : MonacoTheme.ink))
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .frame(minHeight: 52)
        .overlay(alignment: .bottom) {
            if !isLast {
                Rectangle().fill(MonacoTheme.hairline).frame(height: 1).padding(.leading, 56)
            }
        }
        .accessibilityElement(children: .combine)
    }
}
