import MonacoCore
import SwiftUI

/// Proposal detail: card header with inline voting, ballots, buy status, and the comment thread
/// with a composer pinned to the bottom.
struct ProposalDetailView: View {
    let service: ProposalFeedService
    let proposalId: String

    @State private var proposal: ProposalDTO?
    @State private var errorMessage: String?
    @State private var isLoading: Bool
    @State private var isVoting = false
    @State private var comments: [ProposalCommentDTO] = []
    @State private var commentsLoading = true
    @State private var commentsError: String?
    @State private var draft = ""
    @State private var replyTarget: ProposalCommentDTO?
    @State private var isPosting = false
    @State private var toast: MonacoToast?

    init(service: ProposalFeedService, proposalId: String, initialProposal: ProposalDTO? = nil) {
        self.service = service
        self.proposalId = proposalId
        _proposal = State(initialValue: initialProposal)
        _isLoading = State(initialValue: initialProposal == nil)
    }

    /// Entry point for screens outside the feed (home missed votes, activity rows).
    init(auth: PrivyAuthService, proposalId: String) {
        self.init(service: LiveProposalFeedService(auth: auth), proposalId: proposalId)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if let proposal {
                    ProposalCardView(
                        proposal: proposal,
                        isVoting: isVoting,
                        onVote: { choice in Task { await vote(choice) } }
                    )
                    thesis(proposal)
                    ballots(proposal)
                    agentDetails(proposal)
                    buyStatus(proposal)
                    CommentThreadView(
                        comments: comments,
                        isLoading: commentsLoading,
                        errorMessage: commentsError,
                        onRetry: { Task { await loadComments() } },
                        onReply: { replyTarget = $0 }
                    )
                } else if let errorMessage {
                    VStack(alignment: .leading, spacing: 12) {
                        Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                            .font(.footnote)
                            .foregroundStyle(MonacoTheme.warning)
                        Button("Try again") { Task { await loadProposal() } }
                            .buttonStyle(.monacoSecondary)
                    }
                    .monacoSurfaceCard()
                } else if isLoading {
                    ProgressView()
                        .tint(MonacoTheme.accent)
                        .frame(maxWidth: .infinity)
                        .padding(.top, 32)
                }
            }
            .padding(16)
        }
        .scrollDismissesKeyboard(.interactively)
        .background(MonacoTheme.background)
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
        .monacoToast($toast)
        .navigationTitle(ProposalFeedCopy.feedTitle)
        .navigationBarTitleDisplayMode(.inline)
        .task(id: proposalId) {
            // Feed items carry no ballots; always fetch detail so votes and canVote are current.
            await loadProposal()
            await loadComments()
        }
        .refreshable {
            await loadProposal()
            await loadComments()
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("proposal-detail")
    }

    @ViewBuilder
    private func thesis(_ proposal: ProposalDTO) -> some View {
        if let thesis = proposal.thesis, !thesis.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Thesis")
                    .font(.headline)
                Text(thesis)
                    .font(.body)
                    .foregroundStyle(MonacoTheme.primaryText)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("proposal-detail-thesis")
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .monacoSurfaceCard()
        }
    }

    @ViewBuilder
    private func ballots(_ proposal: ProposalDTO) -> some View {
        if let votes = proposal.votes, !votes.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Votes")
                    .font(.headline)
                ForEach(votes) { vote in
                    HStack {
                        Text(vote.displayName)
                        Spacer()
                        Text(vote.choice.capitalized)
                            .font(.body.weight(.semibold))
                            .foregroundStyle(vote.choice.lowercased() == "yes" ? MonacoTheme.success : MonacoTheme.destructive)
                    }
                    .font(.subheadline)
                }
            }
            .monacoSurfaceCard()
            .accessibilityIdentifier("proposal-detail-votes")
        }
    }

    @ViewBuilder
    private func agentDetails(_ proposal: ProposalDTO) -> some View {
        if proposal.resolvedKind == "add_agent" {
            VStack(alignment: .leading, spacing: 8) {
                Text("Agent")
                    .font(.headline)
                if let name = proposal.agentDisplayName {
                    LabeledContent("Agent name", value: name)
                }
                if let allocation = proposal.allocationUsdcMicros {
                    LabeledContent("Budget", value: ProposalAmountFormatter.dollars(fromMicros: allocation))
                }
                if let key = proposal.mintedAgentKey, !key.isEmpty {
                    AgentKeyRevealView(apiKey: key) {
                        toast = MonacoToast(message: "API key copied", isSuccess: true)
                    }
                }
            }
            .font(.subheadline)
            .monacoSurfaceCard()
            .accessibilityIdentifier("proposal-detail-agent")
        }
    }

    @ViewBuilder
    private func buyStatus(_ proposal: ProposalDTO) -> some View {
        if proposal.isTrade, let execution = proposal.execution, execution.state.lowercased() != "not_applicable" {
            VStack(alignment: .leading, spacing: 6) {
                Text(proposal.isSell ? "Sell status" : "Buy status")
                    .font(.headline)
                Text(executionLabel(execution.state))
                    .font(.subheadline)
                    .foregroundStyle(MonacoTheme.secondaryText)
                if let executedAt = execution.executedAt, let date = ProposalTimeFormatter.parse(executedAt) {
                    Text(date.formatted(date: .abbreviated, time: .shortened))
                        .font(.caption)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
            }
            .monacoSurfaceCard()
            .accessibilityIdentifier("proposal-detail-buy-status")
        }
    }

    private func executionLabel(_ state: String) -> String {
        switch state.lowercased() {
        case "confirmed": "Done"
        case "pending": "In progress"
        case "failed": "Failed. Retry it from the cabal's activity."
        default: state.capitalized
        }
    }

    private func loadProposal() async {
        isLoading = proposal == nil
        errorMessage = nil
        defer { isLoading = false }
        do {
            proposal = try await service.proposal(id: proposalId)
        } catch is CancellationError {
            return
        } catch {
            if proposal == nil {
                errorMessage = "Could not load this proposal."
            }
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
            commentsError = ProposalFeedCopy.commentsLoadFailed
        }
    }

    private func vote(_ choice: ProposalVoteChoice) async {
        guard !isVoting else { return }
        isVoting = true
        defer { isVoting = false }
        let result = await ProposalVoting.cast(choice, proposalId: proposalId, service: service)
        toast = result.toast
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
