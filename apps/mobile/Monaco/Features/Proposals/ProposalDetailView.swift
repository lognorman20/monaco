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
    /// The header says what this is; the bar only repeats it once the header has scrolled away.
    @State private var headerScrolledAway = false

    private let votes = ProposalVoteLedger.shared

    /// How far the header scrolls before the bar takes the title: past the ticker row.
    private static let titleThreshold: CGFloat = 64

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
        proposal.flatMap { votes.choice(for: $0, viewerId: service.viewerId) }
    }

    var body: some View {
        ScrollViewReader { proxy in
            detail
                // A posted comment lands at the bottom of a thread that is usually below the fold,
                // behind the pinned composer. Bring it into view so the member sees what they said.
                .onChange(of: postedCommentId) { _, id in
                    guard let id else { return }
                    withAnimation(reduceMotion ? nil : .snappy) { proxy.scrollTo(id, anchor: .bottom) }
                }
        }
    }

    private var detail: some View {
        ScrollView {
            // No horizontal padding on the stack: the ballots and the discussion are ruled lists
            // that run edge to edge, and every section insets its own header and words.
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                if let proposal {
                    ProposalCardView(
                        proposal: proposal,
                        isVoting: isVoting,
                        onVote: { choice in Task { await vote(choice) } },
                        viewerChoice: viewerChoice,
                        viewerId: service.viewerId,
                        showsChrome: false
                    )
                    .padding(.horizontal, MonacoTheme.Space.m)
                    // lane: notifications
                    if ProposalRemindButton.shows(proposal: proposal, service: service, viewerChoice: viewerChoice) {
                        ProposalRemindButton(proposal: proposal, service: service, viewerChoice: viewerChoice, toast: $toast)
                            .padding(.horizontal, MonacoTheme.Space.m)
                            .padding(.top, -MonacoTheme.Space.m)
                    }
                    reasonSection(proposal)
                    trackerSection(proposal)
                    ballotsSection(proposal)
                    agentSection(proposal)
                    CommentThreadView(
                        rows: commentRows,
                        isLoading: commentsLoading,
                        errorMessage: commentsError,
                        emptyMessage: ProposalDiscussionCopy.emptyThread(for: proposal),
                        onRetry: { Task { await loadComments() } },
                        onReply: { replyTarget = $0 }
                    )
                } else if loadFailed {
                    EmptyState(title: ProposalFeedCopy.detailLoadFailed, actionTitle: ProposalFeedCopy.tryAgain) {
                        Task { await loadProposal() }
                    }
                    .padding(.top, MonacoTheme.Space.xl)
                } else {
                    ProposalDetailSkeleton()
                }
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.l)
        }
        .scrollDismissesKeyboard(.interactively)
        .onScrollGeometryChange(for: Bool.self) { geometry in
            geometry.contentOffset.y + geometry.contentInsets.top > Self.titleThreshold
        } action: { _, scrolledAway in
            headerScrolledAway = scrolledAway
        }
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
        .monacoToast($toast, bottomInset: 72)
        .navigationTitle(headerScrolledAway ? proposal.map(ProposalFeedCopy.title(for:)) ?? "" : "")
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
                MonacoSectionHeader(ProposalDiscussionCopy.reasonTitle(for: proposal))
                ProposalQuoteBlock(text: thesis)
                    .textSelection(.enabled)
                    .accessibilityIdentifier("proposal-detail-thesis")
            }
            .padding(.horizontal, MonacoTheme.Space.m)
        }
    }

    @ViewBuilder
    private func trackerSection(_ proposal: ProposalDTO) -> some View {
        if let stage = ProposalExecutionStage.of(proposal) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                MonacoSectionHeader(ProposalFeedCopy.statusTitle)
                ProposalExecutionTracker(stage: stage, isSell: proposal.isSell)
                    .padding(.top, MonacoTheme.Space.xs)
                switch stage {
                case .failed:
                    Text(ProposalFeedCopy.executionFailed(isSell: proposal.isSell))
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.loss)
                        .fixedSize(horizontal: false, vertical: true)
                case .executing:
                    Text(ProposalFeedCopy.executionPending(isSell: proposal.isSell))
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                case .done:
                    // When it landed, as the stamp it is. The chip at the top of the screen
                    // already says "Bought"; this says when.
                    if let executedAt = proposal.execution?.executedAt, let date = ProposalTimeFormatter.parse(executedAt) {
                        Text(date.formatted(date: .abbreviated, time: .shortened))
                            .font(MonacoTheme.Typo.stamp)
                            .foregroundStyle(MonacoTheme.tertiaryText)
                            .frame(maxWidth: .infinity, alignment: .trailing)
                            .transition(.opacity)
                    }
                case .voting:
                    EmptyView()
                }
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            .accessibilityIdentifier("proposal-detail-buy-status")
        }
    }

    /// Who is behind this, by name: every ballot the payload names, the viewer if they still owe
    /// one, and a count for everyone else. A ruled list on the paper, the viewer's own line washed
    /// the way the leaderboard washes it.
    @ViewBuilder
    private func ballotsSection(_ proposal: ProposalDTO) -> some View {
        let lines = ProposalBallotList.lines(for: proposal, viewerId: service.viewerId, viewerChoice: viewerChoice)
        if !lines.isEmpty {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                MonacoSectionHeader(ProposalFeedCopy.votesTitle)
                    .padding(.horizontal, MonacoTheme.Space.m)
                MonacoGroupedList {
                    ForEach(lines) { line in
                        BallotRow(line: line, isLast: line.id == lines.last?.id)
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
                MonacoSectionHeader(ProposeFlowCopy.botKeyTitle)
                AgentKeyRevealView(apiKey: key) { message in
                    toast = MonacoToast(message: message, isSuccess: true)
                }
            }
            .padding(.horizontal, MonacoTheme.Space.m)
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
            withAnimation(reduceMotion ? nil : .easeInOut(duration: 0.2)) {
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


/// Voting → Buying → Done as three nodes on a rule. Steps behind are filled with a check, the
/// step in play is an ink ring with a dot at its centre (breathing while the swap runs), and steps
/// ahead are open rings. A failed swap stops on the middle step, filled in loss.
///
/// When a passed buy lands, the last node fills and the rule into it goes to ink: one settled
/// moment, a quarter of a second, and the success haptic the screen already plays. No bounce.
struct ProposalExecutionTracker: View {
    let stage: ProposalExecutionStage
    let isSell: Bool

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private static let nodeSize: CGFloat = 20
    private static let ruleWeight: CGFloat = 2

    private var steps: [String] {
        ProposalFeedCopy.trackerSteps(isSell: isSell)
    }

    var body: some View {
        // The rule runs the width of the section: the first step sits on the left edge where
        // every header and row on the page starts, the last on the right, the middle between.
        VStack(spacing: MonacoTheme.Space.s) {
            HStack(spacing: 0) {
                ForEach(steps.indices, id: \.self) { index in
                    if index > 0 {
                        segment(reached: index <= stage.stepIndex)
                    }
                    node(ProposalTrackerNode.of(index: index, stage: stage))
                }
            }
            ZStack {
                ForEach(Array(steps.enumerated()), id: \.offset) { index, title in
                    Text(title)
                        .font(labelFont(index))
                        .foregroundStyle(labelColor(index))
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                        .frame(maxWidth: .infinity, alignment: labelAlignment(index))
                }
            }
        }
        .animation(reduceMotion ? nil : .easeInOut(duration: 0.25), value: stage)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityText)
        .accessibilityIdentifier("proposal-tracker")
    }

    @ViewBuilder
    private func node(_ state: ProposalTrackerNode) -> some View {
        ZStack {
            switch state {
            case .done:
                Circle().fill(MonacoTheme.brandFill)
                glyph("checkmark")
            case .failed:
                Circle().fill(MonacoTheme.loss)
                glyph("xmark")
            case .current:
                Circle().fill(MonacoTheme.canvas)
                Circle().strokeBorder(MonacoTheme.ink, lineWidth: Self.ruleWeight)
                Circle()
                    .fill(MonacoTheme.ink)
                    .frame(width: 8, height: 8)
                    .modifier(TrackerPulse(active: stage == .executing && !reduceMotion))
            case .ahead:
                Circle().fill(MonacoTheme.canvas)
                Circle().strokeBorder(MonacoTheme.hairline, lineWidth: Self.ruleWeight)
            }
        }
        .frame(width: Self.nodeSize, height: Self.nodeSize)
    }

    private func glyph(_ name: String) -> some View {
        Image(systemName: name)
            .font(.system(size: 9, weight: .bold))
            .foregroundStyle(MonacoTheme.onBrand)
    }

    /// The stretch of rule leading into a step: ink once the proposal has reached it.
    private func segment(reached: Bool) -> some View {
        Rectangle()
            .fill(reached ? MonacoTheme.ink : MonacoTheme.hairline)
            .frame(height: Self.ruleWeight)
            .frame(maxWidth: .infinity)
    }

    /// Each label sits under its node: the first flush left, the last flush right.
    private func labelAlignment(_ index: Int) -> Alignment {
        if index == 0 { return .leading }
        if index == steps.count - 1 { return .trailing }
        return .center
    }

    private func labelFont(_ index: Int) -> Font {
        index == stage.stepIndex ? MonacoTheme.Typo.captionStrong : MonacoTheme.Typo.caption
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

/// How one tracker node draws for the stage the proposal is at.
enum ProposalTrackerNode: Equatable {
    /// Behind the proposal, or the last step once it is done: filled, with a check.
    case done
    /// Where the proposal is now: an ink ring with a dot.
    case current
    /// The step a swap failed on: filled in loss, with a cross.
    case failed
    /// Not reached yet: an open ring on the rule.
    case ahead

    static func of(index: Int, stage: ProposalExecutionStage) -> ProposalTrackerNode {
        if stage == .failed, index == stage.stepIndex { return .failed }
        if stage == .done, index <= stage.stepIndex { return .done }
        if index < stage.stepIndex { return .done }
        if index == stage.stepIndex { return .current }
        return .ahead
    }
}

/// Slow breathing on the current step while the swap runs.
private struct TrackerPulse: ViewModifier {
    let active: Bool

    func body(content: Content) -> some View {
        content.opacityLoop(to: 0.35, halfPeriod: 0.8, active: active)
    }
}

/// One line of the Votes list.
struct ProposalBallotLine: Identifiable, Equatable {
    enum Choice: Equatable {
        case yes, no, waiting
    }

    let id: String
    /// "You" for the member reading, the voter's name otherwise, "3 more members" for the rest.
    let name: String
    /// Whose initials the face shows. Empty draws a plain person, which is the viewer before
    /// their ballot names them; nil draws an open ring, which is a seat nobody has filled.
    let faceName: String?
    /// The voter's id, so their face is the same animal as on every board.
    var faceSeed: String? = nil
    let choice: Choice
    /// When the ballot was cast, as the server sent it. Nil for a ballot still to come.
    let castAt: String?
    let isViewer: Bool
}

/// The Votes list, in order: the ballots the payload names, the viewer if they still owe a vote,
/// and one line counting everyone else who does.
enum ProposalBallotList {
    static func lines(for proposal: ProposalDTO, viewerId: String?, viewerChoice: String?) -> [ProposalBallotLine] {
        var lines = (proposal.votes ?? []).map { vote in
            let isViewer = viewerId.map { vote.voterId == $0 } ?? false
            return ProposalBallotLine(
                id: "ballot-\(vote.voterId)",
                name: isViewer ? ProposalDiscussionCopy.you : vote.displayName,
                faceName: vote.displayName,
                faceSeed: vote.voterId,
                choice: vote.choice.lowercased() == ProposalVoteChoice.yes.rawValue ? .yes : .no,
                castAt: vote.castAt,
                isViewer: isViewer
            )
        }
        let viewerWaiting = proposal.showsVoteActions && viewerChoice == nil
        let pending = proposal.voteSummary.map { ProposalVoteProgress(summary: $0).pendingCount } ?? 0
        let othersWaiting = max(pending - (viewerWaiting ? 1 : 0), 0)
        if viewerWaiting {
            lines.append(ProposalBallotLine(
                id: "ballot-viewer-waiting",
                name: ProposalDiscussionCopy.you,
                faceName: "",
                // Their own animal, not the one an empty seed hashes to: the row read as a
                // stranger's face until the ballot named them.
                faceSeed: viewerId,
                choice: .waiting,
                castAt: nil,
                isViewer: true
            ))
        }
        if proposal.isOpen, othersWaiting > 0 {
            lines.append(ProposalBallotLine(
                id: "ballot-others-waiting",
                name: ProposalDiscussionCopy.moreMembers(othersWaiting),
                faceName: nil,
                choice: .waiting,
                castAt: nil,
                isViewer: false
            ))
        }
        return lines
    }
}

/// One ballot: the face, the name with when they voted, and Yes / No / Waiting in the market's
/// voice on the right.
private struct BallotRow: View {
    let line: ProposalBallotLine
    let isLast: Bool

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private static let faceSize: CGFloat = 40

    private var isStacked: Bool { dynamicTypeSize.isAccessibilitySize }

    var body: some View {
        Group {
            if isStacked {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    HStack(spacing: MonacoTheme.Space.sm) {
                        face
                        who
                    }
                    choice
                }
            } else {
                HStack(spacing: MonacoTheme.Space.sm) {
                    face
                    who
                    choice
                }
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, minHeight: 60, alignment: .leading)
        .background(line.isViewer ? MonacoTheme.brandWash : Color.clear)
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule().padding(.leading, isStacked ? MonacoTheme.Space.m : MonacoTheme.Space.m + Self.faceSize + MonacoTheme.Space.sm)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
    }

    @ViewBuilder
    private var face: some View {
        Group {
            if let faceName = line.faceName {
                MonacoAvatar(photoURL: nil, displayName: faceName, size: Self.faceSize, seed: line.faceSeed)
            } else {
                Circle()
                    .strokeBorder(MonacoTheme.hairline, style: StrokeStyle(lineWidth: 1, dash: [3, 3]))
            }
        }
        .frame(width: Self.faceSize, height: Self.faceSize)
    }

    private var age: String? {
        guard let castAt = line.castAt else { return nil }
        let label = RelativeTimeFormatter.label(iso: castAt)
        return label.isEmpty ? nil : label
    }

    private var nameText: Text {
        Text(line.name)
            .font(MonacoTheme.Typo.rowTitle)
            .foregroundStyle(line.faceName == nil ? MonacoTheme.muted : MonacoTheme.ink)
    }

    /// The name with when they voted: side by side, or one run of text once the name can wrap.
    @ViewBuilder
    private var who: some View {
        Group {
            if isStacked {
                ProposalStampedLine.text(nameText, stamp: age ?? "")
                    .fixedSize(horizontal: false, vertical: true)
            } else {
                HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
                    nameText
                        .lineLimit(1)
                    if let age {
                        Text(age)
                            .font(MonacoTheme.Typo.stamp)
                            .foregroundStyle(MonacoTheme.tertiaryText)
                            .fixedSize()
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var choice: some View {
        Text(choiceLabel)
            .font(line.choice == .waiting ? MonacoTheme.Typo.data : MonacoTheme.Typo.dataStrong)
            .foregroundStyle(choiceColor)
            .lineLimit(1)
            .fixedSize()
    }

    private var choiceLabel: String {
        switch line.choice {
        case .yes: ProposalFeedCopy.ballotYes
        case .no: ProposalFeedCopy.ballotNo
        case .waiting: ProposalFeedCopy.ballotWaiting
        }
    }

    private var choiceColor: Color {
        switch line.choice {
        case .yes: MonacoTheme.ink
        case .no: MonacoTheme.loss
        case .waiting: MonacoTheme.tertiaryText
        }
    }

    private var spoken: String {
        "\(line.name), \(choiceLabel)"
    }
}

/// Placeholder in the shape of the proposal screen: the header, the reason, a few ballots.
private struct ProposalDetailSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
            ProposalCardSkeleton(showsChrome: false)
                .padding(.horizontal, MonacoTheme.Space.m)
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                SkeletonBlock(width: 96, height: 20)
                SkeletonBlock(height: 14)
                SkeletonBlock(width: 220, height: 14)
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            VStack(alignment: .leading, spacing: 0) {
                SkeletonBlock(width: 72, height: 20)
                    .padding(.horizontal, MonacoTheme.Space.m)
                    .padding(.bottom, MonacoTheme.Space.s)
                ForEach(0..<3, id: \.self) { _ in
                    HStack(spacing: MonacoTheme.Space.sm) {
                        SkeletonBlock(width: 40, height: 40, radius: 20)
                        SkeletonBlock(width: 120, height: 14)
                        Spacer(minLength: MonacoTheme.Space.s)
                        SkeletonBlock(width: 32, height: 14)
                    }
                    .padding(.horizontal, MonacoTheme.Space.m)
                    .frame(minHeight: 60)
                }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }
}

/// Words the proposal screen needs that depend on what kind of proposal it is. The shared
/// `ProposalFeedCopy` says "buy" for every kind, which reads wrong over a sell or a bot.
enum ProposalDiscussionCopy {
    /// The viewer's own line in the Votes list.
    static let you = "You"

    static func moreMembers(_ count: Int) -> String {
        count == 1 ? "1 more member" : "\(count) more members"
    }

    /// The reason's heading: "Why buy", "Why sell", "Why add this bot".
    static func reasonTitle(for proposal: ProposalDTO) -> String {
        switch proposal.resolvedKind {
        case "add_agent": return "Why add this bot"
        case "pause_agent": return "Why pause the bot"
        case "resume_agent": return "Why turn the bot back on"
        case "revoke_agent": return "Why remove the bot"
        default: return ProposalFeedCopy.reasonTitle(for: proposal)
        }
    }

    /// Once the vote is over there is no case left to make.
    static let emptyClosedThread = "No comments yet."

    /// What an empty discussion says, in the words of the thing being decided.
    static func emptyThread(for proposal: ProposalDTO) -> String {
        guard proposal.isOpen else { return emptyClosedThread }
        switch proposal.resolvedKind {
        case "buy": return ProposalFeedCopy.emptyThread
        case "sell": return "No comments yet. Make the case for or against this sell."
        case "add_agent": return "No comments yet. Make the case for or against this bot."
        default: return "No comments yet. Make the case for or against this change."
        }
    }

    /// Every string above for every kind, for the copy audit. Built from the functions so the
    /// audit reads what the screen shows.
    static var auditedStrings: [String] {
        let kinds = ["buy", "sell", "add_agent", "pause_agent", "resume_agent", "revoke_agent"]
        let proposals = kinds.map { ProposalDTO(id: "audit", symbol: "AAPLx", status: "open", kind: $0) }
        return [you, moreMembers(1), moreMembers(3), emptyClosedThread]
            + proposals.map { reasonTitle(for: $0) }
            + proposals.map { emptyThread(for: $0) }
    }
}
