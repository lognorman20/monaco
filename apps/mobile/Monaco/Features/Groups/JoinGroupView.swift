import SwiftUI

struct JoinGroupView: View {
    @ObservedObject var auth: PrivyAuthService
    private let apiClient = MonacoAPIClient()
    @State private var groupId: String
    @State private var didJoin = false
    @State private var requestPending = false
    @State private var errorMessage: String?
    @State private var isJoining = false

    init(auth: PrivyAuthService, groupId: String = "") {
        self.auth = auth
        _groupId = State(initialValue: groupId)
    }

    var body: some View {
        Form {
            Section {
                TextField("Group ID", text: $groupId)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .font(.body.monospaced())
                    .disabled(isJoining || didJoin || requestPending)
            } footer: { Text("Paste the group ID your friend shared.") }
            Section {
                Button(isJoining ? "Joining…" : "Join group") { Task { await joinGroup() } }
                    .disabled(isJoining || didJoin || requestPending || groupId.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            if didJoin {
                Section { Label("You're in! Head home to see your club on the board.", systemImage: "checkmark.circle.fill").foregroundStyle(.green) }
            } else if requestPending {
                Section { Label("Request sent. The club admin will approve your join.", systemImage: "clock.fill").foregroundStyle(MonacoTheme.accent) }
            } else if let errorMessage {
                Section { Label(errorMessage, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.orange) }
            }
        }
        .navigationTitle("Join group")
    }

    private func joinGroup() async {
        guard let accessToken = auth.accessToken else { errorMessage = "Sign in to join a group."; return }
        let trimmedId = groupId.trimmingCharacters(in: .whitespacesAndNewlines)
        isJoining = true; errorMessage = nil
        defer { isJoining = false }
        do {
            let outcome = try await apiClient.joinGroup(accessToken: accessToken, groupId: trimmedId)
            switch outcome {
            case .joined, .alreadyMember: didJoin = true
            case .pending: requestPending = true
            }
        } catch MonacoAPIError.httpStatus(404) { errorMessage = "Group not found. Check the ID and try again." }
        catch { errorMessage = "Could not join group. Try again." }
    }
}
