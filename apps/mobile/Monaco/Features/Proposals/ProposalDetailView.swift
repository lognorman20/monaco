import SwiftUI

/// Proposal detail with yes/no votes for voter-set members.
struct ProposalDetailView: View {
    @ObservedObject var auth: PrivyAuthService
    let proposal: ProposalDTO

    private let apiClient = MonacoAPIClient()

    @State private var localStatus: String
    @State private var errorMessage: String?
    @State private var isVoting = false
    @State private var didVote = false

    init(auth: PrivyAuthService, proposal: ProposalDTO) {
        self.auth = auth
        self.proposal = proposal
        _localStatus = State(initialValue: proposal.status)
    }

    var body: some View {
        Form {
            Section {
                HStack {
                    Text(proposal.symbol)
                        .font(.title2.bold())
                    Spacer()
                    ProposalStatusChip(status: localStatus)
                }
                Text("Club buy proposal for \(formattedUsdc) USDC")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }

            if localStatus.lowercased() == "open", proposal.canVote != false {
                Section("Your vote") {
                    Button(isVoting ? "Submitting…" : "Vote yes") {
                        Task { await castVote(choice: "yes") }
                    }
                    .disabled(isVoting || didVote)
                    .accessibilityIdentifier("proposal-vote-yes")

                    Button(isVoting ? "Submitting…" : "Vote no") {
                        Task { await castVote(choice: "no") }
                    }
                    .disabled(isVoting || didVote)
                    .accessibilityIdentifier("proposal-vote-no")
                }
            }

            if didVote {
                Section {
                    Label("Vote recorded", systemImage: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                }
            }

            if let errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                }
            }
        }
        .navigationTitle("Proposal")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var formattedUsdc: String {
        guard let micro = Int64(proposal.usdcMicros) else { return proposal.usdcMicros }
        let dollars = Double(micro) / 1_000_000.0
        return String(format: "%.2f", dollars)
    }

    private func castVote(choice: String) async {
        guard let token = auth.accessToken else { return }
        isVoting = true
        errorMessage = nil
        defer { isVoting = false }

        do {
            try await apiClient.castVote(accessToken: token, proposalId: proposal.id, choice: choice)
            didVote = true
        } catch MonacoAPIError.httpStatus(let code) {
            errorMessage = "Vote failed (HTTP \(code))."
        } catch {
            errorMessage = "Could not submit vote."
        }
    }
}
