import SwiftUI

enum JoinPolicyMode: String, CaseIterable, Identifiable {
    case open
    case password

    var id: String { rawValue }

    var label: String {
        switch self {
        case .open: "Anyone can join"
        case .password: "Password required"
        }
    }
}

enum VoterSetMode: String, CaseIterable, Identifiable {
    case allMembers = "all_members"
    case namedSubset = "named_subset"

    var id: String { rawValue }

    var label: String {
        switch self {
        case .allMembers: "All members vote"
        case .namedSubset: "You decide (named voters)"
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
        case .unanimous: "Everyone must agree"
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
    @ObservedObject var auth: PrivyAuthService

    private let apiClient = MonacoAPIClient()

    @State private var groupName = ""
    @State private var joinPolicy: JoinPolicyMode = .open
    @State private var joinPassword = ""
    @State private var voterSet: VoterSetMode = .allMembers
    @State private var threshold: VoteThresholdMode = .majority
    @State private var voteExpiry: VoteExpiryOption = .oneDay
    @State private var creatorUserId: String?

    @State private var createdGroup: CreateGroupResponse?
    @State private var errorMessage: String?
    @State private var isCreating = false

    var body: some View {
        Form {
            Section {
                TextField("Group name", text: $groupName)
                    .textInputAutocapitalization(.words)
                    .disabled(isCreating || createdGroup != nil)
                    .accessibilityIdentifier("create-group-name")
            } header: {
                Text("Name your club")
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
                .disabled(isCreating || createdGroup != nil)

                if joinPolicy == .password {
                    SecureField("Join password", text: $joinPassword)
                        .disabled(isCreating || createdGroup != nil)
                        .accessibilityIdentifier("create-group-join-password")
                }
            }

            Section("Who votes on buys?") {
                Picker("Voter set", selection: $voterSet) {
                    ForEach(VoterSetMode.allCases) { mode in
                        Text(mode.label).tag(mode)
                    }
                }
                .pickerStyle(.inline)
                .disabled(isCreating || createdGroup != nil)
            }

            Section("Passing a buy proposal") {
                Picker("Threshold", selection: $threshold) {
                    ForEach(VoteThresholdMode.allCases) { mode in
                        Text(mode.label).tag(mode)
                    }
                }
                .pickerStyle(.inline)
                .disabled(isCreating || createdGroup != nil)

                Picker("Vote window", selection: $voteExpiry) {
                    ForEach(VoteExpiryOption.allCases) { option in
                        Text(option.label).tag(option)
                    }
                }
                .disabled(isCreating || createdGroup != nil)
            }

            Section {
                Button(isCreating ? "Creating…" : "Start investing together") {
                    Task { await createGroup() }
                }
                .disabled(isCreating || !canSubmit || createdGroup != nil)
                .accessibilityIdentifier("create-group-submit")
            }

            if let createdGroup {
                Section("You're in") {
                    Text(createdGroup.name)
                        .font(.headline)
                    Text("Invite friends to join and add money to the pot.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                .accessibilityIdentifier("create-group-success")
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
        .task(id: auth.accessToken) {
            await loadCreatorProfile()
        }
    }

    private var canSubmit: Bool {
        let trimmedName = groupName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedName.isEmpty else { return false }
        if joinPolicy == .password {
            return !joinPassword.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        }
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
            errorMessage = "Sign in to create a group."
            return
        }

        let trimmedName = groupName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedName.isEmpty else {
            errorMessage = "Group name is required."
            return
        }

        if joinPolicy == .password && joinPassword.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            errorMessage = "Enter a join password."
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
        createdGroup = nil

        do {
            let created = try await apiClient.createGroup(
                accessToken: accessToken,
                name: trimmedName,
                joinPolicyMode: joinPolicy.rawValue,
                joinPassword: joinPolicy == .password ? joinPassword : nil,
                voterSetMode: voterSet.rawValue,
                voterMemberIds: memberIds,
                threshold: threshold.rawValue,
                voteExpirySeconds: voteExpiry.rawValue
            )
            createdGroup = created
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "Could not create group (HTTP \(status))."
        } catch {
            errorMessage = "Could not create group. Try again."
        }

        isCreating = false
    }
}

#Preview {
    NavigationStack {
        CreateGroupView(auth: PrivyAuthService())
    }
}
