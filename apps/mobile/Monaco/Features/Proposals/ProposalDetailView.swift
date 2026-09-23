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
    @State private var comments: [ProposalCommentDTO] = []
    /// The thread in display order, built once per change of `comments` rather than per render.
    @State private var commentRows: [ProposalCommentThreadRow] = []
    @State private var commentsLoading = true
    @State private var commentsError: String?
    @State private var replyTarget: ProposalCommentDTO?
    @State private var isPosting = false
    /// The comment this member just posted, so the thread can scroll to it.
    @State private var postedCommentId: String?
    /// The comment to scroll to once the composer has dropped focus. Held back so the scroll is
    /// not issued into a layout the keyboard is still about to resize.
    @State private var pendingScrollCommentId: String?
    @State private var toast: MonacoToast?

    private let votes = ProposalVoteLedger.shared

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.cabalTint) private var cabalTint
    /// The viewer's cabals, for the resolved tint when the surface did not declare one.
    @Environment(AppSessionStore.self) private var session: AppSessionStore?

    init(service: ProposalFeedService, proposalId: String, initialProposal: ProposalDTO? = nil) {
        self.service = service
        self.proposalId = proposalId
        _proposal = State(initialValue: initialProposal)
    }

    /// Entry point for screens outside the feed (Home "Needs your vote", activity rows).
    init(auth: DynamicAuthService, proposalId: String) {
        self.init(service: LiveProposalFeedService(auth: auth), proposalId: proposalId)
    }

    private var viewerChoice: String? {
        proposal.flatMap { votes.choice(for: $0, viewerId: service.viewerId) }
    }

    /// The proposing cabal's colour: the one the surface set, else the resolved one for the
    /// payload's group. Nil when neither is known — the screen then ships without a tint rather
    /// than picking some cabal's colour for it.
    ///
    /// Resolved, never hashed directly: a cabal that is sage on its feed has to be sage on the
    /// proposal you opened from that feed.
    private func tint(for proposal: ProposalDTO) -> MonacoTheme.CabalTint? {
        if let cabalTint { return cabalTint }
        guard let groupId = proposal.groupId, !groupId.isEmpty else { return nil }
        return ProposalCabalTint.tint(forGroupId: groupId, in: session)
    }

    var body: some View {
        ScrollViewReader { proxy in
            detail
                // A posted comment lands at the bottom of a thread that is usually below the fold,
                // behind the pinned composer. Bring it into view so the member sees what they said.
                .onChange(of: postedCommentId) { _, id in
                    guard let id else { return }
                    withAnimation(MonacoMotion.settle.reduced(reduceMotion)) { proxy.scrollTo(id, anchor: .bottom) }
                }
        }
    }

    private var detail: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.section) {
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
                        rows: commentRows,
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
                    replyTarget: replyTarget,
                    isPosting: isPosting,
                    onCancelReply: { replyTarget = nil },
                    onPost: { body in await postComment(body) },
                    onDidStandDown: {
                        postedCommentId = pendingScrollCommentId
                        pendingScrollCommentId = nil
                    }
                )
            }
        }
        .monacoToast($toast, placement: .aboveBottomCTA)
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
            VStack(alignment: .leading, spacing: MonacoTheme.Space.headerToContent) {
                MonacoSectionHeader(ProposalFeedCopy.reasonTitle(for: proposal))
                ProposalQuoteBlock(text: thesis, tint: tint(for: proposal))
                    .textSelection(.enabled)
                    .accessibilityIdentifier("proposal-detail-thesis")
            }
        }
    }

    @ViewBuilder
    private func trackerSection(_ proposal: ProposalDTO) -> some View {
        if let stage = ProposalExecutionStage.of(proposal) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.headerToContent) {
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
            VStack(alignment: .leading, spacing: MonacoTheme.Space.headerToContent) {
                MonacoSectionHeader(ProposalFeedCopy.votesTitle)
                MonacoGroupedList {
                    ForEach(Array(votes.enumerated()), id: \.element.id) { index, vote in
                        BallotRow(
                            voterId: vote.voterId,
                            name: vote.displayName,
                            choice: vote.choice.lowercased() == "yes" ? ProposalFeedCopy.ballotYes : ProposalFeedCopy.ballotNo,
                            isNo: vote.choice.lowercased() != "yes",
                            isLast: index == votes.count - 1 && !viewerWaiting && !(proposal.isOpen && othersWaiting > 0)
                        )
                    }
                    if viewerWaiting {
                        BallotRow(voterId: "you", name: "You", choice: ProposalFeedCopy.ballotWaiting, isWaiting: true, isLast: !(proposal.isOpen && othersWaiting > 0))
                    }
                    if proposal.isOpen, othersWaiting > 0 {
                        BallotRow(
                            voterId: "others",
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
            VStack(alignment: .leading, spacing: MonacoTheme.Space.headerToContent) {
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
            withAnimation(MonacoMotion.glide.reduced(reduceMotion)) { proposal = loaded }
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
            withAnimation(MonacoMotion.glide.reduced(reduceMotion)) { proposal = value }
        }
        if let loadedComments {
            QuietUpdate.apply(loadedComments, over: comments) { setComments($0) }
            if commentsError != nil { commentsError = nil }
        }
    }

    private func loadComments() async {
        commentsLoading = true
        commentsError = nil
        defer { commentsLoading = false }
        do {
            setComments(try await service.comments(proposalId: proposalId))
        } catch is CancellationError {
            return
        } catch {
            if error.isRequestCancellation { return }
            commentsError = ProposalFeedCopy.commentsLoadFailed
        }
    }

    /// The one place comments are stored, so the thread is threaded exactly once per change.
    private func setComments(_ loaded: [ProposalCommentDTO]) {
        comments = loaded
        commentRows = ProposalCommentThread.rows(from: loaded)
    }

    private func vote(_ choice: ProposalVoteChoice) async {
        guard !isVoting else { return }
        isVoting = true
        defer { isVoting = false }
        let result = await ProposalVoting.cast(choice, proposalId: proposalId, service: service)
        toast = result.toast
        if result.succeeded {
            Haptics.success()
            withAnimation(MonacoMotion.settle.reduced(reduceMotion)) {
                votes.record(choice, for: proposalId, viewerId: service.viewerId)
            }
        }
        await loadProposal()
    }

    /// True when the comment was accepted, which is the composer's cue to clear and stand down.
    ///
    /// It answers as soon as the server has taken the comment. Re-reading the thread and the
    /// proposal is how the server's copy of both catches up; none of it decides whether the post
    /// was accepted, so the member does not watch a spinner with the keyboard over the thread for
    /// two more round trips to find out. The comment is shown from the response in the meantime.
    private func postComment(_ body: String) async -> Bool {
        guard !isPosting else { return false }
        isPosting = true
        let parent = replyTarget
        do {
            let posted = try await service.postComment(proposalId: proposalId, body: body, parentId: parent?.id)
            isPosting = false
            replyTarget = nil
            Haptics.success()
            toast = MonacoToast(
                message: parent == nil ? ProposalFeedCopy.commentPosted : ProposalFeedCopy.replyPosted,
                isSuccess: true
            )
            setComments(comments + [posted])
            // Not the scroll target yet. The composer still has focus at this point, so the
            // keyboard — and with it the bottom safe-area inset the thread is laid out against —
            // is about to change. Scrolling now aims at a layout that no longer exists a frame
            // later. `onDidStandDown` publishes it once the composer has let go.
            pendingScrollCommentId = posted.id
            Task {
                await loadComments()
                await loadProposal()
            }
            return true
        } catch {
            isPosting = false
            toast = MonacoToast(message: ProposalFeedErrorCopy.comment(error))
            return false
        }
    }
}

/// Voting → Buying → Done as three connected steps, drawn as a rail: the steps behind you are
/// brand, the step you are on carries a pulsing dot, and a failed swap stops on the middle step
/// in `loss`.
///
/// **Brand, not ink.** The rail says where this proposal is, and "where you are" is the one thing
/// in the app that is allowed to be blue without being tappable — it is the same blue the Vote
/// button carries, on the thing that button was pressed on. It is emphatically not green: a
/// proposal that passed is a decision, not a profit.
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
        .animation(MonacoMotion.settle.reduced(reduceMotion), value: stage)
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
                .fill(reached ? (failedHere ? MonacoTheme.loss : MonacoTheme.brand) : MonacoTheme.fillQuiet)
            if failedHere {
                Image(systemName: "xmark")
                    .font(.system(size: 10, weight: .bold))
                    .foregroundStyle(MonacoTheme.onBrand)
            } else if reached && !current {
                Image(systemName: "checkmark")
                    .font(.system(size: 10, weight: .bold))
                    .foregroundStyle(MonacoTheme.onBrand)
            } else if current {
                Circle()
                    .fill(MonacoTheme.onBrand)
                    .frame(width: 8, height: 8)
                    .modifier(TrackerPulse(active: stage == .executing && !reduceMotion))
            }
        }
        .frame(width: 22, height: 22)
    }

    private func connector(visible: Bool, reached: Bool) -> some View {
        Rectangle()
            .fill(visible ? (reached ? MonacoTheme.brand : MonacoTheme.line) : Color.clear)
            .frame(height: 2)
            .frame(maxWidth: .infinity)
    }

    private func labelColor(_ index: Int) -> Color {
        if stage == .failed && index == stage.stepIndex { return MonacoTheme.loss }
        return index <= stage.stepIndex ? MonacoTheme.fgPrimary : MonacoTheme.fgSubtle
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

/// One ballot: the voter's face carrying how they voted, their name, and Yes / No / Waiting.
///
/// The face is a `MonacoVoteFace`, the same component the tally above draws, so a member reading
/// the list and a member reading the tally are looking at the same encoding rather than at an
/// avatar in one place and a ringed avatar in the other. The aggregate row ("3 more members") has
/// no face at all: it is a count, and a count is not a person.
private struct BallotRow: View {
    let voterId: String
    let name: String
    let choice: String
    var isNo = false
    var isWaiting = false
    var showsAvatar = true
    var isLast = false

    private var state: ProposalVoteDot {
        if isWaiting { return .pending }
        return isNo ? .no : .yes
    }

    var body: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            if showsAvatar {
                MonacoVoteFace(
                    vote: MonacoVote(
                        id: voterId,
                        state: state,
                        face: isWaiting ? nil : MonacoFace(id: voterId, displayName: name)
                    ),
                    size: 28
                )
            } else {
                Circle()
                    .strokeBorder(MonacoTheme.line, lineWidth: 1)
                    .frame(width: 28, height: 28)
            }
            Text(name)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(isWaiting && !showsAvatar ? MonacoTheme.fgMuted : MonacoTheme.fgPrimary)
                .lineLimit(1)
            Spacer(minLength: MonacoTheme.Space.s)
            Text(choice)
                .font(MonacoTheme.Typo.callout.weight(.semibold))
                .foregroundStyle(choiceColor)
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .frame(minHeight: 52)
        .overlay(alignment: .bottom) {
            if !isLast {
                Rectangle().fill(MonacoTheme.line).frame(height: 1).padding(.leading, 56)
            }
        }
        .accessibilityElement(children: .combine)
    }

    /// Brand is "tap" and this row is not tappable, so the word "Yes" is plain. The ring on the
    /// face beside it already says how the ballot was cast, in the same encoding the tally uses,
    /// and it says it without spending the one colour that means a control.
    private var choiceColor: Color {
        if isWaiting { return MonacoTheme.fgSubtle }
        return isNo ? MonacoTheme.loss : MonacoTheme.fgPrimary
    }
}
