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
            appID: value(for: "PRIVY_APP_ID", environment: environment),
            appClientID: resolvedClientID(from: environment),
            smsLoginEnabled: parseBool(
                firstNonEmpty(
                    trimmed(environment["PRIVY_SMS_LOGIN_ENABLED"]),
                    plistString("PRIVY_SMS_LOGIN_ENABLED")
                ),
                defaultValue: true
            ),
            emailLoginEnabled: parseBool(
                firstNonEmpty(
                    trimmed(environment["PRIVY_EMAIL_LOGIN_ENABLED"]),
                    plistString("PRIVY_EMAIL_LOGIN_ENABLED")
                ),
                defaultValue: true
            )
        )
    }

    private static func resolvedClientID(from environment: [String: String]) -> String {
        let clientID = value(for: "PRIVY_APP_CLIENT_ID", environment: environment)
        if !clientID.isEmpty {
            return clientID
        }
        return value(for: "PRIVY_AUTH_ID", environment: environment)
    }

    /// Process env (simctl / Xcode scheme) wins; Info.plist from xcconfig is fallback.
    private static func value(for key: String, environment: [String: String]) -> String {
        let fromEnvironment = trimmed(environment[key])
        if !fromEnvironment.isEmpty {
            return fromEnvironment
        }
        return plistString(key)
    }

    private static func plistString(_ key: String) -> String {
        trimmed(Bundle.main.object(forInfoDictionaryKey: key) as? String)
    }

    private static func firstNonEmpty(_ first: String, _ second: String) -> String? {
        if !first.isEmpty { return first }
        if !second.isEmpty { return second }
        return nil
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
