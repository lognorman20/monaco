import Foundation
import MonacoCore

/// The invite card on the cabal's details sheet, in the member's words.
enum CabalInviteCopy {
    static let codeLabel = JoinCabalCopy.codeLabel
    static let share = "Share invite"
    static let copyLink = "Copy link"
    static let copyCode = "Copy code"
    static let copied = "Copied"
    static let newCode = "New code"
    static let renewing = "Making a new code…"
    static let newCodeTitle = "Make a new invite code?"
    static let newCodeMessage = "The current link and code stop working. Everyone already in stays in."
    static let newCodeConfirm = "Make a new code"
    static let newCodeDone = "New code ready. The old link no longer works."
    static let loadFailedMessage = "Couldn't load the invite code."
    static let retry = "Try again"
    static let qrLabel = "QR code of the invite link"

    static func hint(cabalName: String) -> String {
        "Friends scan this or open the link to join \(cabalName)."
    }

    static func newCodeFailed(_ error: Error) -> String {
        if InviteErrorStatus.isOffline(error) { return "You're offline. Try again when you're back." }
        if InviteErrorStatus.of(error) == 429 { return "Too many new codes. Wait a moment and try again." }
        return "Couldn't make a new code. Try again."
    }

    static func loadFailed(_ error: Error) -> String {
        if InviteErrorStatus.isOffline(error) { return "You're offline. Try again when you're back." }
        return loadFailedMessage
    }

    static let auditedStrings: [String] = [
        codeLabel, share, copyLink, copyCode, copied, newCode, renewing, newCodeTitle, newCodeMessage, newCodeConfirm,
        newCodeDone, loadFailedMessage, retry, qrLabel, hint(cabalName: "Sunday Investors"),
        "You're offline. Try again when you're back.", "Too many new codes. Wait a moment and try again.",
        "Couldn't make a new code. Try again.",
    ]
}

/// The join screen's lines about a typed or pasted invite.
enum InviteEntryCopy {
    static let footer = "Paste the code or link your friend shared."
    static let typing = "Invite codes are eight letters and numbers."
    static let notFound = "No cabal has this code. Ask your friend for a new link."
    static let unavailable = "Couldn't load this cabal. You can still join with the code."
    static let notAnInvite = "Your clipboard doesn't hold an invite code or link."
    static let joinedWithoutCabal = "You're in. Find the cabal on the Cabals tab."
    static let retryPreview = "Load the cabal again"

    /// "9 members · $1,240.50 in the pot". Either half is left out when the preview lacks it.
    static func facts(for preview: InvitePreviewDTO) -> String {
        [JoinCabalScreenCopy.memberLine(preview.memberCount), potLine(preview.potValueUsd)]
            .compactMap { $0 }
            .joined(separator: " · ")
    }

    static func potLine(_ potValueUsd: String) -> String? {
        let trimmed = potValueUsd.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let value = Decimal(string: trimmed, locale: Locale(identifier: "en_US_POSIX")) else { return nil }
        if value <= 0 { return "Nothing in the pot yet" }
        return "\(UsdAmountFormatter.format(decimal: value)) in the pot"
    }

    static let auditedStrings: [String] = [
        footer, typing, notFound, unavailable, notAnInvite, joinedWithoutCabal, retryPreview, "Nothing in the pot yet",
    ]
}
