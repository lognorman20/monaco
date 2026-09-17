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
        "Add money to grow your club's pot.",
        "Create club",
        "Join club",
        "Propose buy",
        "Vote yes",
        "Vote no",
        "Cash out",
        "Your slice",
        "Member board",
        "Sweep in progress",
        "No clubs yet. Create or join one to start investing together.",
    ]
}
