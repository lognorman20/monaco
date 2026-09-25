import MonacoCore
import SwiftUI

enum JoinPolicyMode: String, CaseIterable, Identifiable {
    case open
    case request

    var id: String { rawValue }

    /// "Anyone", not "Anyone with the link": the app has no invite links. What a member shares
    /// is the invite code, and an open cabal also takes anyone who finds it in search.
    var label: String {
        switch self {
        case .open: "Anyone"
        case .request: "I approve"
        }
    }

    /// What the choice means, under the rule on the Start a cabal screen.
    var caption: String {
        switch self {
        case .open: "Anyone can join, from search or with the invite code."
        case .request: "People ask to join, and you say yes or no."
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

    var caption: String {
        switch self {
        case .allMembers: "Every member votes on each proposal."
        case .namedSubset: "Only you vote on proposals."
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

    /// The server's tally, in words: a majority passes once the yes votes outnumber every
    /// other voter, cast or not; unanimity fails on the first no.
    var caption: String {
        switch self {
        case .majority: "Passes once more than half the voters say yes."
        case .unanimous: "Passes only if every voter says yes."
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

    /// A proposal passes the moment it has the votes; the window only decides when one that
    /// does not closes.
    var caption: String {
        "A vote that hasn't passed closes after \(label)."
    }
}

/// The Start a cabal screen's own words. The rule labels and captions live on the enums above.
enum CabalRulesCopy {
    static let screenTitle = "Start a cabal"
    static let namePlaceholder = "Cabal name"
    static let nameHint = "Pick a name your friends will recognize."
    static let sectionTitle = "The rules"
    static let joinTitle = "Who can join"
    static let votersTitle = "Who votes"
    static let thresholdTitle = "To pass"
    static let expiryTitle = "Votes stay open"
    static let create = "Create cabal"
    static let creating = "Creating…"

    static var auditedStrings: [String] {
        [screenTitle, namePlaceholder, nameHint, sectionTitle, joinTitle, votersTitle, thresholdTitle, expiryTitle, create, creating]
            + JoinPolicyMode.allCases.flatMap { [$0.label, $0.caption] }
            + VoterSetMode.allCases.flatMap { [$0.label, $0.caption] }
            + VoteThresholdMode.allCases.flatMap { [$0.label, $0.caption] }
            + VoteExpiryOption.allCases.flatMap { [$0.label, $0.caption] }
    }
}

/// Start a cabal: the name, then the rules it runs on — who can join, who votes, what it takes
/// to pass, and how long a vote stays open. Nothing about money: the pot starts empty and fills
/// once members add to it.
struct CreateGroupView: View {
    @ObservedObject var auth: PrivyAuthService
    /// Present inside the signed-in shell; lightweight session patch after create.
    @Environment(AppSessionStore.self) private var session: AppSessionStore?
    /// The cabal exists. The owner of the stack replaces this form with it, so
    /// Back lands on the Cabals tab instead of on a form that is still armed.
    let onCreated: (CreateGroupResponse) -> Void

    private let actions: CabalsActionSource

    init(
        auth: PrivyAuthService,
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
    @State private var toast: MonacoToast?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                nameField
                    .padding(.horizontal, MonacoTheme.Space.m)
                rules
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .scrollDismissesKeyboard(.interactively)
        .monacoCanvas()
        .navigationTitle(CabalRulesCopy.screenTitle)
        .navigationBarTitleDisplayMode(.inline)
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                Button(isCreating ? CabalRulesCopy.creating : CabalRulesCopy.create) {
                    Task { await createGroup() }
                }
                .buttonStyle(.monacoPrimary)
                .disabled(isCreating || !canSubmit)
                .accessibilityIdentifier("create-group-submit")
            }
        }
        .monacoToast($toast, placement: .aboveBottomCTA)
        // A create that did not go through says so the way every other action in the app does,
        // in a toast over the button rather than a banner inside the form.
        .onChange(of: errorMessage) { _, message in
            if let message { toast = MonacoToast(message: message) }
        }
    }

    /// The field keeps `create-group-name`: the UI tests type into it by that name. Return only
    /// puts the keyboard away; creating is the button's job, so a stray Return never starts a
    /// cabal before the rules have been read.
    private var nameField: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoTextField(CabalRulesCopy.namePlaceholder, text: $groupName)
                .submitLabel(.done)
                .disabled(isCreating)
                .accessibilityIdentifier("create-group-name")
            Text(CabalRulesCopy.nameHint)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
        }
    }

    private var rules: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(CabalRulesCopy.sectionTitle)
                .padding(.horizontal, MonacoTheme.Space.m)

            MonacoGroupedList {
                CabalRuleRow(
                    title: CabalRulesCopy.joinTitle,
                    options: JoinPolicyMode.allCases,
                    selection: $joinPolicy,
                    label: { $0.label },
                    caption: { $0.caption },
                    identifier: "create-rule-join"
                )
                CabalRuleRow(
                    title: CabalRulesCopy.votersTitle,
                    options: VoterSetMode.allCases,
                    selection: $voterSet,
                    label: { $0.label },
                    caption: { $0.caption },
                    identifier: "create-rule-voters"
                )
                CabalRuleRow(
                    title: CabalRulesCopy.thresholdTitle,
                    options: VoteThresholdMode.allCases,
                    selection: $threshold,
                    label: { $0.label },
                    caption: { $0.caption },
                    identifier: "create-rule-threshold"
                )
                CabalRuleRow(
                    title: CabalRulesCopy.expiryTitle,
                    options: VoteExpiryOption.allCases,
                    selection: $voteExpiry,
                    label: { $0.label },
                    caption: { $0.caption },
                    identifier: "create-rule-expiry",
                    isLast: true
                )
            }
            .disabled(isCreating)
        }
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

/// One rule on the Start a cabal screen: its name, what the current choice means, and the
/// choice under them. A ruled row, so the four rules read as one table.
private struct CabalRuleRow<Option: Hashable>: View {
    let title: String
    let options: [Option]
    @Binding var selection: Option
    let label: (Option) -> String
    let caption: (Option) -> String
    let identifier: String
    var isLast = false

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                    .accessibilityAddTraits(.isHeader)
                Text(caption(selection))
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                    .contentTransition(.opacity)
                    .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: selection)
            }
            CabalRuleChoice(options: options, selection: $selection, label: label)
        }
        .padding(MonacoTheme.Space.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule()
                    .padding(.leading, MonacoTheme.Space.m)
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier(identifier)
    }
}

/// The segmented control at the default text sizes. At the accessibility sizes the options
/// stack as full-width chips instead, where a two-up control would cut "Everyone agrees" short.
private struct CabalRuleChoice<Option: Hashable>: View {
    let options: [Option]
    @Binding var selection: Option
    let label: (Option) -> String

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(spacing: MonacoTheme.Space.s) {
                ForEach(options, id: \.self) { option in
                    stackedOption(option)
                }
            }
        } else {
            MonacoSegmented(options, selection: $selection, label: label)
        }
    }

    private func stackedOption(_ option: Option) -> some View {
        let isSelected = option == selection
        return Button {
            guard !isSelected else { return }
            Haptics.selection()
            selection = option
        } label: {
            HStack(spacing: MonacoTheme.Space.s) {
                Text(label(option))
                    .font(MonacoTheme.Typo.calloutStrong)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if isSelected {
                    Image(systemName: "checkmark")
                        .font(MonacoTheme.Typo.calloutStrong)
                        .accessibilityHidden(true)
                }
            }
            .foregroundStyle(isSelected ? MonacoTheme.onBrand : MonacoTheme.ink)
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.vertical, MonacoTheme.Space.s)
            .frame(minHeight: 44)
            .background(Capsule().fill(isSelected ? MonacoTheme.brandFill : MonacoTheme.surfaceSunken))
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isSelected ? [.isSelected] : [])
    }
}

#Preview {
    NavigationStack {
        CreateGroupView(auth: PrivyAuthService())
    }
}
