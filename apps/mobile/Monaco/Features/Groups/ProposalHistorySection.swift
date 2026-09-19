import SwiftUI

enum ProposalHistoryTab: String, CaseIterable, Identifiable {
    case open
    case closed

    var id: String { rawValue }

    var title: String {
        switch self {
        case .open: "Open"
        case .closed: "Closed"
        }
    }
}

struct ProposalHistorySection: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String

    private let apiClient = MonacoAPIClient()

    @State private var selectedTab: ProposalHistoryTab = .open
    @State private var openProposals: [ProposalDTO] = []
    @State private var closedProposals: [ProposalDTO] = []
    @State private var isLoading = true
    @State private var errorMessage: String?

    var body: some View {
        Section {
            Picker("Proposal tab", selection: $selectedTab) {
                ForEach(ProposalHistoryTab.allCases) { tab in
                    Text(tab.title).tag(tab)
                }
            }
            .pickerStyle(.segmented)
            .accessibilityIdentifier("group-proposals-tab-picker")

            if isLoading {
                HStack(spacing: 12) {
                    ProgressView()
                        .tint(MonacoTheme.accent)
                    Text("Loading proposals…")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
                .accessibilityIdentifier("group-proposals-loading")
            } else if let errorMessage {
                Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.warning)
                    .accessibilityIdentifier("group-proposals-error")
            } else if visibleProposals.isEmpty {
                Text(emptyMessage)
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .accessibilityIdentifier("group-proposals-empty")
            } else {
                ForEach(visibleProposals) { proposal in
                    NavigationLink {
                        ProposalDetailView(auth: auth, proposalId: proposal.id, initialProposal: proposal)
                    } label: {
                        proposalRow(proposal)
                    }
                    .accessibilityIdentifier("group-proposal-row-\(proposal.id)")
                }
            }
        } header: {
            Text("Proposals")
        }
        .task(id: loadTaskID) {
            await loadProposals()
        }
    }

    private var loadTaskID: String {
        "\(groupId)-\(selectedTab.rawValue)-\(auth.accessToken ?? "")"
    }

    private var visibleProposals: [ProposalDTO] {
        selectedTab == .open ? openProposals : closedProposals
    }

    private var emptyMessage: String {
        selectedTab == .open ? "No open proposals." : "No closed proposals yet."
    }

    @ViewBuilder
    private func proposalRow(_ proposal: ProposalDTO) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(proposal.resolvedKind == "sell" ? "Sell \(proposal.symbol)" : proposal.symbol)
                    .font(.body.weight(.semibold))
                Spacer()
                ProposalStatusChip(status: proposal.status, kind: proposal.resolvedKind)
            }
            if let proposerName = proposal.proposerName {
                Text("By \(proposerName)")
                    .font(.caption)
                    .foregroundStyle(MonacoTheme.secondaryText)
            }
            if let thesis = proposal.thesis, !thesis.isEmpty {
                Text(thesis)
                    .font(.caption)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .lineLimit(2)
                    .accessibilityIdentifier("group-proposal-thesis-\(proposal.id)")
            }
            if selectedTab == .open, let expiresAt = proposal.expiresAt {
                Text(timeRemaining(until: expiresAt))
                    .font(.caption.weight(.medium))
                    .foregroundStyle(MonacoTheme.accent)
            }
        }
    }

    private func timeRemaining(until raw: String) -> String {
        guard let expiry = ISO8601DateFormatter().date(from: raw) else {
            return "Expires \(raw)"
        }
        let remaining = expiry.timeIntervalSinceNow
        if remaining <= 0 {
            return "Expired"
        }
        let hours = Int(remaining) / 3600
        let minutes = (Int(remaining) % 3600) / 60
        if hours >= 24 {
            let days = hours / 24
            return "Expires in \(days)d"
        }
        if hours > 0 {
            return "Expires in \(hours)h \(minutes)m"
        }
        return "Expires in \(minutes)m"
    }

    private func loadProposals() async {
        guard let token = auth.accessToken else {
            isLoading = false
            errorMessage = "Missing sign-in token."
            return
        }

        isLoading = true
        errorMessage = nil
        defer { isLoading = false }

        do {
            let response = try await apiClient.listGroupProposals(
                accessToken: token,
                groupId: groupId,
                tab: selectedTab.rawValue
            )
            if selectedTab == .open {
                openProposals = response.proposals
            } else {
                closedProposals = response.proposals
            }
        } catch is CancellationError {
            return
        } catch MonacoAPIError.httpStatus(let code) {
            if Task.isCancelled { return }
            errorMessage = "Could not load proposals (HTTP \(code))."
        } catch {
            if error.isRequestCancellation { return }
            errorMessage = "Could not load proposals."
        }
    }
}
