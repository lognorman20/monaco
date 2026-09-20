import Foundation
import UIKit
import UniformTypeIdentifiers

/// Copies a credential the way a credential should be copied.
///
/// A plain `UIPasteboard.general.string = key` hands the bot key to Universal Clipboard, so it
/// lands on the member's other devices, and leaves it there until something else is copied. The
/// key can spend the cabal's money, so it goes on this device only and clears itself shortly
/// after the member has had time to paste it.
enum SecretPasteboard {
    /// Long enough to switch apps and paste, short enough that the key is not still there later.
    static let lifetime: TimeInterval = 120

    static func copy(_ secret: String, to pasteboard: UIPasteboard = .general, now: Date = Date()) {
        pasteboard.setItems([[UTType.utf8PlainText.identifier: secret]], options: options(now: now))
    }

    static func options(now: Date = Date()) -> [UIPasteboard.OptionsKey: Any] {
        [.localOnly: true, .expirationDate: now.addingTimeInterval(lifetime)]
    }
}
