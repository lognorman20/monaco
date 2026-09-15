import SwiftUI

/// M1 create-group proof screen: POST /v1/groups then verify treasury via GET /v1/groups/{id}.
struct CreateGroupView: View {
    @ObservedObject var auth: PrivyAuthService

    private let apiClient = MonacoAPIClient()

    @State private var groupName = ""
    @State private var createdGroup: CreateGroupResponse?
    @State private var verifiedGroup: GetGroupResponse?
    @State private var errorMessage: String?
    @State private var isCreating = false

    var body: some View {
        Form {
            Section {
                TextField("Group name", text: $groupName)
                    .textInputAutocapitalization(.words)
                    .disabled(isCreating || createdGroup != nil)
            }

            Section {
                Button(isCreating ? "Creating…" : "Create group") {
                    Task { await createGroup() }
                }
                .disabled(isCreating || groupName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || createdGroup != nil)
            }

            if let createdGroup {
                Section("Created group") {
                    detailRow(title: "Group ID", value: createdGroup.groupId)
                    detailRow(title: "Name", value: createdGroup.name)
                    detailRow(title: "Treasury address", value: createdGroup.treasuryAddress, monospaced: true)
                }

                if let verifiedGroup {
                    Section("Verified via GET /v1/groups/{id}") {
                        detailRow(title: "Name", value: verifiedGroup.name)
                        detailRow(title: "Treasury address", value: verifiedGroup.treasuryAddress, monospaced: true)

                        Label("Backend-controlled treasury confirmed", systemImage: "checkmark.seal.fill")
                            .font(.footnote)
                            .foregroundStyle(.green)
                    }
                }
            } else if let errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                }
            }
        }
        .navigationTitle("Create group")
        .navigationBarTitleDisplayMode(.inline)
    }

    @ViewBuilder
    private func detailRow(title: String, value: String, monospaced: Bool = false) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(value)
                .font(monospaced ? .body.monospaced() : .body)
                .textSelection(.enabled)
        }
    }

    private func createGroup() async {
        guard let accessToken = auth.accessToken else {
            errorMessage = "Missing Privy access token."
            return
        }

        let trimmedName = groupName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedName.isEmpty else {
            errorMessage = "Group name is required."
            return
        }

        isCreating = true
        errorMessage = nil
        createdGroup = nil
        verifiedGroup = nil

        do {
            let created = try await apiClient.createGroup(accessToken: accessToken, name: trimmedName)
            createdGroup = created
            verifiedGroup = try await apiClient.getGroup(accessToken: accessToken, groupId: created.groupId)
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "API error (\(status)). Is the backend running?"
        } catch {
            errorMessage = "Could not create group via API."
        }

        isCreating = false
    }
}

#Preview {
    NavigationStack {
        CreateGroupView(auth: PrivyAuthService())
    }
}
