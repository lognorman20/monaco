import Foundation

/// Bundled snippets mirroring apps/mobile Features/ — boundary scan runs in host tests.
public enum ProductFeatureSourceManifest {
    public static let sampleFeatureSources: [String] = [
        """
        MonacoAPIClient baseURL Config.apiBaseURL
        GET v1/groups assets query
        POST v1/groups quotes proposals
        POST v1/proposals votes
        GET v1/groups proposals tab
        GET v1/proposals comments
        POST v1/proposals comments parentId
        """,
    ]
}
