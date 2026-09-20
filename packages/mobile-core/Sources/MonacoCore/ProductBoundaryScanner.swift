import Foundation

/// Forbidden external hosts for product flows — mobile talks to backend only.
public enum ProductBoundaryScanner {
    public static let forbiddenHostFragments = [
        "api.xstocks.fi",
        "jup.ag",
        "jupiter",
        "hermes.pyth.network",
        "pyth.network",
        "mainnet-beta",
        "basescan.org",
        "etherscan.io",
    ]

    public static func containsForbiddenHost(_ text: String) -> Bool {
        let lowered = text.lowercased()
        return forbiddenHostFragments.contains { lowered.contains($0) }
    }

    public static func containsForbiddenEVMAddress(_ text: String) -> Bool {
        let pattern = #"0x[0-9a-fA-F]{40}"#
        guard let regex = try? NSRegularExpression(pattern: pattern) else { return false }
        let range = NSRange(text.startIndex..<text.endIndex, in: text)
        return regex.firstMatch(in: text, range: range) != nil
    }

    public static func featureSourcesAreClean(_ sources: [String]) -> Bool {
        sources.allSatisfy { !containsForbiddenHost($0) }
    }

    public static func mainFlowCopyIsClean(_ text: String) -> Bool {
        !containsForbiddenHost(text) && !containsForbiddenEVMAddress(text)
    }
}
