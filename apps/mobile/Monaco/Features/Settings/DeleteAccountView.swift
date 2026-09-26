import Combine
import MonacoCore
import SwiftUI

/// Where the delete screen is: checking, stuck on money still in the account, clear to go, or
/// unable to check.
@MainActor
final class DeleteAccountModel: ObservableObject {
    enum Phase: Equatable {
        case loading
        case failed
        case blocked([DeletionBlockerDTO])
        case ready
    }

    @Published private(set) var phase: Phase = .loading
    @Published private(set) var isDeleting = false
    @Published private(set) var deleteError: String?

    private let service: SettingsService

    init(service: SettingsService) {
        self.service = service
    }

    /// Asks the server what is still in the account. Loud on failure: the member asked.
    func load() async {
        if case .blocked = phase {} else { phase = .loading }
        do {
            let check = try await service.deletionCheck()
            phase = check.canDelete ? .ready : .blocked(check.blockers)
        } catch {
            if error.isRequestCancellation { return }
            phase = .failed
        }
    }

    /// Deletes the account. True once the server has deleted it; a `409` puts the blockers it
    /// sent back on screen instead.
    func delete() async -> Bool {
        guard !isDeleting else { return false }
        isDeleting = true
        deleteError = nil
        defer { isDeleting = false }
        do {
            switch try await service.deleteAccount() {
            case .deleted:
                return true
            case .blocked(let check):
                phase = check.blockers.isEmpty ? .ready : .blocked(check.blockers)
                if check.blockers.isEmpty { deleteError = SettingsCopy.deleteFailed }
                return false
            }
        } catch {
            if !error.isRequestCancellation {
                Haptics.warning()
                deleteError = Self.message(for: error)
            }
            return false
        }
    }

    static func message(for error: Error) -> String {
        if case MonacoCore.MonacoAPIError.rateLimited(let retryAfter, _) = error {
            if let retryAfter, retryAfter > 0 {
                return "Too many tries. Try again in \(retryAfter)s."
            }
            return "Too many tries. Try again in a minute."
        }
        return SettingsCopy.deleteFailed
    }
}

/// Deleting the account, as App Review asks for it: what happens in plain words, what still
/// has to come out first (each with its way out), then a typed confirmation and one
/// destructive button. On success the session ends and sign-in says the account is gone.
struct DeleteAccountView: View {
    @ObservedObject var auth: PrivyAuthService
    let onDeleted: (() async -> Void)?

    @StateObject private var model: DeleteAccountModel
    @EnvironmentObject private var appLock: AppLock
    @State private var typed: String
    @FocusState private var confirmFocused: Bool
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    init(
        auth: PrivyAuthService,
        service: any SettingsService,
        onDeleted: (() async -> Void)?,
        initialConfirmation: String = ""
    ) {
        self.auth = auth
        self.onDeleted = onDeleted
        _model = StateObject(wrappedValue: DeleteAccountModel(service: service))
        _typed = State(initialValue: initialConfirmation)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                switch model.phase {
                case .loading:
                    introAndDetails(showsMoneyRule: true)
                    loading
                case .failed:
                    introAndDetails(showsMoneyRule: true)
                    EmptyState(
                        title: SettingsCopy.checkFailedTitle,
                        message: SettingsCopy.checkFailedMessage,
                        actionTitle: SettingsCopy.tryAgain,
                        action: { Task { await model.load() } }
                    )
                    .accessibilityIdentifier("delete-account-failed")
                case .blocked(let blockers):
                    // What to move out is what the member can act on, so it comes before the
                    // rest of what deleting does.
                    intro
                    blockersSection(blockers)
                    details(showsMoneyRule: false)
                case .ready:
                    introAndDetails(showsMoneyRule: false)
                    confirmSection
                }
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .scrollDismissesKeyboard(.interactively)
        .monacoCanvas()
        .navigationTitle(SettingsCopy.deleteTitle)
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await model.load() }
        .task { await model.load() }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("delete-account-root")
    }

    // MARK: What happens

    private var intro: some View {
        Text(SettingsCopy.deleteIntro)
            .font(MonacoTheme.Typo.bodyStrong)
            .foregroundStyle(MonacoTheme.ink)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, MonacoTheme.Space.m)
    }

    private func introAndDetails(showsMoneyRule: Bool) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            intro
            details(showsMoneyRule: showsMoneyRule)
        }
    }

    /// What deleting does. The money rule is left out once the screen shows the money itself
    /// (the blockers) or says there is none.
    private func details(showsMoneyRule: Bool) -> some View {
        let lines = (showsMoneyRule ? [SettingsCopy.deleteMoneyFirst] : []) + [
            SettingsCopy.deleteWhatGoes,
            SettingsCopy.deleteWhatStays,
            SettingsCopy.deleteSameLogin,
        ]
        return VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            ForEach(lines, id: \.self) { line in
                Text(line)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.muted)
            }
        }
        .fixedSize(horizontal: false, vertical: true)
        .padding(.horizontal, MonacoTheme.Space.m)
        .accessibilityElement(children: .combine)
    }

    // MARK: Loading

    private var loading: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            SkeletonBlock(width: 180, height: 18)
                .frame(height: 27)
                .padding(.horizontal, MonacoTheme.Space.m)
            MonacoGroupedList {
                ForEach(0..<2, id: \.self) { index in
                    HStack(spacing: MonacoTheme.Space.sm) {
                        SkeletonBlock(width: 44, height: 44, radius: MonacoTheme.Radius.tile)
                        VStack(alignment: .leading, spacing: 6) {
                            SkeletonBlock(width: 140, height: 14)
                            SkeletonBlock(width: 180, height: 12)
                        }
                        Spacer(minLength: MonacoTheme.Space.sm)
                        SkeletonBlock(width: 64, height: 14)
                    }
                    .settingsRow(isLast: index == 1)
                }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Checking your account")
        .accessibilityIdentifier("delete-account-loading")
    }

    // MARK: Blockers

    private func blockersSection(_ blockers: [DeletionBlockerDTO]) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            // At accessibility sizes the title needs the whole line, so "Check again" moves
            // under the list instead of truncating the header.
            if dynamicTypeSize.isAccessibilitySize {
                MonacoSectionHeader(SettingsCopy.blockersSection)
                    .padding(.horizontal, MonacoTheme.Space.m)
            } else {
                MonacoSectionHeader(SettingsCopy.blockersSection, trailing: SettingsCopy.checkAgain) {
                    Task { await model.load() }
                }
                .padding(.horizontal, MonacoTheme.Space.m)
            }
            MonacoGroupedList {
                ForEach(blockers) { blocker in
                    blockerRow(blocker, isLast: blocker.id == blockers.last?.id)
                }
            }
            if dynamicTypeSize.isAccessibilitySize {
                Button(SettingsCopy.checkAgain) {
                    Task { await model.load() }
                }
                .buttonStyle(.monacoSecondary)
                .padding(.horizontal, MonacoTheme.Space.m)
                .padding(.top, MonacoTheme.Space.s)
            }
        }
        .accessibilityIdentifier("delete-account-blockers")
    }

    @ViewBuilder
    private func blockerRow(_ blocker: DeletionBlockerDTO, isLast: Bool) -> some View {
        switch blocker.kind {
        case .cabalSlice:
            if let groupId = blocker.groupId {
                NavigationLink {
                    GroupDetailView(auth: auth, groupId: groupId, groupName: blocker.groupName ?? "")
                } label: {
                    blockerRowContent(blocker, isLast: isLast, isAction: true)
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("delete-account-blocker-\(groupId)")
            } else {
                blockerRowContent(blocker, isLast: isLast, isAction: false)
            }
        case .accountBalance:
            NavigationLink {
                WithdrawView(auth: auth)
            } label: {
                blockerRowContent(blocker, isLast: isLast, isAction: true)
            }
            .buttonStyle(.monacoRow)
            .accessibilityIdentifier("delete-account-blocker-balance")
        case .cashOutPending, .transferPending, .unknown:
            blockerRowContent(blocker, isLast: isLast, isAction: false)
        }
    }

    private func blockerRowContent(_ blocker: DeletionBlockerDTO, isLast: Bool, isAction: Bool) -> some View {
        let copy = SettingsCopy.blocker(blocker)
        return MonacoRow(
            title: copy.title,
            subtitle: copy.wayOut,
            subtitleColor: isAction ? MonacoTheme.brand : MonacoTheme.muted,
            chevron: isAction,
            isLast: isLast,
            leading: { blockerMark(blocker) },
            trailing: {
                if let value = blocker.valueUsd {
                    MoneyText(decimalString: value, style: .row)
                } else {
                    Text(SettingsCopy.valueUnavailable)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                }
            }
        )
    }

    @ViewBuilder
    private func blockerMark(_ blocker: DeletionBlockerDTO) -> some View {
        switch blocker.kind {
        case .cabalSlice, .cashOutPending:
            if let groupId = blocker.groupId {
                CabalMark(groupId: groupId, name: blocker.groupName ?? "")
            } else {
                SunkenGlyphMark(systemImage: "person.3")
            }
        case .transferPending:
            SunkenGlyphMark(systemImage: "clock.arrow.circlepath")
        case .accountBalance:
            SunkenGlyphMark(systemImage: "dollarsign")
        case .unknown:
            SunkenGlyphMark(systemImage: "questionmark")
        }
    }

    // MARK: Confirm

    private var canDelete: Bool {
        DeleteConfirmation.matches(typed) && !model.isDeleting
    }

    private var confirmSection: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(SettingsCopy.confirmSection)
            Text(SettingsCopy.deleteNothingLeft)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.ink)
            Text(SettingsCopy.confirmPrompt)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.muted)
            TextField(
                "",
                text: $typed,
                prompt: Text(SettingsCopy.confirmPlaceholder).foregroundStyle(MonacoTheme.disabledLabel)
            )
            .font(MonacoTheme.Typo.ticker)
            .foregroundStyle(MonacoTheme.ink)
            .tint(MonacoTheme.ink)
            .textInputAutocapitalization(.characters)
            .autocorrectionDisabled()
            .submitLabel(.done)
            .focused($confirmFocused)
            .monacoFieldChrome(isFocused: confirmFocused)
            .accessibilityLabel(SettingsCopy.confirmPrompt)
            .accessibilityIdentifier("delete-account-confirm-field")
            .padding(.bottom, MonacoTheme.Space.s)

            Button {
                confirmFocused = false
                Task { await delete() }
            } label: {
                if model.isDeleting {
                    HStack(spacing: MonacoTheme.Space.s) {
                        ProgressView().tint(MonacoTheme.destructive)
                        Text(SettingsCopy.deleting)
                    }
                } else {
                    Text(SettingsCopy.deleteButton)
                }
            }
            .buttonStyle(.monacoDestructive)
            .monacoFullWidthButtons()
            .disabled(!canDelete)
            .accessibilityIdentifier("delete-account-confirm")

            if let error = model.deleteError {
                Text(error)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.destructive)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("delete-account-error")
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
    }

    private func delete() async {
        guard await model.delete() else { return }
        Haptics.success()
        // The lock and the notification mirror were this member's.
        appLock.forget()
        AccountFarewell.shared.toast = MonacoToast(message: SettingsCopy.deletedToast, isSuccess: true)
        if let onDeleted {
            await onDeleted()
        } else {
            await auth.logout()
        }
    }
}
