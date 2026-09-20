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

    private let apiClient = MonacoAPIClient()

    @State private var groupName = ""
    @State private var joinPolicy: JoinPolicyMode = .open
    @State private var voterSet: VoterSetMode = .allMembers
    @State private var threshold: VoteThresholdMode = .majority
    @State private var voteExpiry: VoteExpiryOption = .oneDay
    @State private var creatorUserId: String?

    @State private var navigateToCreated: CreateGroupResponse?
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
        .task(id: auth.accessToken) {
            await loadCreatorProfile()
        }
        .navigationDestination(item: $navigateToCreated) { created in
            GroupDetailView(
                auth: auth,
                groupId: created.groupId,
                groupName: created.name
            )
            .accessibilityIdentifier("create-group-success")
        }
    }

    private var canSubmit: Bool {
        let trimmedName = groupName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedName.isEmpty else { return false }
        if voterSet == .namedSubset {
            return creatorUserId != nil
        }
        return true
    }

    private func loadCreatorProfile() async {
        guard let accessToken = auth.accessToken else {
            creatorUserId = nil
            return
        }
        do {
            _ = try await apiClient.openSession(accessToken: accessToken)
            let profile = try await apiClient.me(accessToken: accessToken)
            creatorUserId = profile.userId
        } catch {
            creatorUserId = nil
        }
    }

    private func createGroup() async {
        guard let accessToken = auth.accessToken else {
            errorMessage = "Sign in to create a cabal."
            return
        }

        let trimmedName = groupName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedName.isEmpty else {
            errorMessage = "Cabal name is required."
            return
        }

        var memberIds: [String] = []
        if voterSet == .namedSubset {
            guard let creatorUserId else {
                errorMessage = "Could not load your profile. Try again."
                return
            }
            memberIds = [creatorUserId]
        }

        isCreating = true
        errorMessage = nil

        do {
            let created = try await apiClient.createGroup(
                accessToken: accessToken,
                name: trimmedName,
                joinPolicyMode: joinPolicy.rawValue,
                voterSetMode: voterSet.rawValue,
                voterMemberIds: memberIds,
                threshold: threshold.rawValue,
                voteExpirySeconds: voteExpiry.rawValue
            )
            // #215: patch the session locally and refresh in the background; no full reload.
            session?.refreshAfterCreate(auth: auth, created: created)
            navigateToCreated = created
        } catch {
            errorMessage = "Couldn't create this cabal. Try again."
        }

        isCreating = false
    }
}

#Preview {
    NavigationStack {
        CreateGroupView(auth: DynamicAuthService())
    }
}
