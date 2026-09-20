import Foundation

public enum MonacoConfig {
    /// The build's API endpoint, resolved once from Info.plist (xcconfig) plus, in Debug,
    /// the process environment. A misconfigured build traps here instead of sending
    /// requests to the wrong place; the app touches this at launch.
    public static let api: MonacoAPIConfiguration = {
        do {
            return try MonacoAPIConfiguration.resolve(
                infoDictionary: Bundle.main.infoDictionary ?? [:],
                processEnvironment: ProcessInfo.processInfo.environment,
                build: .current
            )
        } catch {
            fatalError("Monaco API configuration is invalid: \(error.localizedDescription)")
        }
    }()

    public static var apiBaseURL: URL { api.baseURL }
}
