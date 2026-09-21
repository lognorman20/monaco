import Foundation

/// Copy for the "Sign out of Monaco?" confirmation.
///
/// Getting back in takes a fresh one-time code, and which kind depends on the login
/// channels this build enables (`AUTH_SMS_LOGIN_ENABLED` / `AUTH_EMAIL_LOGIN_ENABLED`).
/// The member may have signed in either way, and a restored session doesn't record which,
/// so the message names every channel that can bring them back rather than guessing one.
enum ProfileSignOutCopy {
    static let title = "Sign out of Monaco?"

    static func message(smsLoginEnabled: Bool, emailLoginEnabled: Bool) -> String {
        let prefix = "Your money stays where it is."
        switch (smsLoginEnabled, emailLoginEnabled) {
        case (true, true):
            return "\(prefix) You'll need a new code, by text or email, to sign back in."
        case (true, false):
            return "\(prefix) You'll need a new code by text to sign back in."
        case (false, true):
            return "\(prefix) You'll need a new code by email to sign back in."
        case (false, false):
            return "\(prefix) You'll need a new sign-in code to get back in."
        }
    }
}
