import Foundation
import XCTest
@testable import MonacoCore

/// The stats grid: which cells exist, and what a missing one does.
final class AssetStatsGridTests: XCTestCase {
    func testNoStats_makesNoGrid() {
        XCTAssertNil(AssetStatsGrid.make(nil))
        XCTAssertNil(AssetStatsGrid.make(AssetStatsDTO()), "an empty grid is no grid")
    }

    func testCompleteStats_layOutInTheOrderAMemberReadsThem() throws {
        let grid = try XCTUnwrap(AssetStatsGrid.make(MarketSampleData.statsComplete))
        XCTAssertEqual(
            grid.cells.map(\.id),
            ["open", "high", "low", "prev-close", "52w-high", "52w-low", "certainty"]
        )
        XCTAssertEqual(grid.cells.first?.value, "$229.00")
        XCTAssertEqual(grid.basisCaption, "AAPL on its home exchange")
    }

    /// A cell we could not source is dropped, never drawn as a dash and never filled
    /// with a placeholder number.
    func testPartialStats_dropTheCellsRatherThanShowingDashes() throws {
        let grid = try XCTUnwrap(AssetStatsGrid.make(MarketSampleData.statsPartial))
        XCTAssertEqual(grid.cells.map(\.id), ["open", "high", "low", "prev-close"])
        XCTAssertFalse(grid.cells.contains { $0.value.contains("—") })
        XCTAssertNil(grid.week52Position, "no year of history means no year bar")
    }

    /// The Kyber spread is the token's number and every cell in this grid is the
    /// equity's, per share. It belongs beside the two prices it is measured between,
    /// on the stock-vs-token card, and a cell here would read as the share's.
    func testGrid_neverCarriesTheTokensSpread() throws {
        let grid = try XCTUnwrap(AssetStatsGrid.make(MarketSampleData.statsComplete))
        XCTAssertFalse(grid.cells.contains { $0.id == "spread" })
        for cell in grid.cells {
            XCTAssertFalse(cell.label.lowercased().contains("spread"), cell.label)
            XCTAssertFalse(cell.label.lowercased().contains("premium"), cell.label)
        }
    }

    func testPriceCertainty_isTheOnlyCellWithABand() throws {
        let grid = try XCTUnwrap(AssetStatsGrid.make(MarketSampleData.statsComplete))
        let certainty = try XCTUnwrap(grid.cells.first { $0.id == "certainty" })
        XCTAssertEqual(certainty.value, "±$0.03")
        XCTAssertEqual(certainty.spoken, "Price certainty, give or take, ±$0.03")
    }

    func testWeek52Position_placesThePriceAndPinsAtTheEnds() {
        let stats = MarketSampleData.statsComplete
        // Low 163, high 262. 232.05 sits a bit above four fifths of the way up.
        let mid = AssetStatsGrid.week52Position(stats, currentUsdcMicros: 232_050_000)
        XCTAssertNotNil(mid)
        XCTAssertEqual(try XCTUnwrap(mid), 0.697, accuracy: 0.01)

        // A new high is a real thing: the marker pins to the end rather than running
        // off the track.
        XCTAssertEqual(AssetStatsGrid.week52Position(stats, currentUsdcMicros: 300_000_000), 1)
        XCTAssertEqual(AssetStatsGrid.week52Position(stats, currentUsdcMicros: 100_000_000), 0)
        XCTAssertNil(AssetStatsGrid.week52Position(stats, currentUsdcMicros: nil))
    }
}

/// The About section and its disclosure.
final class AssetAboutCopyTests: XCTestCase {
    private let token = "0xb200000000000000000000c2e324d24d7eecd1fb"

    func testDisclosureSaysWhatHoldingTheTokenIsNot() {
        let about = AssetAboutCopy.make(symbol: "AAPLc", name: "Apple", tokenAddress: token)
        XCTAssertTrue(about.disclosure.hasPrefix("AAPLc tracks Apple stock. It is not a share"), about.disclosure)
        XCTAssertTrue(about.disclosure.contains("no ownership"), about.disclosure)
        XCTAssertTrue(about.disclosure.contains("no vote"), about.disclosure)
    }

    /// A B20 token's cash dividend is reinvested into the stock behind it and shows
    /// up as a higher multiplier -- which the Chainlink total-return feed this whole
    /// screen is priced from carries. Saying "no dividend" would tell a member the
    /// money went nowhere, in the one sentence they are most likely to read.
    func testDisclosureNeverClaimsThereIsNoDividend() {
        let about = AssetAboutCopy.make(symbol: "AAPLc", name: "Apple", tokenAddress: token)
        for text in [about.disclosure, about.body] {
            XCTAssertFalse(text.lowercased().contains("no dividend"), text)
        }
        XCTAssertTrue(about.body.contains("reinvested into the stock behind the token"), about.body)
        XCTAssertTrue(about.body.contains("raises what one AAPLc is worth"), about.body)
    }

    /// Nothing in the section may place the token on the chain it was ported off.
    func testCopyNeverNamesSolanaOrJupiter() throws {
        let about = AssetAboutCopy.make(symbol: "AAPLc", name: "Apple", tokenAddress: token, liquidityLabel: "Routed on Base")
        var strings = [about.title, about.body, about.disclosure]
        strings += about.facts.map { "\($0.label) \($0.value)" }
        for text in strings {
            for word in ["solana", "jupiter", "xstock", "24/7"] {
                XCTAssertFalse(text.lowercased().contains(word), "\(word) in: \(text)")
            }
        }
        XCTAssertEqual(try XCTUnwrap(about.facts.first { $0.id == "chain" }).value, "Base")
    }

    /// The disclosure is never part of the clamped body: a "Show more" must not be
    /// able to hide it.
    func testDisclosureIsNotFoldedIntoTheBody() {
        let about = AssetAboutCopy.make(symbol: "AAPLc", name: "Apple", tokenAddress: token)
        XCTAssertFalse(about.body.contains("It is not a share"))
    }

    func testFacts_carryTheTokenAddressAndTheVenueWhenThereIsOne() throws {
        let about = AssetAboutCopy.make(
            symbol: "AAPLc",
            name: "Apple",
            tokenAddress: token,
            liquidityLabel: "Routed on Base"
        )
        XCTAssertEqual(about.title, "About AAPLc")
        XCTAssertEqual(about.facts.map(\.id), ["tracks", "chain", "token", "routing"])
        XCTAssertEqual(try XCTUnwrap(about.facts.first { $0.id == "tracks" }).value, "Apple (AAPL)")
        XCTAssertTrue(try XCTUnwrap(about.facts.first { $0.id == "token" }).isAddress)
    }

    func testNoTokenAddressAndNoVenue_dropTheirFactsRatherThanShowingBlanks() {
        let about = AssetAboutCopy.make(symbol: "AAPLc", name: "Apple", tokenAddress: "  ", liquidityLabel: "  ")
        XCTAssertEqual(about.facts.map(\.id), ["tracks", "chain"])
    }

    /// No catalog name is not a licence to invent one.
    func testUnknownCompany_fallsBackToTheTickerRatherThanGuessing() {
        let about = AssetAboutCopy.make(symbol: "ZZZZc", name: "", tokenAddress: "0xabc")
        XCTAssertTrue(about.disclosure.hasPrefix("ZZZZc tracks ZZZZ stock."), about.disclosure)
    }

    /// A catalog name that is only the ticker back again should still resolve to the
    /// company the app knows.
    func testCatalogNameEqualToTheTicker_resolvesThroughTheKnownNames() {
        let about = AssetAboutCopy.make(symbol: "AAPLc", name: "AAPL", tokenAddress: "0xabc")
        XCTAssertTrue(about.disclosure.hasPrefix("AAPLc tracks Apple stock."), about.disclosure)
    }
}

/// "Your cabals' position": totals, and the sentences around them.
final class AssetPositionSummaryTests: XCTestCase {
    func testNothingToShow_makesNoCard() {
        XCTAssertNil(AssetPositionSummary.make(nil, symbol: "AAPLc"))
        XCTAssertNil(
            AssetPositionSummary.make(AssetSocialSampleData.empty(), symbol: "AAPLc"),
            "an empty answer hides the card rather than drawing an empty frame"
        )
    }

    func testTotals_addUpInDecimalAcrossCabals() throws {
        let summary = try XCTUnwrap(AssetPositionSummary.make(AssetSocialSampleData.social(), symbol: "AAPLc"))

        // 2784.60 + 812.18 + 232.05
        XCTAssertEqual(summary.totalValueUsd, "3828.83")
        // 556.92 + 203.05 + 77.35
        XCTAssertEqual(summary.myTotalSliceUsd, "837.32")
        // 3828.83 − (2600.00 + 905.00 + 0.00)
        XCTAssertEqual(summary.totalDollarPnl, "+323.83")
        XCTAssertEqual(summary.headline, "3 cabals hold AAPLc")
    }

    /// The P&L is the cabals', and the slice above it is the member's. The card used
    /// to put them on one baseline, which reads as "$837.32, up $323.83" — a claim
    /// that the member made the whole gain, when their share of it is about a fifth.
    /// Nothing on the wire supports scaling it (`mySliceUsd` is a share of the
    /// *value*, with no basis behind it), so the figure carries a possessive instead.
    func testThePnlSaysWhoseItIs() throws {
        let summary = try XCTUnwrap(AssetPositionSummary.make(AssetSocialSampleData.social(), symbol: "AAPLc"))
        XCTAssertEqual(summary.totalPnlLabel, "Your cabals' return")
        XCTAssertNotEqual(
            summary.totalDollarPnl, summary.myTotalSliceUsd,
            "the two figures are about different money and the card must not pair them unlabelled"
        )
    }

    func testThePnlLabelCountsCabalsTheWayTheHeadlineDoes() {
        XCTAssertEqual(AssetPositionSummary.pnlLabel(holdingCount: 1), "Your cabal's return")
        XCTAssertEqual(AssetPositionSummary.pnlLabel(holdingCount: 3), "Your cabals' return")
    }

    /// A card that is only carrying an "unvalued" notice has no position to report a
    /// return on, so it prints none rather than a $0.00 nobody earned.
    func testNoHoldings_labelsNoReturnAtAll() throws {
        let unreachable = AssetSocialDTO(symbol: "AAPLc", unvaluedGroups: 1)
        let summary = try XCTUnwrap(AssetPositionSummary.make(unreachable, symbol: "AAPLc"))
        XCTAssertNil(summary.totalPnlLabel)
        XCTAssertNil(AssetPositionSummary.pnlLabel(holdingCount: 0))
    }

    /// A gain is signed so `MonacoTheme.signed` tints it; a loss keeps its own sign
    /// and must not gain a "+".
    func testLossKeepsItsSign() throws {
        let losing = AssetSocialDTO(
            symbol: "AAPLc",
            holdings: [AssetSocialSampleData.deskLunch],
            holderCount: 1
        )
        let summary = try XCTUnwrap(AssetPositionSummary.make(losing, symbol: "AAPLc"))
        XCTAssertEqual(summary.totalDollarPnl, "-92.82")
        XCTAssertEqual(try XCTUnwrap(summary.totalPercentReturn), "-0.102564")
    }

    /// A position with no cost basis behind it has a value but no return. A ratio
    /// here would be a 100% gain nobody earned.
    func testNoCostBasisAnywhere_hasNoPercentReturn() throws {
        let migrated = AssetSocialDTO(
            symbol: "AAPLc",
            holdings: [AssetSocialSampleData.migrated],
            holderCount: 1
        )
        let summary = try XCTUnwrap(AssetPositionSummary.make(migrated, symbol: "AAPLc"))
        XCTAssertNil(summary.totalPercentReturn)
        XCTAssertEqual(summary.totalDollarPnl, "+232.05")
    }

    /// The ratio is always fixed-point: `String(someDouble)` goes scientific below
    /// 1e-4, and "5e-05" is read downstream as a 505% gain.
    func testTinyReturn_staysFixedPointAndNeverGoesScientific() {
        let ratio = AssetPositionSummary.ratioString(Decimal(string: "0.00005")!)
        XCTAssertEqual(ratio, "0.00005")
        XCTAssertFalse(ratio.lowercased().contains("e"))
    }

    func testHeadlines_countCorrectlyAtOneAndAtMany() {
        XCTAssertEqual(AssetPositionSummary.headline(holdingCount: 0, ticker: "AAPLc"), "No cabal of yours holds AAPLc yet")
        XCTAssertEqual(AssetPositionSummary.headline(holdingCount: 1, ticker: "AAPLc"), "One cabal holds AAPLc")
        XCTAssertEqual(AssetPositionSummary.headline(holdingCount: 4, ticker: "AAPLc"), "4 cabals hold AAPLc")
    }

    func testVoteHeadlineAndTheErrandItImplies() throws {
        let summary = try XCTUnwrap(AssetPositionSummary.make(AssetSocialSampleData.social(), symbol: "AAPLc"))
        XCTAssertEqual(summary.voteHeadline, "2 open votes on AAPLc")
        // One of the two sample votes already carries the viewer's ballot.
        XCTAssertEqual(summary.waitingOnYou, "1 is waiting on your vote")
    }

    /// The card draws the yes votes as overlapped faces, which VoiceOver cannot read.
    /// Who is already behind a vote is half of what makes it worth looking at, so it
    /// is said in words instead of being lost with the avatars.
    func testTheVotersBehindAVoteAreNamedOutLoud() {
        let ada = AssetSocialSampleData.ada
        let bo = AssetSocialSampleData.bo
        XCTAssertNil(AssetPositionSummary.yesVoterSentence([]))
        XCTAssertEqual(AssetPositionSummary.yesVoterSentence([ada]), "Ada voted yes")
        XCTAssertEqual(AssetPositionSummary.yesVoterSentence([ada, bo]), "Ada and Bo voted yes")
    }

    /// Three names, then a count. A sentence naming nine people is not a sentence
    /// anyone listens to.
    func testALongBallotIsSummarisedRatherThanRecited() {
        let voters = ["Ada", "Bo", "Cy", "Di", "Eve"].map {
            AssetVoterDTO(userId: $0, displayName: $0, choice: .yes)
        }
        XCTAssertEqual(
            AssetPositionSummary.yesVoterSentence(voters),
            "Ada, Bo, Cy and 2 others voted yes"
        )
        XCTAssertEqual(
            AssetPositionSummary.yesVoterSentence(Array(voters.prefix(4))),
            "Ada, Bo, Cy and 1 other voted yes"
        )
    }

    /// A voter the backend sent with no name is not read out as a pause.
    func testANamelessVoterIsSkippedRatherThanSpoken() {
        let nameless = AssetVoterDTO(userId: "u-x", displayName: "  ", choice: .yes)
        XCTAssertEqual(
            AssetPositionSummary.yesVoterSentence([AssetSocialSampleData.ada, nameless]),
            "Ada voted yes"
        )
        XCTAssertNil(AssetPositionSummary.yesVoterSentence([nameless]))
    }

    func testEveryVoteAlreadyCast_asksForNothing() {
        let voted = AssetProposalDTO(
            id: "p", groupId: "g", groupName: "Weekend investors", kind: .buy, myVote: .yes
        )
        XCTAssertNil(AssetPositionSummary.waitingOnYou([voted]))
    }

    /// A cabal that could not be priced is said out loud. Silence would read as
    /// "no cabal holds this", which is a lie about someone's money.
    func testUnvaluedCabalsAreNeverSilent() throws {
        let summary = try XCTUnwrap(AssetPositionSummary.make(AssetSocialSampleData.partial(), symbol: "AAPLc"))
        XCTAssertEqual(summary.unvaluedNotice, "1 cabal could not be priced just now")
        XCTAssertEqual(AssetPositionSummary.unvaluedNotice(3), "3 cabals could not be priced just now")
        XCTAssertNil(AssetPositionSummary.unvaluedNotice(0))
    }

    /// Nothing held, nothing voted, but a cabal we could not reach: the card still
    /// draws, because "we could not check" is the thing it has to say.
    func testUnvaluedOnly_stillMakesACard() throws {
        let unreachable = AssetSocialDTO(symbol: "AAPLc", unvaluedGroups: 2)
        let summary = try XCTUnwrap(AssetPositionSummary.make(unreachable, symbol: "AAPLc"))
        XCTAssertEqual(summary.headline, "No cabal of yours holds AAPLc yet")
        XCTAssertEqual(summary.unvaluedNotice, "2 cabals could not be priced just now")
    }
}

/// "Activity on AAPLc".
final class AssetActivityCopyTests: XCTestCase {
    private let calendar = Calendar(identifier: .gregorian)

    private func lines(_ activity: [AssetActivityDTO]) -> [AssetActivityCopy.Line] {
        AssetActivityCopy.lines(activity, symbol: "AAPLc", now: AssetSocialSampleData.now, calendar: calendar)
    }

    func testTheCabalLeadsEveryLine() throws {
        let rendered = lines(AssetSocialSampleData.activity)
        XCTAssertEqual(rendered[0].title, "Weekend investors proposed buying $500.00 of AAPLc")
        XCTAssertEqual(rendered[1].title, "Desk lunch money bought $905.00 of AAPLc")
        XCTAssertEqual(rendered[2].title, "Weekend investors voted to buy $2,600.00 of AAPLc")
        XCTAssertEqual(rendered[3].title, "Desk lunch money voted down selling 1 AAPLc")
    }

    func testAge_countsInTheLadderTheAppAlreadyUses() {
        let now = AssetSocialSampleData.now
        XCTAssertEqual(AssetActivityCopy.age(now.addingTimeInterval(-30), now: now, calendar: calendar), "now")
        XCTAssertEqual(AssetActivityCopy.age(now.addingTimeInterval(-15 * 60), now: now, calendar: calendar), "15m")
        XCTAssertEqual(AssetActivityCopy.age(now.addingTimeInterval(-3 * 3600), now: now, calendar: calendar), "3h")
        XCTAssertEqual(AssetActivityCopy.age(now.addingTimeInterval(-9 * 86_400), now: now, calendar: calendar), "Sep 13")
    }

    func testRowWithNoTimestamp_readsWithoutATrailingComma() {
        let undated = AssetActivityDTO(id: "x", groupId: "g", groupName: "Weekend investors", kind: .proposed, action: .buy)
        let line = lines([undated])[0]
        XCTAssertEqual(line.age, "")
        XCTAssertEqual(line.spoken, line.title)
    }

    func testSellSize_readsInTokensAndBuySizeInDollars() {
        let sell = AssetActivityDTO(
            id: "s", groupId: "g", groupName: "Desk lunch money",
            kind: .filled, action: .sell, tokenAmount: 350_000_000
        )
        XCTAssertEqual(lines([sell])[0].title, "Desk lunch money sold 3.5 AAPLc")
    }

    func testSizelessRow_namesTheTickerRatherThanAnEmptyAmount() {
        let bare = AssetActivityDTO(id: "e", groupId: "g", groupName: "Weekend investors", kind: .expired, action: .buy)
        XCTAssertEqual(lines([bare])[0].title, "Weekend investors's vote on AAPLc expired")
    }

    func testUnnamedCabal_stillReadsAsASentence() {
        let anonymous = AssetActivityDTO(id: "a", groupId: "g", groupName: "", kind: .proposed, action: .buy, usdcMicros: 1_000_000)
        XCTAssertEqual(lines([anonymous])[0].title, "A cabal proposed buying $1.00 of AAPLc")
    }

    /// A rejected vote is not a loss: red is money, and a vote nobody passed did not
    /// cost anyone anything.
    func testTones_reserveRedForTheVoteThatFailed() {
        XCTAssertEqual(AssetActivityCopy.tone(.filled), .positive)
        XCTAssertEqual(AssetActivityCopy.tone(.passed), .positive)
        XCTAssertEqual(AssetActivityCopy.tone(.failed), .negative)
        XCTAssertEqual(AssetActivityCopy.tone(.proposed), .neutral)
        XCTAssertEqual(AssetActivityCopy.tone(.expired), .neutral)
    }

    func testSpokenLine_joinsTheAgeIntoTheSentence() {
        let line = lines([AssetSocialSampleData.activity[0]])[0]
        XCTAssertEqual(line.spoken, "Weekend investors proposed buying $500.00 of AAPLc, 4h ago")
    }
}

/// The sticky trade bar.
final class AssetTradeBarStateTests: XCTestCase {
    func testNoCabalHoldsIt_hidesSellRatherThanOfferingADeadEnd() {
        let bar = AssetTradeBarState.make(isRoutable: true, holdings: [], holdingsState: .answered)
        XCTAssertFalse(bar.showsSell)
        XCTAssertEqual(bar.sell, .hidden)
        XCTAssertTrue(bar.canBuy)
        XCTAssertEqual(bar.buyTitle, "Propose buy")
    }

    /// Before the holdings answer lands, Sell is hidden rather than shown disabled:
    /// a control that appears late is less jarring than one that turns live.
    func testBeforeTheAnswerLands_sellIsHidden() {
        let bar = AssetTradeBarState.make(
            isRoutable: true,
            holdings: [AssetSocialSampleData.weekendInvestors],
            holdingsState: .loading
        )
        XCTAssertFalse(bar.showsSell)
        XCTAssertEqual(bar.sell, .hidden)
    }

    /// The bug this case exists for: a failed read used to be indistinguishable from
    /// "no cabal of yours holds this", so a member with units in three cabals got a
    /// screen with no Sell button and no explanation.
    func testAFailedRead_saysSoInsteadOfClaimingNothingIsHeld() {
        let bar = AssetTradeBarState.make(isRoutable: true, holdings: [], holdingsState: .failed)
        XCTAssertFalse(bar.showsSell)
        XCTAssertEqual(bar.sell, .unknown(notice: "Couldn't check what your cabals hold"))
        XCTAssertNotEqual(bar.sell, .hidden, "silence here is the lie")
        XCTAssertTrue(bar.canBuy, "the buy path never depended on this read")
    }

    /// A re-read that failed does not take a position away: the holdings already in
    /// hand are still sellable.
    func testAFailedRereadKeepsSellOnTheHoldingsAlreadyInHand() {
        let bar = AssetTradeBarState.make(
            isRoutable: true,
            holdings: [AssetSocialSampleData.weekendInvestors],
            holdingsState: .failed
        )
        XCTAssertEqual(bar.sell, .available(caption: "Weekend investors"))
    }

    func testOneHolder_namesTheCabalUnderSell() {
        let bar = AssetTradeBarState.make(
            isRoutable: true,
            holdings: [AssetSocialSampleData.weekendInvestors],
            holdingsState: .answered
        )
        XCTAssertEqual(bar.sell, .available(caption: "Weekend investors"))
    }

    func testSeveralHolders_countThem() {
        let bar = AssetTradeBarState.make(
            isRoutable: true,
            holdings: [AssetSocialSampleData.weekendInvestors, AssetSocialSampleData.deskLunch],
            holdingsState: .answered
        )
        XCTAssertEqual(bar.sell, .available(caption: "2 cabals"))
    }

    /// An unroutable token says so inside the bar, where the button is, rather than
    /// as a toast after a tap that was never going to work.
    func testUnroutable_explainsItselfInTheBar() throws {
        let bar = AssetTradeBarState.make(isRoutable: false, holdings: [], holdingsState: .answered)
        XCTAssertFalse(bar.canBuy)
        XCTAssertTrue(try XCTUnwrap(bar.buyDisabledReason).hasPrefix("Can't be bought right now."))
        XCTAssertNil(bar.caption, "no promise about voting on a trade that cannot happen")
    }

    func testCaption_saysWhatTheButtonActuallyDoes() {
        let bar = AssetTradeBarState.make(isRoutable: true, holdings: [], holdingsState: .answered)
        XCTAssertEqual(bar.caption, "Your cabal votes before anything is bought")
    }
}

/// What the screen says when the social read did not come back.
final class AssetSocialFailureCopyTests: XCTestCase {
    /// One call behind three cards, so one sentence and one retry cover all of it.
    func testTheMessageCoversTheWholeReadAndNamesTheTicker() {
        XCTAssertEqual(
            AssetSocialFailureCopy.message(symbol: "AAPLc"),
            "Couldn't load what your cabals hold, or what they've done with AAPLc."
        )
        XCTAssertEqual(AssetSocialFailureCopy.cardTitle, "Your cabals' position")
        XCTAssertEqual(AssetSocialFailureCopy.retryTitle, "Retry")
    }

    /// Never the empty-state wording. "No cabal of yours holds this" is an answer,
    /// and a failed read does not have one.
    func testTheFailureNeverClaimsNothingIsHeld() {
        let said = [
            AssetSocialFailureCopy.message(symbol: "AAPLc"),
            AssetSocialFailureCopy.sellUnknown,
            AssetSocialFailureCopy.spoken(symbol: "AAPLc"),
        ]
        for sentence in said {
            XCTAssertFalse(
                sentence.lowercased().contains("no cabal"),
                "\(sentence) asserts an answer the read never gave"
            )
        }
    }

    func testSpokenIsOneSentenceForVoiceOver() {
        XCTAssertEqual(
            AssetSocialFailureCopy.spoken(symbol: "AAPLc"),
            "Your cabals' position. Couldn't load what your cabals hold, or what they've done with AAPLc."
        )
    }

    func testTheLoadStateKnowsAnAnswerFromAFailure() {
        XCTAssertTrue(AssetSocialLoadState.answered.isAnswered)
        XCTAssertFalse(AssetSocialLoadState.failed.isAnswered)
        XCTAssertFalse(AssetSocialLoadState.loading.isAnswered)
    }
}
