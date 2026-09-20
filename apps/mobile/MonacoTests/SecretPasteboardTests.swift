import Foundation
import Testing
import UIKit
import UniformTypeIdentifiers
@testable import Monaco

@MainActor
struct SecretPasteboardTests {
    @Test func aSecretStaysOnThisDevice() {
        // Without this the bot key rides Universal Clipboard to the member's other devices.
        let options = SecretPasteboard.options()
        #expect(options[.localOnly] as? Bool == true)
    }

    @Test func aSecretClearsItselfShortlyAfterTheCopy() {
        let now = Date(timeIntervalSince1970: 1_700_000_000)
        let expiry = SecretPasteboard.options(now: now)[.expirationDate] as? Date
        #expect(expiry == now.addingTimeInterval(120))
    }

    @Test func theSecretIsWhatGetsPasted() {
        let pasteboard = UIPasteboard.withUniqueName()
        defer { UIPasteboard.remove(withName: pasteboard.name) }
        SecretPasteboard.copy("mk_live_abc123", to: pasteboard)
        #expect(pasteboard.string == "mk_live_abc123")
    }
}
