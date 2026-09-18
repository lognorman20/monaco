import SwiftUI

/// Proposal detail with proposer, votes, expiry, and yes/no actions.
struct ProposalDetailView: View {
    @ObservedObject var auth: PrivyAuthService
    let proposalId: String
    let initialProposal: ProposalDTO?

    private let apiClient = MonacoAPIClient()

    @State private var proposal: ProposalDTO?
    @State private var errorMessage: String?
    @State private var isLoading: Bool
    @State private var isVoting = false
    @State private var didVote = false
    @State private var toast: MonacoToast?

    init(auth: PrivyAuthService, proposal: ProposalDTO) {
        self.auth = auth
        self.proposalId = proposal.id
        self.initialProposal = proposal
        _proposal = State(initialValue: proposal)
        _isLoading = State(initialValue: false)
    }

    init(auth: PrivyAuthService, proposalId: String, initialProposal: ProposalDTO? = nil) {
        self.auth = auth
        self.proposalId = proposalId
        self.initialProposal = initialProposal
        _proposal = State(initialValue: initialProposal)
        _isLoading = State(initialValue: initialProposal == nil)
    }

    var body: some View {
        Form {
            if let proposal {
                detailContent(proposal)
            } else if let errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.warning)
                    Button("Try again") {
                        Task { await loadProposal() }
                    }
                    .monacoFormSecondaryAction()
                }
            } else if isLoading {
                Section {
                    ProgressView("Loading proposal…")
                        .tint(MonacoTheme.accent)
                }
            }
        }
        .monacoFormScreen()
        .monacoToast($toast)
        .navigationTitle("Proposal")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: loadTaskID) {
            if initialProposal == nil || proposal?.votes == nil {
                await loadProposal()
            }
        }
    }

    private var loadTaskID: String {
        "\(proposalId)-\(auth.accessToken ?? "")"
    }

    @ViewBuilder
    private func detailContent(_ proposal: ProposalDTO) -> some View {
        Section {
            HStack {
                Text(proposal.symbol)
                    .font(.title2.bold())
                Spacer()
                ProposalStatusChip(status: proposal.status)
            }
            Text("Cabal buy proposal for \(formattedUsdc(proposal)) USDC")
                .font(.subheadline)
                .foregroundStyle(MonacoTheme.secondaryText)

            if let proposerName = proposal.proposerName {
                LabeledContent("Proposed by", value: proposerName)
            }
            if let createdAt = proposal.createdAt {
                LabeledContent("Created", value: formatTimestamp(createdAt))
            }
            if proposal.status.lowercased() == "open", let expiresAt = proposal.expiresAt {
                LabeledContent("Expires", value: formatTimestamp(expiresAt))
                Text(timeRemaining(until: expiresAt))
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(MonacoTheme.accent)
            }
        }

        if let summary = proposal.voteSummary {
            Section("Vote outcome") {
                LabeledContent("Threshold", value: summary.threshold.capitalized)
                LabeledContent("Eligible voters", value: "\(summary.eligibleCount)")
                LabeledContent("Yes / No", value: "\(summary.yesCount) / \(summary.noCount)")
            }
        }

        if let execution = proposal.execution {
            Section("Execution") {
                LabeledContent("Status", value: executionStatusLabel(execution.state))
                LabeledContent("Tx signature", value: displayOrNA(execution.txSignature))
                LabeledContent("Transaction ID", value: displayOrNA(execution.transactionId))
                LabeledContent("Execute request", value: displayOrNA(execution.executeRequestId))
                if let executedAt = execution.executedAt {
                    LabeledContent("Executed", value: formatTimestamp(executedAt))
                } else {
                    LabeledContent("Executed", value: "N/A")
                }
                if let reason = execution.failureReason, !reason.isEmpty {
                    LabeledContent("Failure reason", value: reason)
                }
            }
        }

        if let votes = proposal.votes {
            Section("Votes") {
                if votes.isEmpty {
                    Text("No votes yet.")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.secondaryText)
                } else {
                    ForEach(votes) { vote in
                        HStack {
                            Text(vote.displayName)
                            Spacer()
                            Text(vote.choice.capitalized)
                                .font(.body.weight(.semibold))
                                .foregroundStyle(vote.choice.lowercased() == "yes" ? MonacoTheme.success : MonacoTheme.warning)
                        }
                    }
                }
            }
        }

        if proposal.status.lowercased() == "open", proposal.canVote == true, !didVote {
            Section("Your vote") {
                Button(isVoting ? "Submitting…" : "Vote yes") {
                    Task { await castVote(choice: "yes", proposal: proposal) }
                }
                .disabled(isVoting)
                .accessibilityIdentifier("proposal-vote-yes")

                Button(isVoting ? "Submitting…" : "Vote no") {
                    Task { await castVote(choice: "no", proposal: proposal) }
                }
                .disabled(isVoting)
                .accessibilityIdentifier("proposal-vote-no")
            }
        }

    }

    private func formattedUsdc(_ proposal: ProposalDTO) -> String {
        guard let micro = Int64(proposal.usdcMicros) else { return proposal.usdcMicros }
        let dollars = Double(micro) / 1_000_000.0
        return String(format: "%.2f", dollars)
    }

    private func displayOrNA(_ value: String?) -> String {
        guard let value, !value.isEmpty else { return "N/A" }
        return value
    }

    private func executionStatusLabel(_ state: String) -> String {
        switch state.lowercased() {
        case "confirmed": "Confirmed on chain"
        case "pending": "Pending swap"
        case "failed": "Swap failed"
        case "not_applicable": "N/A"
        default: state.capitalized
        }
    }

    private func formatTimestamp(_ raw: String) -> String {
        guard let date = ISO8601DateFormatter().date(from: raw) else { return raw }
        return date.formatted(date: .abbreviated, time: .shortened)
    }

    private func timeRemaining(until raw: String) -> String {
        guard let expiry = ISO8601DateFormatter().date(from: raw) else {
            return "Time left unknown"
        }
        let remaining = expiry.timeIntervalSinceNow
        if remaining <= 0 {
            return "Voting window closed"
        }
        let hours = Int(remaining) / 3600
        let minutes = (Int(remaining) % 3600) / 60
        if hours >= 24 {
            let days = hours / 24
            return "\(days) day\(days == 1 ? "" : "s") left to vote"
        }
        if hours > 0 {
            return "\(hours)h \(minutes)m left to vote"
        }
        return "\(minutes)m left to vote"
    }

    private func loadProposal() async {
        guard let token = auth.accessToken else {
            isLoading = false
            errorMessage = "Missing sign-in token."
            return
        }

        isLoading = true
        errorMessage = nil
        defer { isLoading = false }

        do {
            proposal = try await apiClient.getProposalDetail(accessToken: token, proposalId: proposalId)
        } catch is CancellationError {
            return
        } catch MonacoAPIError.httpStatus(let code) {
            errorMessage = "Could not load proposal (HTTP \(code))."
        } catch {
            errorMessage = "Could not load proposal."
        }
    }

    private func castVote(choice: String, proposal: ProposalDTO) async {
        guard let token = auth.accessToken else { return }
        isVoting = true
        defer { isVoting = false }

        do {
            try await apiClient.castVote(accessToken: token, proposalId: proposal.id, choice: choice)
            didVote = true
            toast = MonacoToast(message: "Vote recorded", isSuccess: true)
            await loadProposal()
        } catch MonacoAPIError.httpStatus(let code) {
            toast = MonacoToast(message: "Vote failed (HTTP \(code)).")
        } catch {
            toast = MonacoToast(message: "Could not submit vote.")
        }
    }
}
