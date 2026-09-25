import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// The rules on the Start a cabal screen: each choice carries a caption that says what it
/// means, in the words a member uses.
@MainActor
struct CabalRulesCopyTests {
    @Test func everyChoiceSaysWhatItMeansAndEachSaysSomethingDifferent() {
        let joinCaptions = JoinPolicyMode.allCases.map { $0.caption }
        let voterCaptions = VoterSetMode.allCases.map { $0.caption }
        let thresholdCaptions = VoteThresholdMode.allCases.map { $0.caption }
        let expiryCaptions = VoteExpiryOption.allCases.map { $0.caption }

        for captions in [joinCaptions, voterCaptions, thresholdCaptions, expiryCaptions] {
            #expect(captions.allSatisfy { !$0.isEmpty })
            #expect(Set(captions).count == captions.count, "two choices of one rule read the same: \(captions)")
        }
    }

    /// The join rule is who gets in, not how the invite travels: a link and a code lead to the
    /// same rule, so the rule's words never mention a link.
    @Test func joiningNeverPromisesALink() {
        for mode in JoinPolicyMode.allCases {
            #expect(!mode.label.lowercased().contains("link"))
            #expect(!mode.caption.lowercased().contains("link"))
        }
    }

    @Test func theVoteWindowCaptionNamesItsOwnWindow() {
        for option in VoteExpiryOption.allCases {
            #expect(option.caption.contains(option.label))
        }
    }

    /// The server still gets the values it validates; only the words changed.
    @Test func theRulesStillSendTheServersValues() {
        #expect(JoinPolicyMode.open.rawValue == "open")
        #expect(JoinPolicyMode.request.rawValue == "request")
        #expect(VoterSetMode.allMembers.rawValue == "all_members")
        #expect(VoterSetMode.namedSubset.rawValue == "named_subset")
        #expect(VoteThresholdMode.majority.rawValue == "majority")
        #expect(VoteThresholdMode.unanimous.rawValue == "unanimous")
        #expect(VoteExpiryOption.oneDay.rawValue == 86_400)
    }
}

/// The join screen's button has to match the cabal's policy: the approval UI test taps
/// "Ask to join" by its title, and an open cabal must not read as a request.
@MainActor
struct JoinCabalScreenCopyTests {
    @Test func theButtonMatchesTheCabalsPolicy() {
        #expect(JoinCabalScreenCopy.actionTitle(joinMode: .request, isJoining: false, requestPending: false) == "Ask to join")
        #expect(JoinCabalScreenCopy.actionTitle(joinMode: .open, isJoining: false, requestPending: false) == "Join")
        #expect(JoinCabalScreenCopy.actionTitle(joinMode: nil, isJoining: false, requestPending: false) == "Join cabal")
    }

    @Test func theButtonSaysWhatIsHappening() {
        #expect(JoinCabalScreenCopy.actionTitle(joinMode: .request, isJoining: true, requestPending: false) == "Sending…")
        #expect(JoinCabalScreenCopy.actionTitle(joinMode: .open, isJoining: true, requestPending: false) == "Joining…")
        #expect(JoinCabalScreenCopy.actionTitle(joinMode: .request, isJoining: false, requestPending: true) == "Request sent")
    }

    @Test func theMemberLineIsOnlyWhatTheRowKnew() {
        #expect(JoinCabalScreenCopy.memberLine(nil) == nil)
        #expect(JoinCabalScreenCopy.memberLine(0) == nil)
        #expect(JoinCabalScreenCopy.memberLine(1) == "1 member")
        #expect(JoinCabalScreenCopy.memberLine(9) == "9 members")
    }
}

/// The trading bot: what the server's status means to a member, and which key section the
/// bot's screen shows for it.
@MainActor
struct TradingBotTests {
    @Test func theServersStatusesReadAsWords() {
        #expect(TradingBotStatus("active").label == "Active")
        #expect(TradingBotStatus("paused").label == "Paused")
        #expect(TradingBotStatus("ACTIVE") == .active)
        // The member voted to remove it, so that is the word, not the server's "revoked".
        #expect(TradingBotStatus("revoked") == .removed)
        #expect(TradingBotStatus("revoked").label == "Removed")
    }

    @Test func anUnknownStatusIsShownAsSentAndNotAsRemoved() {
        let status = TradingBotStatus("draining")

        #expect(status == .other("draining"))
        #expect(status.label == "Draining")
        #expect(!status.isRemoved)
    }

    @Test func aRemovedBotNeverOffersAKey() {
        #expect(TradingBotKeyState.resolve(status: "revoked", apiKey: nil) == .removed)
        // Even if a key were sent, a removed bot's key no longer works.
        #expect(TradingBotKeyState.resolve(status: "revoked", apiKey: "monaco_ak_abc") == .removed)
    }

    @Test func aLiveBotOffersItsKeyOrSaysThereIsNone() {
        #expect(TradingBotKeyState.resolve(status: "active", apiKey: "monaco_ak_abc") == .key("monaco_ak_abc"))
        #expect(TradingBotKeyState.resolve(status: "paused", apiKey: " monaco_ak_abc\n") == .key("monaco_ak_abc"))
        #expect(TradingBotKeyState.resolve(status: "active", apiKey: nil) == .missing)
        #expect(TradingBotKeyState.resolve(status: "active", apiKey: "   ") == .missing)
    }

    @Test func theBudgetLineIsTheServersFigureOrNothing() {
        #expect(TradingBotCopy.budgetLine(allocationUsdcMicros: "100000000") == "$100.00 budget")
        #expect(TradingBotCopy.budgetLine(allocationUsdcMicros: "") == nil)
        #expect(TradingBotCopy.budgetLine(allocationUsdcMicros: "1e8") == nil)
    }
}

/// Every string these screens add stays out of the plumbing vocabulary.
@MainActor
struct AdminCopyAuditTests {
    @Test func theNewCopyPassesTheMainFlowAudit() {
        let joinCopy = [
            JoinCabalScreenCopy.title,
            JoinCabalScreenCopy.explanation(joinMode: .open),
            JoinCabalScreenCopy.explanation(joinMode: .request),
        ]

        #expect(MainFlowCopyAudit.stringsAreClean(CabalRulesCopy.auditedStrings))
        #expect(MainFlowCopyAudit.stringsAreClean(CabalDetailsCopy.auditedStrings))
        #expect(MainFlowCopyAudit.stringsAreClean(TradingBotCopy.auditedStrings))
        #expect(MainFlowCopyAudit.stringsAreClean(joinCopy))
    }
}
