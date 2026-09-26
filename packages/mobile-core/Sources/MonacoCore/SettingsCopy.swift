import Foundation

/// Every word the Settings and Delete account screens say. Kept here so the copy audit can
/// read all of it, and so the app and its screenshots say the same thing.
public enum SettingsCopy {
    public static let title = "Settings"

    // MARK: Sections

    public static let accountSection = "Account"
    public static let securitySection = "Security"
    public static let notificationsSection = "Notifications"
    public static let appearanceSection = "Appearance"
    public static let aboutSection = "About"

    // MARK: Account

    public static let displayName = "Display name"
    public static let noDisplayName = "Not set"
    public static let deleteAccount = "Delete account"
    public static let deleteAccountSubtitle = "Close your account for good"

    // MARK: Security

    /// "Lock with Face ID", or Touch ID, Optic ID, or the passcode on a device without either.
    public static func lockTitle(method: String) -> String { "Lock with \(method)" }
    public static let lockSubtitleOn = "Asked for when Monaco opens"
    public static let lockSubtitleOff = "Anyone holding your phone can open Monaco"
    public static let lockUnavailable = "Set a passcode in iOS Settings to lock Monaco."
    public static let lockAfter = "Lock after"
    public static let unlockReason = "Unlock Monaco"
    public static let turnOnReason = "Turn on the lock for Monaco"
    public static let unlockButton = "Unlock"
    public static let lockFailed = "Couldn't turn on the lock. Try again."

    // MARK: Notifications

    public static func notificationTitle(_ category: NotificationCategory) -> String {
        switch category {
        case .proposals: return "Votes and proposals"
        case .results: return "Results and fills"
        case .chat: return "Chat"
        case .money: return "Money in and out"
        }
    }

    public static func notificationSubtitle(_ category: NotificationCategory) -> String {
        switch category {
        case .proposals: return "A new proposal, and votes on yours"
        case .results: return "When a vote closes and the buy fills"
        case .chat: return "Messages in your cabals"
        case .money: return "Money landing, cash outs, transfers"
        }
    }

    public static let notificationsSaveFailed = "Couldn't save that. Try again."

    // MARK: About

    public static let version = "Version"
    public static let terms = "Terms of service"
    public static let privacy = "Privacy policy"
    public static let contactSupport = "Contact support"
    public static let rate = "Rate Monaco"
    public static let noMailApp = "No mail app is set up. Write to support@monacolabs.xyz."

    // MARK: Delete account

    public static let deleteTitle = "Delete account"
    public static let deleteIntro = "Deleting your account closes it for good. You can't undo this."
    public static let deleteMoneyFirst = "Cash out every slice and your balance first."
    public static let deleteWhatGoes = "Your name and photo are removed, and you leave every cabal."
    public static let deleteWhatStays = "Your votes, trades and messages stay in each cabal's history as \"Deleted member\"."
    public static let deleteSameLogin = "You can't sign in again with the same phone number or email."
    public static let deleteNothingLeft = "Nothing is left in your account."
    public static let blockersSection = "Move these out first"
    public static let checkAgain = "Check again"
    public static let confirmSection = "Confirm"
    public static let confirmPrompt = "Type DELETE to confirm"
    public static let confirmPlaceholder = "DELETE"
    public static let deleteButton = "Delete my account"
    public static let deleting = "Deleting"
    public static let checkFailedTitle = "Couldn't check your account"
    public static let checkFailedMessage = "Check your connection and try again."
    public static let tryAgain = "Try again"
    public static let deleteFailed = "Couldn't delete your account. Try again."
    public static let deletedToast = "Your account is deleted."
    public static let deletedElsewhere = "This account was deleted."
    public static let valueUnavailable = "Value unavailable"

    /// What a blocker row says, and how to clear it.
    public struct BlockerCopy: Equatable, Sendable {
        public let title: String
        public let wayOut: String
    }

    public static func blocker(_ blocker: DeletionBlockerDTO) -> BlockerCopy {
        let cabal = blocker.groupName ?? "your cabal"
        switch blocker.kind {
        case .cabalSlice:
            return BlockerCopy(title: cabal, wayOut: "Cash out from \(cabal)")
        case .cashOutPending:
            return BlockerCopy(title: cabal, wayOut: "Cash out on its way. Wait for it to land.")
        case .transferPending:
            return BlockerCopy(title: "Money on its way", wayOut: "Wait for the transfer to finish")
        case .accountBalance:
            return BlockerCopy(title: "Account balance", wayOut: "Cash out your balance")
        case .unknown:
            return BlockerCopy(title: "Something is still in your account", wayOut: "Contact support to close it")
        }
    }

    /// Every fixed string above, for `MainFlowCopyAudit`.
    public static var auditedStrings: [String] {
        let fixed = [
            title, accountSection, securitySection, notificationsSection, appearanceSection, aboutSection,
            displayName, noDisplayName, deleteAccount, deleteAccountSubtitle,
            lockTitle(method: "Face ID"), lockSubtitleOn, lockSubtitleOff, lockUnavailable, lockAfter,
            unlockReason, turnOnReason, unlockButton, lockFailed,
            notificationsSaveFailed, version, terms, privacy, contactSupport, rate,
            deleteTitle, deleteIntro, deleteWhatGoes, deleteWhatStays, deleteMoneyFirst, deleteSameLogin, deleteNothingLeft,
            blockersSection, checkAgain, confirmSection, confirmPrompt, deleteButton, deleting,
            checkFailedTitle, checkFailedMessage, tryAgain, deleteFailed, deletedToast, deletedElsewhere,
            valueUnavailable, noMailApp,
        ]
        let notifications = NotificationCategory.allCases.flatMap { [notificationTitle($0), notificationSubtitle($0)] }
        let lock = AppLockTimeout.allCases.flatMap { [$0.title, $0.shortTitle] }
        let appearance = AppearanceChoice.allCases.map(\.title)
        let blockers = [
            DeletionBlockerDTO(kind: .cabalSlice, groupId: "g", groupName: "Sunday Investors", valueUsd: "1.00"),
            DeletionBlockerDTO(kind: .cashOutPending, groupId: "g", groupName: "Sunday Investors", valueUsd: "1.00"),
            DeletionBlockerDTO(kind: .transferPending, valueUsd: "1.00"),
            DeletionBlockerDTO(kind: .accountBalance, valueUsd: "1.00"),
            DeletionBlockerDTO(kind: .unknown("later"), valueUsd: nil),
        ].flatMap { blocker -> [String] in
            let copy = Self.blocker(blocker)
            return [copy.title, copy.wayOut]
        }
        return fixed + notifications + lock + appearance + blockers
    }
}
