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

    @Test func aSecretCopiedLongEnoughAgoIsGone() {
        // Through `copy`, the entry point the bot screens call: the two tests above check the
        // options in isolation, so a `pasteboard.string = secret` that ignored them would leave
        // all of them green and the key back on Universal Clipboard for good.
        let pasteboard = UIPasteboard.withUniqueName()
        defer { UIPasteboard.remove(withName: pasteboard.name) }
        SecretPasteboard.copy("mk_live_abc123", to: pasteboard, now: Date(timeIntervalSinceNow: -300))
        #expect(pasteboard.string == nil)
    }
}
