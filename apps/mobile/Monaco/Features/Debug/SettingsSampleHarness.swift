#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: Settings, Delete account and the lock screen from canned data, with no Privy,
/// no backend and none of the phone's real settings. Launch with
/// `-MonacoSettingsSample <settings|settingsAbout|deleteBlocked|deleteReady|deleteLoading|deleteFailed|lock>`.
enum SettingsSampleScenario: String, CaseIterable {
    /// The top of Settings: account, the lock on with Face ID, notifications.
    case settings
    /// Settings scrolled to the end: appearance and about.
    case settingsAbout
    /// Delete account with a slice, a cash out on its way and a balance still in the account.
    case deleteBlocked
    /// Delete account with nothing left in it and DELETE typed.
    case deleteReady
    case deleteLoading
    case deleteFailed
    case lock

    static var requested: SettingsSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: "-MonacoSettingsSample"),
              arguments.indices.contains(flag + 1)
        else { return nil }
        return SettingsSampleScenario(rawValue: arguments[flag + 1])
    }
}

struct SettingsSampleHarness: View {
    let scenario: SettingsSampleScenario
    @ObservedObject var auth: PrivyAuthService

    @State private var session: AppSessionStore
    @StateObject private var store: SettingsStore
    @StateObject private var lock: AppLock
    private let service: SampleSettingsService

    init(scenario: SettingsSampleScenario, auth: PrivyAuthService) {
        self.scenario = scenario
        self.auth = auth
        let store = Self.makeStore()
        _store = StateObject(wrappedValue: store)
        // Suspended: the sample shows the lock's setting, it never locks the harness.
        _lock = StateObject(wrappedValue: AppLock(settings: store, authenticator: SampleAuthenticator(), isSuspended: true))
        _session = State(initialValue: Self.makeSession())
        service = SampleSettingsService(scenario: scenario)
    }

    var body: some View {
        content
            .environment(session)
            .environmentObject(store)
            .environmentObject(lock)
    }

    @ViewBuilder
    private var content: some View {
        switch scenario {
        case .settings, .settingsAbout:
            NavigationStack {
                SettingsView(auth: auth, userId: Self.userId, service: service, onDeleted: {})
            }
            .defaultScrollAnchor(scenario == .settingsAbout ? .bottom : .top)
        case .deleteBlocked, .deleteReady, .deleteLoading, .deleteFailed:
            NavigationStack {
                DeleteAccountView(
                    auth: auth,
                    service: service,
                    onDeleted: {},
                    initialConfirmation: scenario == .deleteReady ? DeleteConfirmation.word : ""
                )
            }
        case .lock:
            LockScreenView(method: "Face ID", isAuthenticating: false, onUnlock: {})
        }
    }

    static let userId = "sample-user"

    /// Its own defaults suite, emptied on every launch, so the sample never reads or writes
    /// the lock a developer may have turned on for real.
    private static func makeStore() -> SettingsStore {
        let suite = "monaco.sample.settings"
        let defaults = UserDefaults(suiteName: suite) ?? .standard
        defaults.removePersistentDomain(forName: suite)
        let store = SettingsStore(defaults: defaults)
        store.lockEnabled = true
        store.lockTimeout = .fiveMinutes
        var notifications = NotificationPreferencesDTO.allOn
        notifications.chat = false
        store.mirrorNotifications(notifications, for: userId)
        return store
    }

    private static func makeSession() -> AppSessionStore {
        let session = AppSessionStore()
        session.isLoading = false
        session.me = MeResponse(
            userId: userId,
            displayName: "Logan Norman",
            memberWalletAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
            profilePhotoUrl: nil,
            createdAt: ISO8601DateFormatter().date(from: "2026-09-01T14:30:00Z")
        )
        return session
    }
}

private struct SampleAuthenticator: DeviceOwnerAuthenticating {
    func availableMethod() -> String? { "Face ID" }
    func authenticate(reason: String) async -> Bool { true }
}

/// Canned answers for each scenario. Nothing here reaches the network.
@MainActor
private final class SampleSettingsService: SettingsService {
    private let scenario: SettingsSampleScenario
    private var preferencesDoc: PreferencesDTO

    init(scenario: SettingsSampleScenario) {
        self.scenario = scenario
        var notifications = NotificationPreferencesDTO.allOn
        notifications.chat = false
        preferencesDoc = PreferencesDTO(notifications: notifications)
    }

    func signInIdentities() async -> [SignInIdentity] {
        [.phone("+14155557177")]
    }

    func preferences() async throws -> PreferencesDTO {
        preferencesDoc
    }

    func setNotifications(_ changes: [NotificationCategory: Bool]) async throws -> PreferencesDTO {
        for (category, on) in changes {
            preferencesDoc.notifications[category] = on
        }
        return preferencesDoc
    }

    func deletionCheck() async throws -> DeletionCheckDTO {
        switch scenario {
        case .deleteBlocked:
            return DeletionCheckDTO(canDelete: false, blockers: Self.blockers)
        case .deleteLoading:
            try await Task.sleep(for: .seconds(3600))
            throw CancellationError()
        case .deleteFailed:
            throw URLError(.notConnectedToInternet)
        default:
            return DeletionCheckDTO(canDelete: true, blockers: [])
        }
    }

    func deleteAccount() async throws -> AccountDeletionOutcome {
        if scenario == .deleteBlocked {
            return .blocked(DeletionCheckDTO(canDelete: false, blockers: Self.blockers))
        }
        return .deleted(deletedAt: Date())
    }

    private static let blockers = [
        DeletionBlockerDTO(kind: .cabalSlice, groupId: "3b1f6a52-sunday", groupName: "Sunday Investors", valueUsd: "245.12"),
        DeletionBlockerDTO(kind: .cashOutPending, groupId: "9a3f5b88-semis", groupName: "Semis or Bust", valueUsd: "58.40"),
        DeletionBlockerDTO(kind: .accountBalance, valueUsd: "30.00"),
    ]
}
#endif
