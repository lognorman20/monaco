import Foundation

/// Named backend the app build talks to. Selected by the `MONACO_ENVIRONMENT` build
/// setting (apps/mobile/Config/Monaco.xcconfig) and carried into Info.plist.
public enum MonacoEnvironment: String, Equatable, Sendable, CaseIterable {
    case local
    case staging
    case production
}

public enum MonacoBuildKind: Equatable, Sendable {
    case debug
    case release

    public static var current: MonacoBuildKind {
        #if DEBUG
        return .debug
        #else
        return .release
        #endif
    }
}

public enum MonacoAPIConfigurationError: Error, Equatable, LocalizedError {
    case missingEnvironment
    case unknownEnvironment(String)
    case missingBaseURL
    case malformedBaseURL(String)
    case insecureScheme(String)
    case localHost(String)
    case localEnvironmentInRelease

    public var errorDescription: String? {
        switch self {
        case .missingEnvironment:
            return "MONACO_ENVIRONMENT is missing from Info.plist. Regenerate apps/mobile/Config with scripts/ensure-ios-privy-config.sh."
        case .unknownEnvironment(let raw):
            return "MONACO_ENVIRONMENT \"\(raw)\" is not one of local, staging, production."
        case .missingBaseURL:
            return "MONACO_API_BASE_URL is empty for the selected environment. Set MONACO_STAGING_API_BASE_URL / MONACO_PRODUCTION_API_BASE_URL (see apps/mobile/TestFlight.md)."
        case .malformedBaseURL(let raw):
            return "MONACO_API_BASE_URL \"\(raw)\" is not an absolute http(s) URL with a host and no credentials, query, or fragment."
        case .insecureScheme(let raw):
            return "MONACO_API_BASE_URL \"\(raw)\" must use https in Release builds."
        case .localHost(let raw):
            return "MONACO_API_BASE_URL \"\(raw)\" points at a local host, which a Release build can never reach."
        case .localEnvironmentInRelease:
            return "MONACO_ENVIRONMENT local is Debug-only. Release builds must select staging or production."
        }
    }
}

/// Validated API endpoint for this build. `resolve` is the only place the base URL is
/// read; both API clients take it from `MonacoConfig.api`.
public struct MonacoAPIConfiguration: Equatable, Sendable {
    public enum Source: String, Equatable, Sendable {
        case infoPlist = "Info.plist"
        case processEnvironment = "process environment"
        case builtInLocalDefault = "built-in local default"
    }

    /// Info.plist key and process environment variable (same name, like the Privy keys).
    public static let baseURLKey = "MONACO_API_BASE_URL"
    public static let environmentKey = "MONACO_ENVIRONMENT"

    static let localDefaultBaseURL = URL(string: "http://localhost:8080")!

    public let environment: MonacoEnvironment
    public let baseURL: URL
    public let source: Source

    /// Shown in DEBUG-only diagnostics so a dev can see which backend the build targets.
    public var debugSummary: String {
        "env \(environment.rawValue) — \(baseURL.absoluteString) (\(source.rawValue))"
    }

    /// Debug: process environment (simctl `SIMCTL_CHILD_*` / Xcode scheme) wins, then
    /// Info.plist, then localhost when the bundle carries no config at all (host `swift test`).
    /// Release: Info.plist only, https only, never a local host or the local environment.
    public static func resolve(
        infoDictionary: [String: Any],
        processEnvironment: [String: String],
        build: MonacoBuildKind
    ) throws -> MonacoAPIConfiguration {
        let plistEnvironment = trimmed(infoDictionary[environmentKey] as? String)
        let plistBaseURL = trimmed(infoDictionary[baseURLKey] as? String)

        var rawEnvironment = plistEnvironment
        var rawBaseURL = plistBaseURL
        var source = Source.infoPlist

        if build == .debug {
            let overrideEnvironment = trimmed(processEnvironment[environmentKey])
            if !overrideEnvironment.isEmpty {
                rawEnvironment = overrideEnvironment
            }
            let overrideBaseURL = trimmed(processEnvironment[baseURLKey])
            if !overrideBaseURL.isEmpty {
                rawBaseURL = overrideBaseURL
                source = .processEnvironment
            }
            if rawEnvironment.isEmpty && rawBaseURL.isEmpty {
                return MonacoAPIConfiguration(
                    environment: .local,
                    baseURL: localDefaultBaseURL,
                    source: .builtInLocalDefault
                )
            }
            if rawEnvironment.isEmpty {
                rawEnvironment = MonacoEnvironment.local.rawValue
            }
        }

        guard !rawEnvironment.isEmpty else {
            throw MonacoAPIConfigurationError.missingEnvironment
        }
        guard let environment = MonacoEnvironment(rawValue: rawEnvironment.lowercased()) else {
            throw MonacoAPIConfigurationError.unknownEnvironment(rawEnvironment)
        }
        if build == .release && environment == .local {
            throw MonacoAPIConfigurationError.localEnvironmentInRelease
        }

        let baseURL = try validatedBaseURL(rawBaseURL, build: build)
        return MonacoAPIConfiguration(environment: environment, baseURL: baseURL, source: source)
    }

    static func validatedBaseURL(_ raw: String, build: MonacoBuildKind) throws -> URL {
        guard !raw.isEmpty else {
            throw MonacoAPIConfigurationError.missingBaseURL
        }
        guard
            let components = URLComponents(string: raw),
            let scheme = components.scheme?.lowercased(),
            scheme == "http" || scheme == "https",
            let host = components.host, !host.isEmpty,
            components.user == nil, components.password == nil,
            components.query == nil, components.fragment == nil,
            let url = components.url
        else {
            throw MonacoAPIConfigurationError.malformedBaseURL(raw)
        }

        if build == .release {
            guard scheme == "https" else {
                throw MonacoAPIConfigurationError.insecureScheme(raw)
            }
            guard !isLocalHost(host) else {
                throw MonacoAPIConfigurationError.localHost(raw)
            }
        }
        return url
    }

    private static func isLocalHost(_ host: String) -> Bool {
        let normalized = host
            .trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
            .lowercased()
        if ["localhost", "::1", "0.0.0.0", "::"].contains(normalized) {
            return true
        }
        return normalized.hasPrefix("127.")
            || normalized.hasSuffix(".localhost")
            || normalized.hasSuffix(".local")
    }

    private static func trimmed(_ value: String?) -> String {
        value?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    }
}
