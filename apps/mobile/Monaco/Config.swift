import Foundation

enum Config {
    static let apiBaseURL = URL(string: "http://localhost:8080")!

    /// Privy credentials and login flags for M1 auth (T9/T10).
    static let privy = PrivyAuthSettings.current
}

struct PrivyAuthSettings: Equatable {
    let appID: String
    let appClientID: String
    let smsLoginEnabled: Bool
    let emailLoginEnabled: Bool

    var isConfigured: Bool {
        !appID.isEmpty && !appClientID.isEmpty
    }

    static var current: PrivyAuthSettings {
        let environment = ProcessInfo.processInfo.environment
        return PrivyAuthSettings(
            appID: trimmed(environment["PRIVY_APP_ID"]),
            appClientID: trimmed(environment["PRIVY_APP_CLIENT_ID"]),
            smsLoginEnabled: parseBool(environment["PRIVY_SMS_LOGIN_ENABLED"], defaultValue: true),
            emailLoginEnabled: parseBool(environment["PRIVY_EMAIL_LOGIN_ENABLED"], defaultValue: true)
        )
    }

    private static func trimmed(_ value: String?) -> String {
        value?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    }

    private static func parseBool(_ value: String?, defaultValue: Bool) -> Bool {
        guard let value else { return defaultValue }
        switch value.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() {
        case "0", "false", "no", "off":
            return false
        case "1", "true", "yes", "on":
            return true
        default:
            return defaultValue
        }
    }
}
