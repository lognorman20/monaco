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

    /// What it means in the member's own words, under the label.
    var detail: String {
        switch self {
        case .open: "Share the invite code and they're in."
        case .request: "Requests wait for you to say yes."
        }
    }

    var systemImage: String {
        switch self {
        case .open: "link"
        case .request: "hand.raised"
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

    var detail: String {
        switch self {
        case .allMembers: "Every member gets a vote on every buy."
        case .namedSubset: "You decide; everyone else can still fund the pot."
        }
    }

    var systemImage: String {
        switch self {
        case .allMembers: "person.2"
        case .namedSubset: "person"
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

/// Copy the create flow says out loud, kept pure so it can be read without a view.
enum CreateCabalCopy {
    /// The rule in a sentence a member would say, rather than a picker value they have to
    /// translate. "Everyone has to say yes before a buy goes through."
    static func passingSentence(voterSet: VoterSetMode, threshold: VoteThresholdMode) -> String {
        switch (voterSet, threshold) {
        case (.namedSubset, _):
            return "You're the only voter, so a buy goes through when you say yes."
        case (.allMembers, .majority):
            return "More than half the members have to say yes before a buy goes through."
        case (.allMembers, .unanimous):
            return "Every member has to say yes before a buy goes through."
        }
    }

    static func windowSentence(_ expiry: VoteExpiryOption) -> String {
        switch expiry {
        case .oneHour: return "A vote is open for an hour, then it closes."
        case .oneDay: return "A vote is open for a day, then it closes."
        case .sevenDays: return "A vote is open for a week, then it closes."
        }
    }
}

/// Start a cabal: name it, say who can join, say how a buy passes.
///
/// This was four wheel pickers in a `Form`. It is the moment a founder brings their friends into
/// the product, so it is three designed steps — with the mark recolouring and re-initialling live
/// as the name is typed, which is where the tint system stops being an implementation detail and
/// becomes a feature you can see.
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

    private var trimmedName: String {
        groupName.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The preview mark's colour, hashed from what has been typed so far.
    ///
    /// It is **not** the colour this cabal will end up with, and nothing on the screen says it is.
    /// The real tint is hashed from the server-issued id and then run through
    /// `CabalTintAssignment.resolve` against the founder's other cabals, so it is not knowable
    /// until the cabal exists — a colour a founder watched settle would change three seconds
    /// later, in front of them. The initials are the honest half and they are the half that
    /// matters: "Semis or bust" becomes SB, not SO. The copy under the mark says which is which.
    private var previewTint: MonacoTheme.CabalTint {
        .forGroupId(trimmedName.isEmpty ? "monaco" : trimmedName)
    }

    var body: some View {
        MonacoScreen {
            ScrollView {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.section) {
                    nameStep
                    joinStep
                    votesStep
                    if let errorMessage {
                        Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                            .font(MonacoTheme.Typo.caption)
                            // §5.6 maps a failed thing to the danger ramp; amber is "pending" and
                            // "closing soon". The cabal was not created, which is a failure.
                            .foregroundStyle(MonacoTheme.dangerOnWash)
                            .padding(MonacoTheme.Space.sm)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(
                                MonacoTheme.dangerWash,
                                in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.container, style: .continuous)
                            )
                            .accessibilityIdentifier("create-group-error")
                    }
                }
                .padding(.horizontal, MonacoTheme.Space.gutter)
                .padding(.bottom, MonacoTheme.Space.l)
            }
            .scrollDismissesKeyboard(.interactively)
            // On the scroll view, not on the screen: an identifier applied after the bottom
            // inset propagates into it and overwrites the Create button's own, so the one
            // control that finishes this flow could not be addressed. `GroupChatView` carries
            // the same note for the same reason.
            .accessibilityIdentifier("create-group-root")
        }
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                Button(isCreating ? "Creating…" : "Create cabal") {
                    Task { await createGroup() }
                }
                .buttonStyle(.monacoPrimary)
                .disabled(isCreating || !canSubmit)
                .accessibilityIdentifier("create-group-submit")
            }
        }
        .navigationTitle("New cabal")
        .navigationBarTitleDisplayMode(.inline)
    }

    // MARK: - Step 1

    private var nameStep: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.headerToContent) {
            CreateStepHeader(number: 1, title: "Name your cabal")
            HStack(spacing: MonacoTheme.Space.m) {
                // The one screen in the app that gets the live recolour (§4 #22, §5.11.1).
                CabalMark(
                    tint: previewTint,
                    name: trimmedName.isEmpty ? "?" : trimmedName,
                    size: 56,
                    animatesIdentity: true
                )
                .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(trimmedName.isEmpty ? "Your cabal" : trimmedName)
                        .displayFont(.section)
                        .foregroundStyle(trimmedName.isEmpty ? MonacoTheme.fgSubtle : MonacoTheme.fgPrimary)
                        .lineLimit(2)
                    Text("Your initials now — we'll give it a colour when it's created")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.fgMuted)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .accessibilityElement(children: .combine)

            MonacoTextField("Cabal name", text: $groupName)
                .disabled(isCreating)
                .accessibilityIdentifier("create-group-name")
            Text("Pick a name your friends will recognize.")
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.fgMuted)
        }
    }

    // MARK: - Step 2

    private var joinStep: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.headerToContent) {
            CreateStepHeader(number: 2, title: "Who can join?")
            VStack(spacing: MonacoTheme.Space.s) {
                ForEach(JoinPolicyMode.allCases) { mode in
                    CreateChoiceCard(
                        title: mode.label,
                        detail: mode.detail,
                        systemImage: mode.systemImage,
                        isSelected: joinPolicy == mode
                    ) { joinPolicy = mode }
                    .accessibilityIdentifier("create-group-join-\(mode.rawValue)")
                }
            }
            .disabled(isCreating)
        }
    }

    // MARK: - Step 3

    private var votesStep: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.headerToContent) {
            CreateStepHeader(number: 3, title: "How a buy passes")
            VStack(spacing: MonacoTheme.Space.s) {
                ForEach(VoterSetMode.allCases) { mode in
                    CreateChoiceCard(
                        title: mode.label,
                        detail: mode.detail,
                        systemImage: mode.systemImage,
                        isSelected: voterSet == mode
                    ) { voterSet = mode }
                    .accessibilityIdentifier("create-group-voters-\(mode.rawValue)")
                }
            }

            if voterSet == .allMembers {
                MonacoSegmented(VoteThresholdMode.allCases, selection: $threshold) { $0.label }
                    .accessibilityIdentifier("create-group-threshold")
            }

            MonacoSegmented(VoteExpiryOption.allCases, selection: $voteExpiry) { $0.label }
                .accessibilityIdentifier("create-group-expiry")

            // The rule, in a sentence, rather than three picker values the founder has to
            // translate for themselves. It rewrites as the choices above it change.
            VStack(alignment: .leading, spacing: 4) {
                Text(CreateCabalCopy.passingSentence(voterSet: voterSet, threshold: threshold))
                Text(CreateCabalCopy.windowSentence(voteExpiry))
                    .foregroundStyle(MonacoTheme.fgMuted)
            }
            .font(MonacoTheme.Typo.callout)
            .foregroundStyle(MonacoTheme.fgPrimary)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(MonacoTheme.Space.m)
            .background(
                MonacoTheme.bgSunken,
                in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.container, style: .continuous)
            )
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("create-group-summary")
        }
        .disabled(isCreating)
    }

    /// Only the name gates the button. "Just me" needs the creator's id, but
    /// that is a reason to say so when the member taps — not to hand them a
    /// dead button with no explanation.
    private var canSubmit: Bool {
        !trimmedName.isEmpty
    }

    private func createGroup() async {
        // The disabled state only lands on the next render, so a fast double tap
        // gets through it. Same guard the money screens use.
        guard !isCreating else { return }

        let trimmedName = self.trimmedName
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

/// A numbered step heading. The numeral is the only thing on this screen that says "there are
/// three of these and you are on the second", which is what a wheel picker never told anybody.
///
/// It is set as an eyebrow above the title, not as a disc. Nothing about a step number is
/// tappable and §0 rule 2 is that brand blue is the interactive accent and nothing else carries
/// it — a numbered blue circle is both a broken rule and the exact onboarding furniture §5.3.2
/// deletes four brand-wash circles for. The eyebrow is §2.2's highest-leverage style and it is
/// free here.
private struct CreateStepHeader: View {
    let number: Int
    let title: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Step \(number)")
                .displayFont(.eyebrow)
                .foregroundStyle(MonacoTheme.fgMuted)
                .fixedSize(horizontal: false, vertical: true)
            Text(title)
                .displayFont(.section)
                .foregroundStyle(MonacoTheme.fgPrimary)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Step \(number). \(title)")
    }
}

/// One large selectable card. Selection is a brand stroke and a check, never a tint: blue means
/// tap, and this is a control.
private struct CreateChoiceCard: View {
    let title: String
    let detail: String
    let systemImage: String
    let isSelected: Bool
    let select: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Button {
            Haptics.selection()
            select()
        } label: {
            HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
                Image(systemName: systemImage)
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(isSelected ? MonacoTheme.brandOnWash : MonacoTheme.fgMuted)
                    .frame(width: 32, height: 32)
                    .background(Circle().fill(isSelected ? MonacoTheme.brandWash : MonacoTheme.fillQuiet))
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(MonacoTheme.Typo.rowTitle)
                        .foregroundStyle(MonacoTheme.fgPrimary)
                        .fixedSize(horizontal: false, vertical: true)
                    Text(detail)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.fgMuted)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Image(systemName: isSelected ? "checkmark.circle.fill" : "circle")
                    .font(.system(size: 18))
                    .foregroundStyle(isSelected ? MonacoTheme.brand : MonacoTheme.line)
                    .padding(.top, 6)
            }
            .multilineTextAlignment(.leading)
            .padding(MonacoTheme.Space.m)
            .frame(maxWidth: .infinity, minHeight: 64, alignment: .leading)
            .background(
                MonacoTheme.bgRaised,
                in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.container, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: MonacoTheme.Radius.container, style: .continuous)
                    .strokeBorder(
                        isSelected ? MonacoTheme.brand : MonacoTheme.line,
                        lineWidth: isSelected ? 2 : 1
                    )
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .animation(MonacoMotion.snap.reduced(reduceMotion), value: isSelected)
        .accessibilityAddTraits(isSelected ? [.isButton, .isSelected] : .isButton)
    }
}

#Preview {
    NavigationStack {
        CreateGroupView(auth: DynamicAuthService())
    }
}
