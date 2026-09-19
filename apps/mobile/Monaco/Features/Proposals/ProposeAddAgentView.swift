import SwiftUI

struct ProposeAddAgentView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let treasuryTotalMicros: Int64?

    private let apiClient = MonacoAPIClient()

    @State private var agentName = ""
    @State private var allocationText = ""
    @State private var isSubmitting = false
    @State private var errorMessage: String?
    @State private var toast: MonacoToast?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        Form {
            Section {
                Text("Members vote once to add an AI agent with a treasury budget. After the vote passes, you copy the API key into your bot.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }

            Section("Agent") {
                TextField("Agent name", text: $agentName)
                    .accessibilityIdentifier("add-agent-name-field")
                TextField("Allocation (USDC)", text: $allocationText)
                    .keyboardType(.decimalPad)
                    .accessibilityIdentifier("add-agent-allocation-field")
                if let treasuryTotalMicros {
                    Text("Treasury total: \(formatUsd(micros: treasuryTotalMicros))")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }

            if let errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(MonacoTheme.warning)
                }
            }

            Section {
                Button(isSubmitting ? "Submitting…" : "Propose add agent") {
                    Task { await submit() }
                }
                .disabled(isSubmitting || !canSubmit)
                .accessibilityIdentifier("add-agent-submit")
            }
        }
        .monacoFormScreen()
        .monacoToast($toast)
        .navigationTitle("Add agent")
    }

    private var canSubmit: Bool {
        !agentName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && allocationMicros > 0
    }

    private var allocationMicros: Int64 {
        guard let decimal = Decimal(string: allocationText.trimmingCharacters(in: .whitespaces)), decimal > 0 else {
            return 0
        }
        return (decimal as NSDecimalNumber).multiplying(by: 1_000_000).int64Value
    }

    private func submit() async {
        guard let token = auth.accessToken else { return }
        isSubmitting = true
        errorMessage = nil
        defer { isSubmitting = false }

        do {
            _ = try await apiClient.createProposal(
                accessToken: token,
                groupId: groupId,
                kind: "add_agent",
                symbol: nil,
                usdcMicros: nil,
                tokenAmount: nil,
                agentDisplayName: agentName.trimmingCharacters(in: .whitespacesAndNewlines),
                allocationUsdcMicros: allocationMicros
            )
            toast = MonacoToast(message: "Add agent proposal created", isSuccess: true)
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func formatUsd(micros: Int64) -> String {
        String(format: "$%.2f", Double(micros) / 1_000_000.0)
    }
}
