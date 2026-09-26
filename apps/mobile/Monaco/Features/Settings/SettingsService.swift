import Combine
import Foundation
import MonacoCore
import PrivySDK

/// What the Settings screens ask of the outside world. `LiveSettingsService` is the app; the
/// sample harness answers with canned data.
@MainActor
protocol SettingsService: AnyObject {
    /// The phone number or email the member signs in with, as Privy has it.
    func signInIdentities() async -> [SignInIdentity]
    func preferences() async throws -> PreferencesDTO
    func setNotifications(_ changes: [NotificationCategory: Bool]) async throws -> PreferencesDTO
    func deletionCheck() async throws -> DeletionCheckDTO
    func deleteAccount() async throws -> AccountDeletionOutcome
}

@MainActor
final class LiveSettingsService: SettingsService {
    private let auth: PrivyAuthService

    init(auth: PrivyAuthService) {
        self.auth = auth
    }

    func signInIdentities() async -> [SignInIdentity] {
        guard let user = await auth.privy.getUser() else { return [] }
        let identities = user.linkedAccounts.compactMap { account -> SignInIdentity? in
            switch account {
            case .phone(let phone): return .phone(phone.phoneNumber)
            case .email(let email): return .email(email.email)
            default: return nil
            }
        }
        // Phone first: it is how most members get their code.
        func rank(_ identity: SignInIdentity) -> Int {
            if case .phone = identity { return 0 }
            return 1
        }
        return identities.sorted { rank($0) < rank($1) }
    }

    func preferences() async throws -> PreferencesDTO {
        try await auth.withAccessToken { token in
            try await Self.client(token).getPreferences()
        }
    }

    func setNotifications(_ changes: [NotificationCategory: Bool]) async throws -> PreferencesDTO {
        try await auth.withAccessToken { token in
            try await Self.client(token).updateNotificationPreferences(changes)
        }
    }

    func deletionCheck() async throws -> DeletionCheckDTO {
        try await auth.withAccessToken { token in
            try await Self.client(token).getDeletionCheck()
        }
    }

    func deleteAccount() async throws -> AccountDeletionOutcome {
        try await auth.withAccessToken { token in
            try await Self.client(token).deleteAccount()
        }
    }

    /// A client pinned to the token the call runs under, so a 401 is reported against the
    /// token that was actually rejected (`PrivyAuthService.withAccessToken`).
    private static func client(_ token: String) -> MonacoCore.MonacoAPIClient {
        MonacoCore.MonacoAPIClient(baseURL: Config.apiBaseURL, accessTokenProvider: { token })
    }
}

/// The Settings screen's state: the member's notification switches and how they sign in.
///
/// The switches draw from the phone's mirror first, then from the server. A flipped switch
/// moves at once and saves in the background; if the save fails it moves back and a toast
/// says so. Only the switch a save was about is taken from its answer, so two quick flips
/// cannot land out of order and undo each other.
@MainActor
final class SettingsModel: ObservableObject {
    @Published private(set) var notifications: NotificationPreferencesDTO
    @Published private(set) var identities: [SignInIdentity] = []
    @Published private(set) var hasLoadedIdentities = false
    @Published var toast: MonacoToast?

    private let service: SettingsService
    private let store: SettingsStore
    private let userId: String?
    @Published private var inFlight: [NotificationCategory: Int] = [:]
    private var writeGeneration = 0

    init(service: SettingsService, store: SettingsStore, userId: String?) {
        self.service = service
        self.store = store
        self.userId = userId
        notifications = store.notifications(for: userId)
    }

    func load() async {
        // Privy answers from the phone, the preferences from the server: neither waits on the other.
        let identitiesLoad = Task { await service.signInIdentities() }
        let generation = writeGeneration
        if let server = try? await service.preferences(), generation == writeGeneration, inFlight.isEmpty {
            // Quiet on failure: the mirror is already on screen.
            apply(server.notifications)
        }
        identities = await identitiesLoad.value
        hasLoadedIdentities = true
    }

    func isSaving(_ category: NotificationCategory) -> Bool {
        inFlight[category] != nil
    }

    func setNotification(_ category: NotificationCategory, on: Bool) async {
        guard notifications[category] != on else { return }
        let before = notifications[category]
        writeGeneration += 1
        let generation = writeGeneration
        inFlight[category] = generation
        notifications[category] = on
        store.mirrorNotifications(notifications, for: userId)

        do {
            let server = try await service.setNotifications([category: on])
            guard inFlight[category] == generation else { return }
            inFlight[category] = nil
            notifications[category] = server.notifications[category]
            store.mirrorNotifications(notifications, for: userId)
        } catch {
            guard inFlight[category] == generation else { return }
            inFlight[category] = nil
            notifications[category] = before
            store.mirrorNotifications(notifications, for: userId)
            if !error.isRequestCancellation {
                Haptics.warning()
                toast = MonacoToast(message: SettingsCopy.notificationsSaveFailed)
            }
        }
    }

    private func apply(_ server: NotificationPreferencesDTO) {
        notifications = server
        store.mirrorNotifications(server, for: userId)
    }
}
