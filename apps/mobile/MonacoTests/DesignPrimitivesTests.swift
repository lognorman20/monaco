import Foundation
import Testing
@testable import Monaco

struct CabalTintTests {
    @Test func fnv1a64MatchesPublishedVectors() {
        #expect(MonacoTheme.CabalTint.fnv1a64("") == 0xCBF2_9CE4_8422_2325)
        #expect(MonacoTheme.CabalTint.fnv1a64("a") == 0xAF63_DC4C_8601_EC8C)
        #expect(MonacoTheme.CabalTint.fnv1a64("foobar") == 0x8594_4171_F739_67E8)
    }

    @Test func tintIsStableForAGroupId() {
        let id = "3f5b2c9e-8d1a-4e7f-9b6c-2a1d0e4f7c88"
        let first = MonacoTheme.CabalTint.forGroupId(id)
        for _ in 0..<50 {
            #expect(MonacoTheme.CabalTint.forGroupId(id) == first)
        }
    }

    @Test func tintsSpreadAcrossAllFive() {
        let ids = (0..<200).map { "group-\($0)" }
        let used = Set(ids.map { MonacoTheme.CabalTint.forGroupId($0) })
        #expect(used.count == MonacoTheme.CabalTint.allCases.count)
    }
}

struct CabalMarkInitialsTests {
    @Test(arguments: [
        ("Weekend investors", "WI"),
        ("semis or bust", "SO"),
        ("Rent", "R"),
        ("🚀 Moon crew", "MC"),
        ("🚀🚀🚀", "🚀"),
        ("  ", ""),
        ("", ""),
        ("!!!", "!"),
        ("4B dorm fund", "4D"),
        ("Élan vital", "ÉV"),
    ])
    func initials(name: String, expected: String) {
        #expect(CabalMark.initials(for: name) == expected)
    }
}

struct AmountEntryTextTests {
    @Test(arguments: [
        ("50", "50"),
        ("050", "50"),
        ("0", "0"),
        (".", "0."),
        ("12.345", "12.34"),
        ("1.2.3", "1.23"),
        ("12,5", "12.5"),
        ("$1a2", "12"),
        ("0.05", "0.05"),
    ])
    func sanitize(raw: String, expected: String) {
        #expect(AmountEntryText.sanitize(raw) == expected)
    }

    @Test func displayGroupsIntegerPartAndKeepsTypedDecimals() {
        #expect(AmountEntryText.display("") == "$0")
        #expect(AmountEntryText.display("1250") == "$1,250")
        #expect(AmountEntryText.display("1250.") == "$1,250.")
        #expect(AmountEntryText.display("1250.5") == "$1,250.5")
    }

    @Test func plainAndRounding() {
        #expect(AmountEntryText.plain(Decimal(25)) == "25")
        #expect(AmountEntryText.plain(Decimal(string: "12.50")!) == "12.5")
        #expect(AmountEntryText.roundDownToCents(Decimal(string: "548.209")!) == Decimal(string: "548.2")!)
    }
}

struct PnLSpeechTests {
    @Test func dollarsReadWithDirection() {
        #expect(PnLSpeech.dollars("+48.20") == "up 48 dollars 20 cents")
        #expect(PnLSpeech.dollars("-7.60") == "down 7 dollars 60 cents")
        #expect(PnLSpeech.dollars("-0.001") == "no change")
        #expect(PnLSpeech.dollars("+1") == "up 1 dollar")
        #expect(PnLSpeech.dollars("0.01") == "up 1 cent")
    }

    @Test func badgeValue() {
        #expect(PnLSpeech.badge(dollarPnl: "+48.20", percentReturn: "0.096") == "up 48 dollars 20 cents, 9.6 percent")
        #expect(PnLSpeech.badge(dollarPnl: "-7.60", percentReturn: nil) == "down 7 dollars 60 cents")
    }

    @Test func toneTreatsDustAsFlat() {
        #expect(PnLTone(dollarPnl: "-0.00") == .flat)
        #expect(PnLTone(dollarPnl: "-0.001") == .flat)
        #expect(PnLTone(dollarPnl: "garbage") == .flat)
        #expect(PnLTone(dollarPnl: "-7.6") == .loss)
        #expect(PnLTone(dollarPnl: "\u{2212}7.6") == .loss)
        #expect(PnLTone(dollarPnl: "+48.2") == .profit)
    }
}
