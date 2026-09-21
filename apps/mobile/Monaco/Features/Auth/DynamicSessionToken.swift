import Foundation

/// Dynamic issues an access token (`minAuthToken`) and an ID token (`token`).
/// The backend verifies the access token only.
enum DynamicSessionToken {
    static func preferred(minAuthToken: String?, idToken: String?) -> String? {
        for candidate in [minAuthToken, idToken] {
            let trimmed = candidate?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            if !trimmed.isEmpty {
                return trimmed
            }
        }
        return nil
    }
}
