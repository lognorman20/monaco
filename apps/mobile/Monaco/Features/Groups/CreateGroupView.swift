import MonacoCore
import SwiftUI

enum JoinPolicyMode: String, CaseIterable, Identifiable {
    case open
    case request

    var id: String { rawValue }

    var label: String {
        switch self {
        case .open: "Anyone with the link"
        case .request: "I approve"
        }
    }
}

enum VoterSetMode: String, CaseIterable, Identifiable {
    case allMembers = "all_members"
    case namedSubset = "named_subset"

    var id: String { rawValue }

    var label: String {
        switch self {
        case .allMembers: "Everyone"
        case .namedSubset: "Just me"
        }
    }
}

enum VoteThresholdMode: String, CaseIterable, Identifiable {
    case majority
    case unanimous

    var id: String { rawValue }

    var label: String {
        switch self {
        case .majority: "Majority"
        case .unanimous: "Everyone agrees"
        }
    }
}

enum VoteExpiryOption: Int64, CaseIterable, Identifiable {
    case oneHour = 3600
    case oneDay = 86_400
    case sevenDays = 604_800

    var id: Int64 { rawValue }

    var label: String {
        switch self {
        case .oneHour: "1 hour"
        case .oneDay: "24 hours"
        case .sevenDays: "7 days"
        }
    }
}

/// Product create-group flow: join policy, voter set, threshold, and vote expiry.
struct CreateGroupView: View {
    @ObservedObject var auth: DynamicAuthService
    /// Present inside the signed-in shell; lightweight session patch after create.
    @Environment(AppSessionStore.self) private var session: AppSessionStore?
    /// The cabal exists. The owner of the stack replaces this form with it, so
    /// Back lands on the Cabals tab instead of on a form that is still armed.
    let onCreated: (CreateGroupResponse) -> Void

    private let actions: CabalsActionSource

    init(
        auth: DynamicAuthService,
        actions: CabalsActionSource? = nil,
        onCreated: @escaping (CreateGroupResponse) -> Void = { _ in }
    ) {
        self.auth = auth
        self.actions = actions ?? LiveCabalsActionSource(auth: auth)
        self.onCreated = onCreated
    }

    @State private var groupName = ""
    @State private var joinPolicy: JoinPolicyMode = .open
    @State private var voterSet: VoterSetMode = .allMembers
    @State private var threshold: VoteThresholdMode = .majority
    @State private var voteExpiry: VoteExpiryOption = .oneDay

    @State private var errorMessage: String?
    @State private var isCreating = false

    var body: some View {
        Form {
            Section {
                TextField("Cabal name", text: $groupName)
                    .textInputAutocapitalization(.words)
                    .disabled(isCreating)
                    .accessibilityIdentifier("create-group-name")
            } header: {
                Text("Name your cabal")
            } footer: {
                Text("Pick a name your friends will recognize.")
            }

            Section("Who can join?") {
                Picker("Join policy", selection: $joinPolicy) {
                    ForEach(JoinPolicyMode.allCases) { mode in
                        Text(mode.label).tag(mode)
                    }
                }
                .pickerStyle(.inline)
                .disabled(isCreating)

            }

            Section("Who votes on buys?") {
                Picker("Voter set", selection: $voterSet) {
                    ForEach(VoterSetMode.allCases) { mode in
                        Text(mode.label).tag(mode)
                    }
                }
                .pickerStyle(.inline)
                .disabled(isCreating)
            }

            Section("Passing a buy proposal") {
                Picker("Threshold", selection: $threshold) {
                    ForEach(VoteThresholdMode.allCases) { mode in
                        Text(mode.label).tag(mode)
                    }
                }
                .pickerStyle(.inline)
                .disabled(isCreating)

                Picker("Vote window", selection: $voteExpiry) {
                    ForEach(VoteExpiryOption.allCases) { option in
                        Text(option.label).tag(option)
                    }
                }
                .disabled(isCreating)
            }

            Section {
                Button(isCreating ? "Creating…" : "Create cabal") {
                    Task { await createGroup() }
                }
                .disabled(isCreating || !canSubmit)
                .accessibilityIdentifier("create-group-submit")
            }

            if let errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                }
            }
        }
        .monacoFormScreen()
        .navigationTitle("New cabal")
        .navigationBarTitleDisplayMode(.inline)
    }

    /// Only the name gates the button. "Just me" needs the creator's id, but
    /// that is a reason to say so when the member taps — not to hand them a
    /// dead button with no explanation.
    private var canSubmit: Bool {
        !groupName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func createGroup() async {
        // The disabled state only lands on the next render, so a fast double tap
        // gets through it. Same guard the money screens use.
        guard !isCreating else { return }

        let trimmedName = groupName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedName.isEmpty else {
            errorMessage = "Cabal name is required."
            return
        }

        // Held for the whole run, including the profile read below, so there is
        // no window where a second tap can start a second cabal.
        isCreating = true
        errorMessage = nil
        defer { isCreating = false }

        var memberIds: [String] = []
        if voterSet == .namedSubset {
            // "Just me" needs the creator's id. The signed-in profile is already
            // in the shell; the form used to re-open a backend session and
            // re-read /v1/me on every appearance.
            if session?.me?.userId == nil {
                // Recover here rather than sending them away: this form is
                // pushed, so "pull down on Cabals" costs them what they typed.
                await session?.refresh(auth: auth)
            }
            guard let creatorUserId = session?.me?.userId else {
                errorMessage = "We couldn't confirm your profile. Check your connection, then tap Create cabal again."
                return
            }
            memberIds = [creatorUserId]
        }

        do {
            let created = try await actions.createGroup(
                name: trimmedName,
                joinPolicyMode: joinPolicy.rawValue,
                voterSetMode: voterSet.rawValue,
                voterMemberIds: memberIds,
                threshold: threshold.rawValue,
                voteExpirySeconds: voteExpiry.rawValue
            )
            // #215: patch the session locally and refresh in the background; no full reload.
            session?.refreshAfterCreate(auth: auth, created: created)
            onCreated(created)
        } catch MonacoAPIError.missingAccessToken {
            errorMessage = "Sign in to create a cabal."
        } catch {
            errorMessage = "Couldn't create this cabal. Try again."
        }
    }
}

#Preview {
    NavigationStack {
        CreateGroupView(auth: DynamicAuthService())
    }
}
