import SwiftUI

/// Join an investing club by group ID.
struct JoinGroupView: View {
    @ObservedObject var auth: PrivyAuthService

    private let apiClient = MonacoAPIClient()

    @State private var groupId = ""
    @State private var didJoin = false
    @State private var errorMessage: String?
    @State private var isJoining = false

    var body: some View {
        Form {
            Section {
                TextField("Group ID", text: $groupId)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .font(.body.monospaced())
                    .disabled(isJoining || didJoin)
                    .accessibilityIdentifier("join-group-id")
            } header: {
                Text("Club invite")
            } footer: {
                Text("Paste the group ID your friend shared.")
            }

            Section {
                Button(isJoining ? "Joining…" : "Join group") {
                    Task { await joinGroup() }
                }
                .disabled(isJoining || didJoin || !canSubmit)
                .accessibilityIdentifier("join-group-submit")
            }

            if didJoin {
                Section {
                    Label("You're in! Head home to see your club on the board.", systemImage: "checkmark.circle.fill")
                        .font(.footnote)
                        .foregroundStyle(.green)
                }
                .accessibilityIdentifier("join-group-success")
            } else if let errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                }
            }
        }
        .navigationTitle("Join group")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var canSubmit: Bool {
        !groupId.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func joinGroup() async {
        guard let accessToken = auth.accessToken else {
            errorMessage = "Sign in to join a group."
            return
        }

        let trimmedId = groupId.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedId.isEmpty else {
            errorMessage = "Enter a group ID."
            return
        }

        isJoining = true
        errorMessage = nil

        do {
            try await apiClient.joinGroup(
                accessToken: accessToken,
                groupId: trimmedId,
                password: nil
            )
            didJoin = true
        } catch MonacoAPIError.httpStatus(403) {
            errorMessage = "You are not allowed to join this club."
        } catch MonacoAPIError.httpStatus(404) {
            errorMessage = "Group not found. Check the ID and try again."
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "Could not join group (HTTP \(status))."
        } catch {
            errorMessage = "Could not join group. Try again."
        }

        isJoining = false
    }
}

#Preview {
    NavigationStack {
        JoinGroupView(auth: PrivyAuthService())
    }
}
