import Foundation
import MonacoCore
import SwiftUI
import Testing
@testable import Monaco

/// The words and figures the propose screens work out for themselves.
@MainActor
struct ProposeScreenCopyTests {
    @Test func theSellRowNamesUpToThreeHoldingsThenCountsTheRest() {
        #expect(ProposeScreenCopy.sellRowDetail(names: ["Apple"]) == "Apple")
        #expect(ProposeScreenCopy.sellRowDetail(names: ["Apple", "Nvidia", "Tesla"]) == "Apple, Nvidia, Tesla")
        #expect(
            ProposeScreenCopy.sellRowDetail(names: ["Apple", "Nvidia", "Tesla", "Microsoft", "Amber"])
                == "Apple, Nvidia, Tesla and 2 more"
        )
    }

    @Test func aStockThatCannotBeBoughtKeepsItsNameAndSaysWhy() {
        #expect(ProposeScreenCopy.cantBuyCaption(name: "Amber") == "Amber · Can't buy right now")
    }

    /// To the tenth, whole numbers without the ".0", and never a "0.0%" for a real amount.
    @Test func thePotRowSaysHowMuchOfThePotABuySpends() {
        #expect(ProposeScreenCopy.potShare(amountMicros: 25_000_000, potMicros: 548_200_000) == "4.6% of $548.20")
        #expect(ProposeScreenCopy.potShare(amountMicros: 100_000_000, potMicros: 500_000_000) == "20% of $500.00")
        #expect(ProposeScreenCopy.potShare(amountMicros: 548_200_000, potMicros: 548_200_000) == "100% of $548.20")
        #expect(ProposeScreenCopy.potShare(amountMicros: 10_000, potMicros: 548_200_000) == "under 0.1% of $548.20")
    }

    /// Nearly all of the pot is not all of it.
    @Test func thePotRowDoesNotRoundABigBuyUpToTheWholePot() {
        #expect(ProposeScreenCopy.potShare(amountMicros: 546_000_000, potMicros: 548_200_000) == "99.6% of $548.20")
    }

    @Test func thePotRowIsLeftOutWithoutAPotToMeasureAgainst() {
        #expect(ProposeScreenCopy.potShare(amountMicros: 25_000_000, potMicros: 0) == nil)
        #expect(ProposeScreenCopy.potShare(amountMicros: 0, potMicros: 548_200_000) == nil)
    }

    @Test func aSellSaysWhatTheCabalKeeps() {
        #expect(ProposeScreenCopy.keeps(heldAtomics: 120_340_000, soldAtomics: 60_170_000) == "0.6017 shares")
        #expect(ProposeScreenCopy.keeps(heldAtomics: 120_340_000, soldAtomics: 120_340_000) == "None")
    }

    /// The headline is the exact thing the cabal votes on — dollars for a buy, shares for a sell.
    @Test func headlinesStateWhatTheCabalVotesOn() {
        #expect(ProposeScreenCopy.buyHeadline(amount: "$25.00", ticker: "AAPL") == "Buy $25.00 of AAPL")
        #expect(ProposeScreenCopy.sellHeadline(shares: "0.6017 shares", ticker: "AAPL") == "Sell 0.6017 shares of AAPL")
        #expect(ProposeScreenCopy.about("0.108 shares") == "about 0.108 shares")
    }

    /// The receipt heads the reason the way the proposal's screen will, so the member sees what
    /// the cabal will see.
    @Test func theReceiptHeadsTheReasonAsTheProposalDoes() {
        #expect(ProposeScreenCopy.reasonTitle(isSell: false) == ProposalFeedCopy.reasonTitle(for: ProposalDTO(id: "b", symbol: "AAPLx", status: "open")))
        #expect(ProposeScreenCopy.reasonTitle(isSell: true) == ProposalFeedCopy.reasonTitle(for: ProposalDTO(id: "s", symbol: "AAPLx", status: "open", kind: "sell")))
    }

    @Test func thePickerAsksWhichCabalForTheStock() {
        let buy = ProposeScreenCopy.pickerQuestion(kind: .buy)
        let sell = ProposeScreenCopy.pickerQuestion(kind: .sell)
        #expect(buy.lead + "AAPL" + buy.tail == "Which cabal should buy AAPL?")
        #expect(sell.lead + "AAPL" + sell.tail == "Which cabal should sell AAPL?")
    }

    @Test func everyStringPassesTheMainFlowCopyAudit() {
        #expect(MainFlowCopyAudit.stringsAreClean(ProposeScreenCopy.auditedStrings))
    }
}

@MainActor
struct ProposePresetsTests {
    @Test func aDollarChipEntersItsAmountAndPrintsIt() {
        #expect(ProposePresets.amount(for: .dollars(50), max: nil) == 50)
        #expect(ProposePresets.label(for: .dollars(50)) == "$50")
        #expect(ProposePresets.label(for: .dollars(1000)) == "$1,000")
    }

    /// "50%" of a $278.47 holding is $139.235, and a chip never asks for the half cent.
    @Test func aFractionChipRoundsDownToTheCent() {
        let holding = Decimal(string: "278.47")!
        #expect(ProposePresets.amount(for: .fraction(0.5, label: "50%"), max: holding) == Decimal(string: "139.23")!)
        #expect(ProposePresets.amount(for: .fraction(0.25, label: "25%"), max: holding) == Decimal(string: "69.61")!)
        #expect(ProposePresets.amount(for: .fraction(1, label: "All"), max: holding) == holding)
        #expect(ProposePresets.label(for: .fraction(1, label: "Max")) == "Max")
    }

    @Test func aFractionChipNeedsSomethingToBeAFractionOf() {
        #expect(ProposePresets.amount(for: .fraction(1, label: "Max"), max: nil) == nil)
        #expect(ProposePresets.amount(for: .fraction(1, label: "Max"), max: 0) == nil)
    }
}

@MainActor
struct ReceiptLayoutTests {
    /// Beside its label a figure reads from the right; stacked under it, from the left.
    @Test func aFigureAlignsWithWhereItSits() {
        #expect(ReceiptLayout.figureAlignment(.large) == .trailing)
        #expect(ReceiptLayout.figureAlignment(.xxxLarge) == .trailing)
        #expect(ReceiptLayout.figureAlignment(.accessibility1) == .leading)
        #expect(ReceiptLayout.figureAlignment(.accessibility5) == .leading)
    }
}

@MainActor
struct ProposeGlyphTests {
    /// A proposed buy and a buy that landed in the cabal's history draw the same glyph.
    @Test func buyAndSellShareTheActivityListsGlyphs() {
        #expect(ProposeGlyph.buy == GroupActivityRules.glyph(for: "buy"))
        #expect(ProposeGlyph.sell == GroupActivityRules.glyph(for: "sell"))
    }

    @Test func eachBotProposalHasItsOwnGlyph() {
        #expect(ProposeGlyph.lifecycle("pause_agent") == "pause")
        #expect(ProposeGlyph.lifecycle("resume_agent") == "play")
        #expect(ProposeGlyph.lifecycle("revoke_agent") == "xmark")
    }
}
