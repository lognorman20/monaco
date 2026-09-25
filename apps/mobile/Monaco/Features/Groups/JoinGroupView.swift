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

/// The join screen's titles and lines, so the button a member taps always matches what the
/// row they came from said about the cabal. Pure, so it is pinned by a test.
enum JoinCabalScreenCopy {
    /// The same for every route. The button already says "Ask to join" when the admin approves
    /// members; saying it in the bar too put the same words twice on one screen.
    static let title = "Join a cabal"

    /// "Join" when the screen already names the cabal above the button, "Ask to join" when its
    /// admin approves members, and "Join cabal" for a pasted code, whose cabal is not known yet.
    static func actionTitle(joinMode: GroupJoinMode?, isJoining: Bool, requestPending: Bool) -> String {
        if isJoining { return joinMode == .request ? "Sending…" : "Joining…" }
        if requestPending { return "Request sent" }
        switch joinMode {
        case .request: return "Ask to join"
        case .open: return "Join"
        case nil: return "Join cabal"
        }
    }

    /// "9 members" under the cabal's name. Nil when the route did not carry a count, rather
    /// than a guess.
    static func memberLine(_ count: Int?) -> String? {
        guard let count, count > 0 else { return nil }
        return count == 1 ? "1 member" : "\(count) members"
    }

    static func explanation(joinMode: GroupJoinMode?) -> String {
        joinMode == .request
            ? "The cabal admin approves new members. You'll show up once they say yes."
            : "Anyone can join this cabal. You can add money after you're in."
    }
}

/// Join a cabal by pasted invite code, or from a search/board row that already knows the
/// cabal's name and join policy — in which case the screen leads with the cabal itself.
struct JoinGroupView: View {
    @ObservedObject var auth: PrivyAuthService
    /// Present inside the signed-in shell; refreshed after a join so every tab updates.
    @Environment(AppSessionStore.self) private var session: AppSessionStore?
    private let actions: CabalsActionSource
    private let groupName: String?
    private let joinMode: GroupJoinMode?
    /// What the row knew about the cabal. Both optional: the Cabals route carries neither yet,
    /// and the screen draws the tinted initials and leaves the member line out without them.
    private let memberCount: Int?
    private let pictureUrl: String?
    /// The viewer is a member now. The owner of the stack takes it from here —
    /// this screen never pushes the cabal itself, so Back cannot land back on a
    /// join form for a cabal the member is already in.
    private let onJoined: (_ groupId: String, _ groupName: String?) -> Void
    @State private var groupId: String
    @State private var requestPending = false
    @State private var isJoining = false
    @State private var toast: MonacoToast?

    init(
        auth: PrivyAuthService,
        groupId: String = "",
        groupName: String? = nil,
        joinMode: GroupJoinMode? = nil,
        memberCount: Int? = nil,
        pictureUrl: String? = nil,
        actions: CabalsActionSource? = nil,
        onJoined: @escaping (_ groupId: String, _ groupName: String?) -> Void = { _, _ in }
    ) {
        self.auth = auth
        self.actions = actions ?? LiveCabalsActionSource(auth: auth)
        self.groupName = groupName
        self.joinMode = joinMode
        self.memberCount = memberCount
        self.pictureUrl = pictureUrl
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
        JoinCabalScreenCopy.actionTitle(joinMode: joinMode, isJoining: isJoining, requestPending: requestPending)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                if let groupName {
                    cabalHeader(groupName)
                } else {
                    codeEntry
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .scrollDismissesKeyboard(.interactively)
        .monacoCanvas()
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                Button(actionTitle) {
                    Task { await joinGroup() }
                }
                .buttonStyle(.monacoPrimary)
                .disabled(isJoining || requestPending || !canSubmit)
                .accessibilityIdentifier("join-group-submit")
            }
        }
        .monacoToast($toast, placement: .aboveBottomCTA)
        .navigationTitle(JoinCabalScreenCopy.title)
        .navigationBarTitleDisplayMode(.inline)
    }

    // MARK: - A cabal the row already knows

    private func cabalHeader(_ name: String) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
            HStack(spacing: MonacoTheme.Space.sm) {
                CabalMark(groupId: trimmedId, name: name, size: 56, pictureUrl: pictureUrl)
                VStack(alignment: .leading, spacing: 2) {
                    // Its own element, labelled with the name alone: the join UI tests read
                    // the label back, so the member line must not be combined into it.
                    Text(name)
                        .font(MonacoTheme.Typo.title)
                        .foregroundStyle(MonacoTheme.ink)
                        .lineLimit(2)
                        .minimumScaleFactor(0.8)
                        .accessibilityAddTraits(.isHeader)
                        .accessibilityIdentifier("join-group-name")
                    if let members = JoinCabalScreenCopy.memberLine(memberCount) {
                        Text(members)
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.muted)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            Text(JoinCabalScreenCopy.explanation(joinMode: joinMode))
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    // MARK: - A pasted invite code

    private var codeIsMalformed: Bool {
        !trimmedId.isEmpty && !isCodeWellFormed
    }

    private var codeEntry: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            InviteCodeField(code: $groupId, isDisabled: isJoining || requestPending)
            Text(codeIsMalformed ? JoinCabalCopy.malformedCode : JoinCabalCopy.codeFooter)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(codeIsMalformed ? MonacoTheme.warning : MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
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

/// The invite code field: the `MonacoTextField` anatomy, with the code in the market's mono —
/// a code is data, not words — and the system Paste button inside it, which reads the
/// clipboard without the paste prompt.
private struct InviteCodeField: View {
    @Binding var code: String
    let isDisabled: Bool

    @FocusState private var focused: Bool

    var body: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            TextField(
                "",
                text: $code,
                prompt: Text(JoinCabalCopy.codeLabel).foregroundStyle(MonacoTheme.disabledLabel)
            )
            .font(MonacoTheme.Typo.data)
            .foregroundStyle(MonacoTheme.ink)
            .tint(MonacoTheme.ink)
            .keyboardType(.asciiCapable)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .submitLabel(.done)
            .focused($focused)
            .disabled(isDisabled)
            .frame(maxWidth: .infinity, minHeight: 56)
            .contentShape(Rectangle())
            .onTapGesture { focused = true }
            .accessibilityLabel(JoinCabalCopy.codeLabel)
            .accessibilityIdentifier("join-group-id")

            PasteButton(payloadType: String.self) { strings in
                guard let pasted = strings.first else { return }
                Task { @MainActor in
                    code = pasted.trimmingCharacters(in: .whitespacesAndNewlines)
                }
            }
            .labelStyle(.iconOnly)
            .buttonBorderShape(.capsule)
            // The Paste button draws its glyph in white on the tint in both schemes, so the tint
            // is the ink that stays dark in both rather than `brandFill`, which goes light in dark.
            .tint(MonacoTheme.heroInk)
            .disabled(isDisabled)
            .accessibilityIdentifier("join-group-paste")
        }
        .padding(.leading, MonacoTheme.Space.m)
        .padding(.trailing, MonacoTheme.Space.s)
        .background(
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
                .fill(MonacoTheme.surfaceSunken)
        )
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
                .strokeBorder(MonacoTheme.ink, lineWidth: focused ? 1 : 0)
        }
        .animation(.easeOut(duration: 0.15), value: focused)
    }
}

#Preview {
    NavigationStack {
        JoinGroupView(
            auth: PrivyAuthService(),
            groupId: "5b1f0c9e-0005-4c55-9a51-000000000005",
            groupName: "Weekend investors",
            joinMode: .request
        )
    }
}
