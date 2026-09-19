import MonacoCore
import SwiftUI

/// Join a cabal by pasted ID, or from a search/board row that already knows
/// the cabal's name and join policy.
struct JoinGroupView: View {
    @ObservedObject var auth: PrivyAuthService
    /// Present inside the signed-in shell; refreshed after a join so every tab updates.
    @Environment(AppSessionStore.self) private var session: AppSessionStore?
    private let apiClient = MonacoAPIClient()
    private let groupName: String?
    private let joinMode: GroupJoinMode?
    @State private var groupId: String
    @State private var didJoin = false
    @State private var requestPending = false
    @State private var isJoining = false
    @State private var toast: MonacoToast?

    init(auth: PrivyAuthService, groupId: String = "", groupName: String? = nil, joinMode: GroupJoinMode? = nil) {
        self.auth = auth
        self.groupName = groupName
        self.joinMode = joinMode
        _groupId = State(initialValue: groupId)
    }

    private var actionTitle: String {
        if isJoining { return joinMode == .request ? "Sending…" : "Joining…" }
        if requestPending { return "Request sent" }
        if didJoin { return "You're in" }
        return joinMode == .request ? "Ask to join" : "Join cabal"
    }

    var body: some View {
        Form {
            if let groupName {
                Section {
                    Text(groupName)
                        .font(MonacoTheme.TypeRole.title)
                        .accessibilityIdentifier("join-group-name")
                } footer: {
                    Text(joinMode == .request
                        ? "The cabal admin approves new members. You'll show up once they say yes."
                        : "Anyone can join this cabal. You can add money after you're in.")
                }
            } else {
                Section {
                    TextField("Cabal ID", text: $groupId)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .font(.body.monospaced())
                        .disabled(isJoining || didJoin || requestPending)
                        .accessibilityIdentifier("join-group-id")
                } footer: {
                    Text("Paste the cabal ID your friend shared.")
                }
            }
            Section {
                Button(actionTitle) {
                    Task { await joinGroup() }
                }
                .disabled(isJoining || didJoin || requestPending || groupId.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                .accessibilityIdentifier("join-group-submit")
            }
        }
        .monacoFormScreen()
        .monacoToast($toast)
        .navigationTitle(joinMode == .request ? "Ask to join" : "Join cabal")
    }

    private func joinGroup() async {
        guard let accessToken = auth.accessToken else {
            toast = MonacoToast(message: "Sign in to join a cabal.")
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
                    message: "You're in! Your cabal is on the Cabals tab now.",
                    isSuccess: true
                )
                await session?.refresh(auth: auth)
            case .pending:
                requestPending = true
                toast = MonacoToast(
                    message: "Request sent. The cabal admin will approve your join.",
                    isSuccess: true
                )
            }
        } catch MonacoAPIError.httpStatus(404) {
            toast = MonacoToast(message: "Cabal not found. Check the ID and try again.")
        } catch {
            toast = MonacoToast(message: "Could not join cabal. Try again.")
        }
    }
}

#Preview {
    NavigationStack {
        JoinGroupView(auth: PrivyAuthService(), groupId: "g1", groupName: "Weekend investors", joinMode: .request)
    }
}
