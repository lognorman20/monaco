import MonacoCore
import SwiftUI

/// What the member is told when a join does not go through. Pure so the copy
/// can be tested without a network.
enum JoinCabalCopy {
    /// One name for the value a friend shares. The New cabal sheet, the cabal's
    /// details sheet and this form all use it, so the handoff reads the same
    /// everywhere.
    static let codeLabel = "Invite code"
    static let codeFooter = "Paste the invite code your friend shared."
    static let malformedCode = "That doesn't look like an invite code. Ask your friend to send it again."

    static func failureMessage(for error: Error, enteredCode: Bool) -> String {
        if case MonacoAPIError.missingAccessToken = error {
            return "Sign in again to join a cabal."
        }
        switch status(of: error) {
        case 403:
            // Demo cabals on the board are read-only; retrying never works.
            return "This cabal is a demo. You can look, but not join."
        case 404:
            return enteredCode
                ? "No cabal with that invite code. Check it and try again."
                : "This cabal isn't around any more."
        case 401:
            return "Sign in again to join a cabal."
        default:
            return "Couldn't join this cabal. Try again."
        }
    }

    private static func status(of error: Error) -> Int? {
        switch error as? MonacoAPIError {
        case .httpStatus(let status): return status
        case .apiError(let status, _): return status
        default: return nil
        }
    }
}

/// Join a cabal by pasted invite code, or from a search/board row that already
/// knows the cabal's name and join policy.
struct JoinGroupView: View {
    @ObservedObject var auth: DynamicAuthService
    /// Present inside the signed-in shell; refreshed after a join so every tab updates.
    @Environment(AppSessionStore.self) private var session: AppSessionStore?
    private let actions: CabalsActionSource
    private let groupName: String?
    private let joinMode: GroupJoinMode?
    /// The viewer is a member now. The owner of the stack takes it from here —
    /// this screen never pushes the cabal itself, so Back cannot land back on a
    /// join form for a cabal the member is already in.
    private let onJoined: (_ groupId: String, _ groupName: String?) -> Void
    @State private var groupId: String
    @State private var requestPending = false
    @State private var isJoining = false
    @State private var toast: MonacoToast?

    init(
        auth: DynamicAuthService,
        groupId: String = "",
        groupName: String? = nil,
        joinMode: GroupJoinMode? = nil,
        actions: CabalsActionSource? = nil,
        onJoined: @escaping (_ groupId: String, _ groupName: String?) -> Void = { _, _ in }
    ) {
        self.auth = auth
        self.actions = actions ?? LiveCabalsActionSource(auth: auth)
        self.groupName = groupName
        self.joinMode = joinMode
        self.onJoined = onJoined
        _groupId = State(initialValue: groupId)
    }

    /// True on the paste-a-code route, where the member types the cabal id.
    private var entersCode: Bool { groupName == nil }

    private var trimmedId: String {
        groupId.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Invite codes are cabal ids. Checking the shape here keeps a truncated
    /// paste from becoming a server error the member is told to retry.
    private var isCodeWellFormed: Bool {
        UUID(uuidString: trimmedId) != nil
    }

    /// The shape check guards what the member typed. An id that came from a row
    /// is the server's own, so it is taken as given.
    private var canSubmit: Bool {
        guard !trimmedId.isEmpty else { return false }
        return entersCode ? isCodeWellFormed : true
    }

    private var actionTitle: String {
        if isJoining { return joinMode == .request ? "Sending…" : "Joining…" }
        if requestPending { return "Request sent" }
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
                    HStack {
                        TextField(JoinCabalCopy.codeLabel, text: $groupId)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .font(.body.monospaced())
                            .disabled(isJoining || requestPending)
                            .accessibilityIdentifier("join-group-id")
                        PasteButton(payloadType: String.self) { strings in
                            guard let pasted = strings.first else { return }
                            Task { @MainActor in
                                groupId = pasted.trimmingCharacters(in: .whitespacesAndNewlines)
                            }
                        }
                        .labelStyle(.iconOnly)
                        .buttonBorderShape(.capsule)
                        .accessibilityIdentifier("join-group-paste")
                    }
                } footer: {
                    Text(trimmedId.isEmpty || isCodeWellFormed
                        ? JoinCabalCopy.codeFooter
                        : JoinCabalCopy.malformedCode)
                }
            }
            Section {
                Button(actionTitle) {
                    Task { await joinGroup() }
                }
                .disabled(isJoining || requestPending || !canSubmit)
                .accessibilityIdentifier("join-group-submit")
            }
        }
        .monacoFormScreen()
        .monacoToast($toast)
        .navigationTitle(joinMode == .request ? "Ask to join" : "Join cabal")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func joinGroup() async {
        guard !isJoining, canSubmit else { return }
        let id = trimmedId
        isJoining = true
        defer { isJoining = false }
        do {
            let outcome = try await actions.joinGroup(groupId: id)
            switch outcome {
            case .joined, .alreadyMember:
                // Hand over before refreshing: the member lands in the cabal
                // straight away, and the refresh catches up behind it.
                onJoined(id, groupName)
                await session?.refresh(auth: auth)
            case .pending:
                requestPending = true
                toast = MonacoToast(
                    message: "Request sent. You'll be in once an admin approves.",
                    isSuccess: true
                )
            }
        } catch {
            toast = MonacoToast(message: JoinCabalCopy.failureMessage(for: error, enteredCode: entersCode))
        }
    }
}

#Preview {
    NavigationStack {
        JoinGroupView(
            auth: DynamicAuthService(),
            groupId: "5b1f0c9e-0005-4c55-9a51-000000000005",
            groupName: "Weekend investors",
            joinMode: .request
        )
    }
}
