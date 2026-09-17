import Foundation

/// Forbidden external hosts for product flows — mobile talks to backend only.
public enum ProductBoundaryScanner {
    public static let forbiddenHostFragments = [
        "api.xstocks.fi",
        "jup.ag",
        "jupiter",
        "hermes.pyth.network",
        "pyth.network",
        "mainnet-beta.solana.com",
        "solana-mainnet",
    ]

    public static func containsForbiddenHost(_ text: String) -> Bool {
        let lowered = text.lowercased()
        return forbiddenHostFragments.contains { lowered.contains($0) }
    }

    public static func featureSourcesAreClean(_ sources: [String]) -> Bool {
        sources.allSatisfy { !containsForbiddenHost($0) }
    }
}
