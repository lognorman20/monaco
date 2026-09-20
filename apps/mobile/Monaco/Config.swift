import Foundation

enum Config {
    static let apiBaseURL = URL(string: "http://localhost:8080")!

    static let dynamic = DynamicAuthSettings.current
}

struct DynamicAuthSettings: Equatable {
    let environmentID: String
    let smsLoginEnabled: Bool
    let emailLoginEnabled: Bool

    var isConfigured: Bool {
        !environmentID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    static var current: DynamicAuthSettings {
        let environment = ProcessInfo.processInfo.environment
        return DynamicAuthSettings(
            environmentID: firstNonEmpty(
                trimmed(environment["DYNAMIC_ENVIRONMENT_ID"]),
                plistString("DYNAMIC_ENVIRONMENT_ID")
            ) ?? "",
            smsLoginEnabled: parseBool(
                firstNonEmpty(
                    trimmed(environment["AUTH_SMS_LOGIN_ENABLED"]),
                    plistString("AUTH_SMS_LOGIN_ENABLED")
                ),
                defaultValue: true
            ),
            emailLoginEnabled: parseBool(
                firstNonEmpty(
                    trimmed(environment["AUTH_EMAIL_LOGIN_ENABLED"]),
                    plistString("AUTH_EMAIL_LOGIN_ENABLED")
                ),
                defaultValue: true
            )
        )
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
