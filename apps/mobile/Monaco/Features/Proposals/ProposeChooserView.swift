import SwiftUI

struct ProposeChooserView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let groupView: GroupViewDTO

    var body: some View {
        List {
            NavigationLink {
                ProposeBuyView(auth: auth, groupId: groupId)
            } label: {
                Label("Buy", systemImage: "chart.line.uptrend.xyaxis")
            }
            .accessibilityIdentifier("propose-kind-buy")

            NavigationLink {
                ProposeSellView(auth: auth, groupId: groupId, holdings: heldStocks)
            } label: {
                Label("Sell", systemImage: "chart.line.downtrend.xyaxis")
            }
            .disabled(heldStocks.isEmpty)
            .accessibilityIdentifier("propose-kind-sell")

            if groupView.agent == nil {
                NavigationLink {
                    ProposeAddAgentView(
                        auth: auth,
                        groupId: groupId,
                        treasuryTotalMicros: treasuryMicros(from: groupView)
                    )
                } label: {
                    Label("Add agent", systemImage: "cpu")
                }
                .accessibilityIdentifier("propose-kind-add-agent")
            } else if groupView.agent?.status.lowercased() == "active" {
                NavigationLink {
                    ProposeAgentLifecycleView(auth: auth, groupId: groupId, kind: "pause_agent")
                } label: {
                    Label("Pause agent", systemImage: "pause.circle")
                }
                .accessibilityIdentifier("propose-kind-pause-agent")
            } else if groupView.agent?.status.lowercased() == "paused" {
                NavigationLink {
                    ProposeAgentLifecycleView(auth: auth, groupId: groupId, kind: "resume_agent")
                } label: {
                    Label("Resume agent", systemImage: "play.circle")
                }
                .accessibilityIdentifier("propose-kind-resume-agent")
            }

            if groupView.agent != nil {
                NavigationLink {
                    ProposeAgentLifecycleView(auth: auth, groupId: groupId, kind: "revoke_agent")
                } label: {
                    Label("Revoke agent", systemImage: "xmark.circle")
                }
                .accessibilityIdentifier("propose-kind-revoke-agent")
            }

            if heldStocks.isEmpty {
                Text("No stocks to sell yet.")
                    .foregroundStyle(.secondary)
            }
        }
        .navigationTitle("Propose")
    }

    private var heldStocks: [PotRowDTO] {
        groupView.pot.filter { row in
            row.symbol.uppercased() != "USDC" && (Int64(row.tokenAmount ?? "0") ?? 0) > 0
        }
    }

    private func treasuryMicros(from view: GroupViewDTO) -> Int64? {
        let usdcRow = view.pot.first { $0.symbol.uppercased() == "USDC" }
        guard let valueUsd = usdcRow?.valueUsd, let decimal = Decimal(string: valueUsd) else { return nil }
        return (decimal as NSDecimalNumber).multiplying(by: 1_000_000).int64Value
    }
}

struct ProposeAgentLifecycleView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let kind: String

    private let apiClient = MonacoAPIClient()
    @State private var isSubmitting = false
    @State private var errorMessage: String?
    @State private var toast: MonacoToast?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        Form {
            Section {
                Text(description)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if let errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(MonacoTheme.warning)
                }
            }
            Section {
                Button(isSubmitting ? "Submitting…" : "Submit proposal") {
                    Task { await submit() }
                }
                .disabled(isSubmitting)
            }
        }
        .monacoFormScreen()
        .monacoToast($toast)
        .navigationTitle(title)
    }

    private var title: String {
        switch kind {
        case "pause_agent": "Pause agent"
        case "resume_agent": "Resume agent"
        case "revoke_agent": "Revoke agent"
        default: "Agent proposal"
        }
    }

    private var description: String {
        switch kind {
        case "pause_agent": "Members vote to pause agent trading. The API key stays valid but new trades are rejected until resume."
        case "resume_agent": "Members vote to resume agent trading with the same API key."
        case "revoke_agent": "Members vote to permanently revoke the agent and invalidate its API key."
        default: ""
        }
    }

    private func submit() async {
        guard let token = auth.accessToken else { return }
        isSubmitting = true
        errorMessage = nil
        defer { isSubmitting = false }
        do {
            _ = try await apiClient.createProposal(accessToken: token, groupId: groupId, kind: kind)
            toast = MonacoToast(message: "Proposal created", isSuccess: true)
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
