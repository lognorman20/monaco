import Foundation
import MonacoCore

enum Config {
    /// Environment + API base URL from the build configuration (Config/Monaco.xcconfig →
    /// Info.plist), validated in MonacoCore. Traps on a misconfigured build.
    static var api: MonacoAPIConfiguration { MonacoConfig.api }
    static var apiBaseURL: URL { api.baseURL }

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
            appID: resolvedAppID(from: environment),
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

    private static func resolvedAppID(from environment: [String: String]) -> String {
        let envAppID = trimmed(environment["PRIVY_APP_ID"])
        if !envAppID.isEmpty {
            return envAppID
        }
        return plistString("PRIVY_APP_ID")
    }

    private static func resolvedClientID(from environment: [String: String]) -> String {
        let envClientID = trimmed(environment["PRIVY_APP_CLIENT_ID"])
        if isValidPrivyIOSClientID(envClientID) {
            return envClientID
        }
        let envAuthID = trimmed(environment["PRIVY_AUTH_ID"])
        if isValidPrivyIOSClientID(envAuthID) {
            return envAuthID
        }
        let plistClientID = plistString("PRIVY_APP_CLIENT_ID")
        if isValidPrivyIOSClientID(plistClientID) {
            return plistClientID
        }
        return ""
    }

    private static func isValidPrivyIOSClientID(_ value: String) -> Bool {
        !value.isEmpty && value.hasPrefix("client-")
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
