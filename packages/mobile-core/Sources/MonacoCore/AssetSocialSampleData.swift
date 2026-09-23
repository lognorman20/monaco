import Foundation

/// Canned `GET /v1/assets/{symbol}/social` payloads for the sample harness and for
/// previews.
///
/// The awkward states are the point, the same way they are in `MarketSampleData`: a
/// cabal that is under water, a position with no cost basis behind it, a vote nobody
/// has cast a ballot on yet, and a pass where one cabal could not be priced at all.
/// Every one of those is something the backend really produces.
public enum AssetSocialSampleData {
    /// Anchored to `MarketSampleData.tradingTuesday` so a screenshot of the activity
    /// list shows the same ages on every run.
    public static var now: Date { MarketSampleData.tradingTuesday }

    // MARK: - Voters

    public static let ada = AssetVoterDTO(userId: "u-ada", displayName: "Ada", choice: .yes)
    public static let bo = AssetVoterDTO(userId: "u-bo", displayName: "Bo", choice: .yes)
    public static let cy = AssetVoterDTO(userId: "u-cy", displayName: "Cy", choice: .no)

    // MARK: - Holdings

    /// A cabal in profit, with the viewer holding roughly a fifth of it.
    public static let weekendInvestors = AssetHoldingDTO(
        groupId: "g-weekend",
        name: "Weekend investors",
        units: "12",
        tokenAmount: "1200000000",
        markUsd: "232.05",
        valueUsd: "2784.60",
        costBasisUsd: "2600.00",
        dollarPnl: "+184.60",
        percentReturn: "0.071",
        mySliceUsd: "556.92",
        mySlicePercent: "0.2"
    )

    /// A cabal under water on the same stock. Two cabals disagreeing about the same
    /// position is the card's most interesting state and its easiest bug.
    public static let deskLunch = AssetHoldingDTO(
        groupId: "g-desk",
        name: "Desk lunch money",
        units: "3.5",
        tokenAmount: "350000000",
        markUsd: "232.05",
        valueUsd: "812.18",
        costBasisUsd: "905.00",
        dollarPnl: "-92.82",
        percentReturn: "-0.1026",
        mySliceUsd: "203.05",
        mySlicePercent: "0.25"
    )

    /// Units with no cost basis behind them: a value, and deliberately no percentage.
    public static let migrated = AssetHoldingDTO(
        groupId: "g-migrated",
        name: "The old group chat",
        units: "1",
        tokenAmount: "100000000",
        markUsd: "232.05",
        valueUsd: "232.05",
        costBasisUsd: "0.00",
        dollarPnl: "+232.05",
        percentReturn: nil,
        mySliceUsd: "77.35",
        mySlicePercent: "0.3333"
    )

    // MARK: - Proposals

    /// A buy nobody has voted on yet from the viewer's side — the "waiting on you" case.
    public static let openBuy = AssetProposalDTO(
        id: "p-buy",
        groupId: "g-weekend",
        groupName: "Weekend investors",
        kind: .buy,
        usdcMicros: 500_000_000,
        thesis: "Earnings on Thursday",
        yes: 2,
        no: 1,
        memberCount: 5,
        myVote: nil,
        voters: [ada, bo, cy],
        expiresAt: now.addingTimeInterval(20 * 3600),
        createdAt: now.addingTimeInterval(-4 * 3600)
    )

    /// A sell the viewer has already voted on.
    public static let openSell = AssetProposalDTO(
        id: "p-sell",
        groupId: "g-desk",
        groupName: "Desk lunch money",
        kind: .sell,
        tokenAmount: 150_000_000,
        yes: 1,
        no: 0,
        memberCount: 4,
        myVote: .yes,
        voters: [ada],
        expiresAt: now.addingTimeInterval(9 * 3600),
        createdAt: now.addingTimeInterval(-40 * 60)
    )

    // MARK: - Activity

    public static let activity: [AssetActivityDTO] = [
        AssetActivityDTO(
            id: "a-1", groupId: "g-weekend", groupName: "Weekend investors",
            kind: .proposed, action: .buy, usdcMicros: 500_000_000,
            createdAt: now.addingTimeInterval(-4 * 3600)
        ),
        AssetActivityDTO(
            id: "a-2", groupId: "g-desk", groupName: "Desk lunch money",
            kind: .filled, action: .buy, usdcMicros: 905_000_000, tokenAmount: 350_000_000,
            actorName: "Ada", txHash: "0x5a1e5a1e5a1e5a1e5a1e5a1e5a1e5a1e5a1e5a1e5a1e5a1e5a1e5a1e5a1e5a1e",
            createdAt: now.addingTimeInterval(-2 * 86_400)
        ),
        AssetActivityDTO(
            id: "a-3", groupId: "g-weekend", groupName: "Weekend investors",
            kind: .passed, action: .buy, usdcMicros: 2_600_000_000,
            createdAt: now.addingTimeInterval(-9 * 86_400)
        ),
        AssetActivityDTO(
            id: "a-4", groupId: "g-desk", groupName: "Desk lunch money",
            kind: .failed, action: .sell, tokenAmount: 100_000_000,
            createdAt: now.addingTimeInterval(-15 * 86_400)
        ),
    ]

    // MARK: - Payloads

    /// The full card: three cabals, two open votes, a history behind it.
    public static func social(symbol: String = "AAPLc") -> AssetSocialDTO {
        AssetSocialDTO(
            symbol: symbol,
            holdings: [weekendInvestors, deskLunch, migrated],
            openProposals: [openBuy, openSell],
            activity: activity,
            holderCount: 3
        )
    }

    /// One cabal, one vote — the common case, and the one the card has to look best in.
    public static func modest(symbol: String = "AAPLc") -> AssetSocialDTO {
        AssetSocialDTO(
            symbol: symbol,
            holdings: [weekendInvestors],
            openProposals: [openBuy],
            activity: Array(activity.prefix(2)),
            holderCount: 1
        )
    }

    /// Nobody holds it and nobody is voting: the card must not draw at all, and the
    /// trade bar must not offer a sell.
    public static func empty(symbol: String = "AAPLc") -> AssetSocialDTO {
        AssetSocialDTO(symbol: symbol, activity: [])
    }

    /// A pass where one cabal could not be priced. The card shows what it has and
    /// says what it could not check.
    public static func partial(symbol: String = "AAPLc") -> AssetSocialDTO {
        AssetSocialDTO(
            symbol: symbol,
            holdings: [weekendInvestors],
            openProposals: [],
            activity: Array(activity.prefix(1)),
            holderCount: 1,
            unvaluedGroups: 1
        )
    }
}
