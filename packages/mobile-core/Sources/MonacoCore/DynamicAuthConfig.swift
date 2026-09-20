import Foundation

/// Host-testable Dynamic client configuration for Monaco mobile auth.
public struct DynamicAuthConfig: Equatable, Sendable {
    public let environmentID: String
    public let smsLoginEnabled: Bool
    public let emailLoginEnabled: Bool

    public init(
        environmentID: String,
        smsLoginEnabled: Bool = true,
        emailLoginEnabled: Bool = true
    ) {
        self.environmentID = environmentID
        self.smsLoginEnabled = smsLoginEnabled
        self.emailLoginEnabled = emailLoginEnabled
    }

    public var isConfigured: Bool {
        !environmentID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    public static func fromEnvironment(_ environment: [String: String]) throws -> DynamicAuthConfig {
        let environmentID = environment["DYNAMIC_ENVIRONMENT_ID"]?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if environmentID.isEmpty {
            throw DynamicAuthConfigError.missingEnvironmentID
        }
        return DynamicAuthConfig(
            environmentID: environmentID,
            smsLoginEnabled: parseBool(environment["AUTH_SMS_LOGIN_ENABLED"], defaultValue: true),
            emailLoginEnabled: parseBool(environment["AUTH_EMAIL_LOGIN_ENABLED"], defaultValue: true)
        )
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

public enum DynamicAuthConfigError: Error, Equatable {
    case missingEnvironmentID
}
