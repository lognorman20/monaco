import Foundation

/// Host-testable Privy client configuration for Monaco mobile auth (M1-T9/T10).
public struct PrivyAuthConfig: Equatable, Sendable {
    public let appID: String
    public let appClientID: String
    public let smsLoginEnabled: Bool
    /// Email login uses Privy OTP ("one-time password"), not traditional password fields (M1-T11).
    public let emailLoginEnabled: Bool

    public init(
        appID: String,
        appClientID: String,
        smsLoginEnabled: Bool = true,
        emailLoginEnabled: Bool = true
    ) {
        self.appID = appID
        self.appClientID = appClientID
        self.smsLoginEnabled = smsLoginEnabled
        self.emailLoginEnabled = emailLoginEnabled
    }

    public var isConfigured: Bool {
        !appID.isEmpty && !appClientID.isEmpty
    }

    /// Parses Privy settings from environment-style key/value pairs (e.g. dotenv or Xcode scheme).
    public static func fromEnvironment(_ environment: [String: String]) -> PrivyAuthConfig {
        let appID = environment["PRIVY_APP_ID"]?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let appClientID = resolvedClientID(from: environment)
        let smsEnabled = parseBool(environment["PRIVY_SMS_LOGIN_ENABLED"], defaultValue: true)
        let emailEnabled = parseBool(environment["PRIVY_EMAIL_LOGIN_ENABLED"], defaultValue: true)

        return PrivyAuthConfig(
            appID: appID,
            appClientID: appClientID,
            smsLoginEnabled: smsEnabled,
            emailLoginEnabled: emailEnabled
        )
    }

    private static func resolvedClientID(from environment: [String: String]) -> String {
        let clientID = environment["PRIVY_APP_CLIENT_ID"]?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if isValidPrivyIOSClientID(clientID) {
            return clientID
        }
        let authID = environment["PRIVY_AUTH_ID"]?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if isValidPrivyIOSClientID(authID) {
            return authID
        }
        return ""
    }

    private static func isValidPrivyIOSClientID(_ value: String) -> Bool {
        !value.isEmpty && value.hasPrefix("client-")
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
