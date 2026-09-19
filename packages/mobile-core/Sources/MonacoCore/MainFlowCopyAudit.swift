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
        "treasury",
        "route",
        "quote",
        "bps",
        "units",
        "stake",
        "sweep",
        "http",
        "api key",
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
        "No cabals yet. Create or join one to start investing together.",
        "Home",
        "Profile",
        "Cabals",
        "Stocks",
        "Assets",
        "Settings",
        "Join a cabal to see members on the leaderboard.",
        "Search a stock",
        "Popular",
        "Load more",
        "Can't be bought right now",
        "Join a cabal first to propose a buy or sell.",
        "Pick a cabal",
        "This cabal does not hold this stock.",
        "Price history is not available yet.",
        "Your cabals",
        "Join a cabal to see it here.",
        "Join a cabal to see your positions here.",
        "P&L history shows up after you fund a cabal.",
        "You're caught up.",
        "Needs your vote",
        "No shared cabals yet.",
        "Loading cabals…",
        GroupChatCopy.title,
        GroupChatCopy.emptyState,
        GroupChatCopy.composerPlaceholder,
        GroupChatCopy.loadEarlier,
    ]
}
