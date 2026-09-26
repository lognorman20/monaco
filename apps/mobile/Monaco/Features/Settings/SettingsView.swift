import MonacoCore
import StoreKit
import SwiftUI

/// Settings, reached from Profile: the account, the app lock, notifications, appearance, and
/// the legal and support links App Review asks for. Ruled sections on the paper, like every
/// other screen; the only card-like thing is a switch.
///
/// The store and the lock come from the environment (`monacoSettingsRoot()` puts them there),
/// so a sample harness can hand in its own and never touch the phone's real settings.
struct SettingsView: View {
    @ObservedObject var auth: PrivyAuthService
    /// Keys the phone's mirror of the notification switches to the member it belongs to.
    let userId: String?
    /// The app talks to the API; the sample harness answers with canned data.
    var service: (any SettingsService)?
    /// What happens once the account is deleted. The app signs out; see `DeleteAccountView`.
    var onDeleted: (() async -> Void)?

    @EnvironmentObject private var store: SettingsStore
    @EnvironmentObject private var appLock: AppLock

    var body: some View {
        SettingsScreen(
            auth: auth,
            store: store,
            appLock: appLock,
            service: service ?? LiveSettingsService(auth: auth),
            userId: userId,
            onDeleted: onDeleted
        )
    }
}

private struct SettingsScreen: View {
    @ObservedObject var auth: PrivyAuthService
    @ObservedObject var store: SettingsStore
    @ObservedObject var appLock: AppLock
    let service: any SettingsService
    let onDeleted: (() async -> Void)?

    @StateObject private var model: SettingsModel
    @Environment(AppSessionStore.self) private var session
    @Environment(\.openURL) private var openURL
    @Environment(\.requestReview) private var requestReview

    @State private var showNameEditor = false
    @State private var sheetURL: SheetURL?
    @State private var lockMethod: String?
    @State private var hasCheckedLockMethod = false
    @State private var isChangingLock = false

    init(
        auth: PrivyAuthService,
        store: SettingsStore,
        appLock: AppLock,
        service: any SettingsService,
        userId: String?,
        onDeleted: (() async -> Void)?
    ) {
        self.auth = auth
        self.store = store
        self.appLock = appLock
        self.service = service
        self.onDeleted = onDeleted
        _model = StateObject(wrappedValue: SettingsModel(service: service, store: store, userId: userId))
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                accountSection
                securitySection
                notificationsSection
                appearanceSection
                aboutSection
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .monacoCanvas()
        .navigationTitle(SettingsCopy.title)
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($model.toast)
        .sheet(item: $sheetURL) { link in
            SafariSheet(url: link.url)
                .ignoresSafeArea()
        }
        .sheet(isPresented: $showNameEditor) {
            nameEditor
        }
        .task {
            lockMethod = appLock.method
            hasCheckedLockMethod = true
            await model.load()
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("settings-root")
    }

    // MARK: Account

    private var displayName: String {
        let name = session.me?.displayName.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? SettingsCopy.noDisplayName : name
    }

    private var accountSection: some View {
        section(SettingsCopy.accountSection) {
            Button {
                showNameEditor = true
            } label: {
                SettingsRowLabel(systemImage: "person", title: SettingsCopy.displayName, subtitle: displayName, chevron: true)
                    .settingsRow()
            }
            .buttonStyle(.monacoRow)
            .accessibilityIdentifier("settings-display-name")

            if !model.hasLoadedIdentities {
                HStack(spacing: MonacoTheme.Space.sm) {
                    SkeletonBlock(width: 40, height: 40, radius: 20)
                        .frame(width: 44, height: 44)
                    SkeletonBlock(width: 64, height: 14)
                    Spacer(minLength: MonacoTheme.Space.sm)
                    SkeletonBlock(width: 112, height: 14)
                }
                .settingsRow()
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("Loading")
            } else {
                ForEach(Array(model.identities.enumerated()), id: \.offset) { _, identity in
                    SettingsRowLabel(systemImage: Self.glyph(for: identity), title: identity.label) {
                        Text(identity.masked)
                            .font(MonacoTheme.Typo.data)
                            .foregroundStyle(MonacoTheme.muted)
                            .lineLimit(1)
                    }
                    .settingsRow()
                    .accessibilityElement(children: .combine)
                    .accessibilityIdentifier("settings-identity-\(identity.label.lowercased())")
                }
            }

            NavigationLink {
                DeleteAccountView(auth: auth, service: service, onDeleted: onDeleted)
            } label: {
                SettingsRowLabel(
                    systemImage: "trash",
                    title: SettingsCopy.deleteAccount,
                    subtitle: SettingsCopy.deleteAccountSubtitle,
                    titleColor: MonacoTheme.destructive,
                    chevron: true
                )
                .settingsRow(isLast: true)
            }
            .buttonStyle(.monacoRow)
            .accessibilityIdentifier("settings-delete-account")
        }
    }

    private static func glyph(for identity: SignInIdentity) -> String {
        switch identity {
        case .phone: return "phone"
        case .email: return "envelope"
        }
    }

    private var nameEditor: some View {
        NavigationStack {
            ScrollView {
                ProfileNameEditor(auth: auth, initialDraft: session.me?.displayName) {
                    // Close first: the toast is on this screen, under the sheet.
                    showNameEditor = false
                    model.toast = MonacoToast(message: "Name updated.", isSuccess: true)
                }
            }
            .monacoCanvas()
            .navigationTitle("Edit profile")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { showNameEditor = false }
                }
            }
        }
        .presentationDetents([.medium])
    }

    // MARK: Security

    private var lockBinding: Binding<Bool> {
        Binding(
            get: { store.lockEnabled },
            set: { wanted in
                guard !isChangingLock else { return }
                isChangingLock = true
                Task {
                    let done = await appLock.setEnabled(wanted)
                    isChangingLock = false
                    if !done {
                        Haptics.warning()
                        model.toast = MonacoToast(message: SettingsCopy.lockFailed)
                    }
                }
            }
        )
    }

    private var lockSubtitle: String {
        guard lockMethod != nil || !hasCheckedLockMethod else { return SettingsCopy.lockUnavailable }
        return store.lockEnabled ? SettingsCopy.lockSubtitleOn : SettingsCopy.lockSubtitleOff
    }

    private var securitySection: some View {
        section(SettingsCopy.securitySection) {
            SettingsToggleRow(
                systemImage: LockScreenView.glyph(for: lockMethod),
                title: SettingsCopy.lockTitle(method: lockMethod ?? "Face ID"),
                subtitle: lockSubtitle,
                isOn: lockBinding
            )
            .disabled(isChangingLock || (hasCheckedLockMethod && lockMethod == nil && !store.lockEnabled))
            .accessibilityIdentifier("settings-lock-toggle")

            Menu {
                Picker(SettingsCopy.lockAfter, selection: $store.lockTimeout) {
                    ForEach(AppLockTimeout.allCases) { timeout in
                        Text(timeout.title).tag(timeout)
                    }
                }
            } label: {
                SettingsRowLabel(systemImage: "timer", title: SettingsCopy.lockAfter, isMuted: !store.lockEnabled) {
                    HStack(spacing: MonacoTheme.Space.xs) {
                        Text(store.lockTimeout.shortTitle)
                            .font(MonacoTheme.Typo.calloutStrong)
                            .lineLimit(1)
                            .foregroundStyle(store.lockEnabled ? MonacoTheme.brand : MonacoTheme.disabledLabel)
                        Image(systemName: "chevron.up.chevron.down")
                            .font(MonacoTheme.Typo.micro)
                            .foregroundStyle(store.lockEnabled ? MonacoTheme.brand : MonacoTheme.disabledLabel)
                            .accessibilityHidden(true)
                    }
                }
                .settingsRow(isLast: true)
            }
            .tint(MonacoTheme.ink)
            .disabled(!store.lockEnabled)
            .accessibilityLabel(SettingsCopy.lockAfter)
            .accessibilityValue(store.lockTimeout.title)
            .accessibilityIdentifier("settings-lock-after")
        }
    }

    // MARK: Notifications

    private func notificationBinding(_ category: NotificationCategory) -> Binding<Bool> {
        Binding(
            get: { model.notifications[category] },
            set: { on in Task { await model.setNotification(category, on: on) } }
        )
    }

    private static func glyph(for category: NotificationCategory) -> String {
        switch category {
        case .proposals: return "hand.raised"
        case .results: return "flag.checkered"
        case .chat: return "bubble.left"
        case .money: return "arrow.left.arrow.right"
        }
    }

    private var notificationsSection: some View {
        section(SettingsCopy.notificationsSection) {
            ForEach(NotificationCategory.allCases) { category in
                SettingsToggleRow(
                    systemImage: Self.glyph(for: category),
                    title: SettingsCopy.notificationTitle(category),
                    subtitle: SettingsCopy.notificationSubtitle(category),
                    isOn: notificationBinding(category),
                    isLast: category == NotificationCategory.allCases.last
                )
                .accessibilityIdentifier("settings-notify-\(category.rawValue)")
            }
        }
    }

    // MARK: Appearance

    private var appearanceSection: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(SettingsCopy.appearanceSection)
                .padding(.horizontal, MonacoTheme.Space.m)
            MonacoSegmented(AppearanceChoice.allCases, selection: $store.appearance) { $0.title }
                .accessibilityLabel(SettingsCopy.appearanceSection)
                .accessibilityIdentifier("settings-appearance")
                .padding(.horizontal, MonacoTheme.Space.m)
        }
    }

    // MARK: About

    private var version: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
    }

    private var build: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? ""
    }

    private var aboutSection: some View {
        section(SettingsCopy.aboutSection) {
            SettingsRowLabel(systemImage: "number", title: SettingsCopy.version) {
                Text(SettingsLinks.versionLabel(version: version, build: build))
                    .font(MonacoTheme.Typo.data)
                    .foregroundStyle(MonacoTheme.muted)
            }
            .settingsRow()
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("settings-version")

            linkRow(SettingsCopy.terms, systemImage: "doc.text", identifier: "settings-terms") {
                sheetURL = SheetURL(url: SettingsLinks.terms)
            }
            linkRow(SettingsCopy.privacy, systemImage: "shield", identifier: "settings-privacy") {
                sheetURL = SheetURL(url: SettingsLinks.privacy)
            }
            linkRow(SettingsCopy.contactSupport, systemImage: "envelope", identifier: "settings-support") {
                let mail = SettingsLinks.supportEmail(version: version, build: build, userId: session.me?.userId)
                openURL(mail) { opened in
                    if !opened {
                        model.toast = MonacoToast(message: SettingsCopy.noMailApp)
                    }
                }
            }
            linkRow(SettingsCopy.rate, systemImage: "star", identifier: "settings-rate", isLast: true) {
                requestReview()
            }
        }
    }

    private func linkRow(
        _ title: String,
        systemImage: String,
        identifier: String,
        isLast: Bool = false,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            SettingsRowLabel(systemImage: systemImage, title: title, chevron: true)
                .settingsRow(isLast: isLast)
        }
        .buttonStyle(.monacoRow)
        .accessibilityIdentifier(identifier)
    }

    private func section<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(title)
                .padding(.horizontal, MonacoTheme.Space.m)
            MonacoGroupedList {
                content()
            }
        }
    }
}
