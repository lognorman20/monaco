import Combine
import Foundation
import MonacoCore
import SwiftUI
import UIKit

/// What Settings keeps on the phone, in `UserDefaults`: the app lock, the appearance, and a
/// mirror of the member's notification switches so the screen draws them before the network
/// answers. The switches themselves live on the server (`/v1/me/preferences`); the mirror only
/// saves a wait, and it is keyed to the member it belongs to.
@MainActor
final class SettingsStore: ObservableObject {
    static let shared = SettingsStore()

    /// Every key this store writes. Nothing else touches them.
    enum Key {
        static let lockEnabled = "monaco.settings.lockEnabled"
        static let lockTimeout = "monaco.settings.lockTimeout"
        static let appearance = "monaco.settings.appearance"
        static let notifications = "monaco.settings.notifications"
        static let notificationsOwner = "monaco.settings.notificationsOwner"
    }

    @Published var lockEnabled: Bool {
        didSet { defaults.set(lockEnabled, forKey: Key.lockEnabled) }
    }

    @Published var lockTimeout: AppLockTimeout {
        didSet { defaults.set(lockTimeout.rawValue, forKey: Key.lockTimeout) }
    }

    @Published var appearance: AppearanceChoice {
        didSet { defaults.set(appearance.rawValue, forKey: Key.appearance) }
    }

    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        lockEnabled = defaults.bool(forKey: Key.lockEnabled)
        lockTimeout = defaults.string(forKey: Key.lockTimeout).flatMap(AppLockTimeout.init(rawValue:)) ?? .immediately
        appearance = defaults.string(forKey: Key.appearance).flatMap(AppearanceChoice.init(rawValue:)) ?? .system
    }

    /// The switches last seen for `userId`, or all on when the mirror belongs to someone else
    /// (another member signed in on this phone) or there is none yet.
    func notifications(for userId: String?) -> NotificationPreferencesDTO {
        guard let userId, defaults.string(forKey: Key.notificationsOwner) == userId,
              let data = defaults.data(forKey: Key.notifications),
              let stored = try? JSONDecoder().decode(NotificationPreferencesDTO.self, from: data)
        else { return .allOn }
        return stored
    }

    func mirrorNotifications(_ notifications: NotificationPreferencesDTO, for userId: String?) {
        guard let userId, let data = try? JSONEncoder().encode(notifications) else { return }
        defaults.set(data, forKey: Key.notifications)
        defaults.set(userId, forKey: Key.notificationsOwner)
    }

    /// After the account is deleted: the lock and the mirror were that member's. The
    /// appearance is the phone's and stays.
    func forgetAccount() {
        lockEnabled = false
        defaults.removeObject(forKey: Key.notifications)
        defaults.removeObject(forKey: Key.notificationsOwner)
    }
}

extension AppearanceChoice {
    /// Nil follows the system.
    var colorScheme: ColorScheme? {
        switch self {
        case .system: return nil
        case .light: return .light
        case .dark: return .dark
        }
    }

    /// For UIKit windows SwiftUI's preference does not reach (the lock window).
    var interfaceStyle: UIUserInterfaceStyle {
        switch self {
        case .system: return .unspecified
        case .light: return .light
        case .dark: return .dark
        }
    }
}
