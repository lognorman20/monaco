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

    @Test func tintIgnoresCaseAndWhitespaceOfTheId() {
        let id = "3f5b2c9e-8d1a-4e7f-9b6c-2a1d0e4f7c88"
        let tint = MonacoTheme.CabalTint.forGroupId(id)
        #expect(MonacoTheme.CabalTint.forGroupId(id.uppercased()) == tint)
        #expect(MonacoTheme.CabalTint.forGroupId(" \(id)\n") == tint)
        #expect(MonacoTheme.CabalTint.forGroupId(UUID(uuidString: id)!.uuidString) == tint)
    }

    @Test func tintIsPinnedForKnownIds() {
        // Pinned so a change to the hash or the case list is a deliberate, visible decision.
        #expect(MonacoTheme.CabalTint.forGroupId("") == .butter)
        #expect(MonacoTheme.CabalTint.forGroupId("a") == .peach)
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
        ("Semis or bust", "SB"),
        ("semis or bust", "SB"),
        ("Index huggers", "IH"),
        ("Dorm 4B fund", "DF"),
        ("The Rent Money Club", "RC"),
        ("Bulls & bears", "BB"),
        ("Rent", "R"),
        ("The", "T"),
        ("🚀 Moon crew", "MC"),
        ("🚀🚀🚀", "🚀"),
        ("  ", ""),
        ("", ""),
        ("!!!", "!"),
        ("4B dorm fund", "4F"),
        ("Élan vital", "ÉV"),
        ("The extremely long cabal name for testing", "ET"),
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

    /// The figure over the pad renders "$1,250.50", so that is what comes back on a paste.
    /// Reading every comma as a decimal point turned it into $1.25.
    @Test(arguments: [
        ("1,250.50", "1250.50"),
        ("$1,250.50", "1250.50"),
        ("1,250", "1250"),
        ("1,250,000", "1250000"),
        // Both separators present: the one that comes last is the decimal point.
        ("1.250,50", "1250.50"),
    ])
    func pastedGroupingKeepsItsValue(raw: String, expected: String) {
        #expect(AmountEntryText.sanitize(raw) == expected)
    }

    /// A decimal pad in a comma-decimal locale types a comma. One comma that cannot be grouping
    /// still reads as a decimal point.
    @Test(arguments: [("12,", "12."), ("12,5", "12.5"), ("12,50", "12.50")])
    func typedCommaIsStillADecimalPoint(raw: String, expected: String) {
        #expect(AmountEntryText.sanitize(raw) == expected)
    }

    /// Commas that are not in the shape of grouping separators are decimal-point attempts — a
    /// double tap on a comma-decimal pad — and the extras are dropped exactly as extra dots are.
    /// Reading them as grouping instead made "1,250,5" into 12505, a hundredfold error.
    @Test(arguments: [("1,2,5", "1.25"), ("1,250,5", "1.25"), ("12,,5", "12.5"), ("1234,567", "1234.56")])
    func strayCommasReadLikeStrayDots(raw: String, expected: String) {
        #expect(AmountEntryText.sanitize(raw) == expected)
    }

    /// One conversion for Add money, Cash out and Withdraw, which each carried their own copy.
    @Test func microsRoundsToTheNearestMicro() {
        #expect(AmountEntryText.micros("12.34") == 12_340_000)
        #expect(AmountEntryText.micros("0.10") == 100_000)
        #expect(AmountEntryText.micros("1250.50") == 1_250_500_000)
        #expect(AmountEntryText.micros("") == nil)
        #expect(AmountEntryText.micros("abc") == nil)
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
