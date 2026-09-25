import MonacoCore
import SwiftUI
import Testing
@testable import Monaco

/// "12 shares · $556.92 yours": the holding row's share count on the stock screen.
struct AssetHoldingShareLabelTests {
    @Test func theTokenAmountIsReadWhenTheBackendSendsOne() {
        #expect(AssetHoldingShareLabel.text(units: "12", tokenAmount: "1200000000") == "12 shares")
        #expect(AssetHoldingShareLabel.text(units: "3.5", tokenAmount: "350000000") == "3.5 shares")
    }

    /// The row printed `units` raw with "units" after it, so one share read "1 units".
    @Test func oneShareIsSingular() {
        #expect(AssetHoldingShareLabel.text(units: "1", tokenAmount: "100000000") == "1 share")
        #expect(AssetHoldingShareLabel.text(units: "1", tokenAmount: "0") == "1 share")
    }

    /// `tokenAmount` decodes to "0" when the key is missing, which is not "no shares".
    @Test func theWholeTokenFigureStandsInWhenThereIsNoTokenAmount() {
        #expect(AssetHoldingShareLabel.text(units: "12.5", tokenAmount: "0") == "12.5 shares")
        #expect(AssetHoldingShareLabel.text(units: "1250", tokenAmount: "") == "1,250 shares")
    }

    /// The same rules as the cabal screen's holdings, dust included.
    @Test func itFormatsLikeTheCabalScreen() {
        #expect(
            AssetHoldingShareLabel.text(units: "0", tokenAmount: "123456789")
                == ProposalShareFormatter.sharesLabel(fromAtomics: "123456789")
        )
        #expect(AssetHoldingShareLabel.text(units: "0.00001", tokenAmount: "0") == "< 0.0001 shares")
    }

    /// Nothing to count is said by saying nothing, not "0 shares" beside a value.
    @Test func noPositiveFigureDrawsNoCount() {
        #expect(AssetHoldingShareLabel.text(units: "0", tokenAmount: "0") == nil)
        #expect(AssetHoldingShareLabel.text(units: "", tokenAmount: "") == nil)
        #expect(AssetHoldingShareLabel.text(units: "twelve", tokenAmount: "lots") == nil)
        #expect(AssetHoldingShareLabel.text(units: "-2", tokenAmount: "-200000000") == nil)
    }

    @Test func itNeverSaysUnits() {
        let labels = [
            AssetHoldingShareLabel.text(units: "12", tokenAmount: "1200000000"),
            AssetHoldingShareLabel.text(units: "1", tokenAmount: "0"),
            AssetHoldingShareLabel.text(units: "3.5", tokenAmount: "0"),
        ].compactMap { $0 }
        #expect(labels.count == 3)
        #expect(MainFlowCopyAudit.stringsAreClean(labels))
    }
}

/// The line under the member's slice on the stock screen.
struct AssetPositionTotalsCopyTests {
    private func holding(_ name: String) -> AssetHoldingDTO {
        AssetHoldingDTO(groupId: name, name: name, units: "1", valueUsd: "10.00", costBasisUsd: "9.00", dollarPnl: "+1.00")
    }

    /// "Across your cabals" under one cabal read as a sentence written for someone else.
    @Test func oneHolderIsNamed() {
        #expect(
            AssetPositionTotalsCopy.sliceLine(totalValueUsd: "2784.60", holdings: [holding("Weekend investors")])
                == "Your slice of $2,784.60 held by Weekend investors"
        )
    }

    @Test func severalHoldersAreYourCabals() {
        #expect(
            AssetPositionTotalsCopy.sliceLine(totalValueUsd: "3828.83", holdings: [holding("A"), holding("B")])
                == "Your slice of $3,828.83 held across your cabals"
        )
    }

    /// A cabal with no name is not named as one.
    @Test func aBlankNameFallsBackToTheGeneralLine() {
        #expect(
            AssetPositionTotalsCopy.sliceLine(totalValueUsd: "10", holdings: [holding("  ")])
                == "Your slice of $10.00 held across your cabals"
        )
    }
}

/// Where the rules and the curve sit in the Stocks tab's rows and the stock screen's sections.
struct StockRowLayoutTests {
    /// 56pt with 4pt either side cut "2 cabals · your slice $294.70" to "your slice $29…".
    @Test func theRowSparklineIs48Wide() {
        #expect(Sparkline.rowWidth == 48)
    }

    /// The second line ends on the member's own slice; it wraps before it truncates.
    @Test func theSecondLineWrapsRatherThanCuttingOffTheSlice() {
        #expect(StockListRow.subtitleLineLimit == 2)
    }

    /// A holding row's rule starts under its text, as `MonacoRow`'s separator does: the
    /// section already insets its content by the page's 16pt, so the rule adds the mark
    /// and its gap and nothing else.
    @Test func aRuleAfterAMarkStartsWhereTheTextDoes() {
        let rowText = MonacoRowLayout(dynamicTypeSize: .large).separatorLeadingInset(markSize: 40)
        #expect(MonacoTheme.Space.m + AssetCardDivider.inset(afterMark: 40) == rowText)
    }
}

#if DEBUG
/// `-MonacoAssetDetailScroll` / `-MonacoStocksTabScroll`: where a harness opens its page.
struct SampleScrollAnchorTests {
    private let flag = "-MonacoAssetDetailScroll"

    @Test func withoutTheFlagTheScreenOpensAsTheAppDoes() {
        #expect(SampleScrollAnchor.requested(by: flag, in: ["Monaco", "-MonacoAssetDetailSample", "cabals"]) == nil)
        #expect(SampleScrollAnchor.requested(by: flag, in: ["Monaco", flag]) == nil)
    }

    /// Leading anchors, so the sideways rows on the page (range chips, movers) stay put.
    @Test func namedPlacesAreLeadingAnchors() {
        #expect(SampleScrollAnchor.requested(by: flag, in: [flag, "center"]) == .leading)
        #expect(SampleScrollAnchor.requested(by: flag, in: [flag, "bottom"]) == .bottomLeading)
    }

    @Test func aFractionIsThatFarDown() {
        #expect(SampleScrollAnchor.requested(by: flag, in: [flag, "0.25"]) == UnitPoint(x: 0, y: 0.25))
    }

    @Test func anythingElseIsIgnored() {
        #expect(SampleScrollAnchor.requested(by: flag, in: [flag, "top"]) == nil)
        #expect(SampleScrollAnchor.requested(by: flag, in: [flag, "1.5"]) == nil)
        #expect(SampleScrollAnchor.requested(by: flag, in: [flag, "-0.2"]) == nil)
    }
}
#endif
