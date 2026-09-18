import SwiftUI

struct JoinGroupView: View {
    @ObservedObject var auth: PrivyAuthService
    private let apiClient = MonacoAPIClient()
    @State private var groupId: String
    @State private var didJoin = false
    @State private var requestPending = false
    @State private var isJoining = false
    @State private var toast: MonacoToast?

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
            } footer: {
                Text("Paste the group ID your friend shared.")
            }
            Section {
                Button(isJoining ? "Joining…" : "Join group") {
                    Task { await joinGroup() }
                }
                .disabled(isJoining || didJoin || requestPending || groupId.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
        .monacoToast($toast)
        .navigationTitle("Join group")
    }

    private func joinGroup() async {
        guard let accessToken = auth.accessToken else {
            toast = MonacoToast(message: "Sign in to join a group.")
            return
        }
        let trimmedId = groupId.trimmingCharacters(in: .whitespacesAndNewlines)
        isJoining = true
        defer { isJoining = false }
        do {
            let outcome = try await apiClient.joinGroup(accessToken: accessToken, groupId: trimmedId)
            switch outcome {
            case .joined, .alreadyMember:
                didJoin = true
                toast = MonacoToast(
                    message: "You're in! Head home to see your club on the board.",
                    isSuccess: true
                )
            case .pending:
                requestPending = true
                toast = MonacoToast(
                    message: "Request sent. The club admin will approve your join.",
                    isSuccess: true
                )
            }
        } catch MonacoAPIError.httpStatus(404) {
            toast = MonacoToast(message: "Group not found. Check the ID and try again.")
        } catch {
            toast = MonacoToast(message: "Could not join group. Try again.")
        }
    }
}

#Preview {
    NavigationStack {
        JoinGroupView(auth: PrivyAuthService())
    }
}
