import Foundation

public enum MainFlowCopyAudit {
    public static let forbiddenTerms = [
        "wallet",
        "gas",
        "seed phrase",
        "seedphrase",
        " mint",
        "mint ",
        "nav",
        "xstock",
        "club",
        " group",
        "group ",
    ]

    public static func stringsAreClean(_ strings: [String]) -> Bool {
        strings.allSatisfy { string in
            let lowered = string.lowercased()
            return !forbiddenTerms.contains { term in lowered.contains(term) }
        }
    }
}

public enum MainFlowCopyManifest {
    public static let mainFlowStrings: [String] = [
        "Fund this cabal to grow your cabal's pot.",
        "Create cabal",
        "Join cabal",
        "Propose",
        "Propose sell",
        "Vote yes",
        "Vote no",
        "Cash out",
        "Your slice",
        "Member board",
        "Sweep in progress",
        "No cabals yet. Create or join one to start investing together.",
    ]
}
