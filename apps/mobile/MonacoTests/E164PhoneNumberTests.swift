import Testing
@testable import Monaco

/// Sign-in only accepts E.164. The field invites iOS autofill, which hands back a formatted
/// number, so everything a member can realistically put in the field is parsed here.
struct E164PhoneNumberTests {
    @Test func autofillFormattingIsStripped() {
        #expect(E164PhoneNumber("+1 (555) 123-4567")?.value == "+15551234567")
        #expect(E164PhoneNumber("(555) 123-4567")?.value == "+15551234567")
        #expect(E164PhoneNumber("555-123-4567")?.value == "+15551234567")
        #expect(E164PhoneNumber(" +15551234567 ")?.value == "+15551234567")
    }

    @Test func contactsInvisibleMarksAreStripped() {
        // Contacts wraps numbers in bidi marks when the device language is right-to-left.
        #expect(E164PhoneNumber("\u{202A}+1 (555) 123-4567\u{202C}")?.value == "+15551234567")
        #expect(E164PhoneNumber("\u{202A}+44 20 7946 0958\u{202C}")?.value == "+442079460958")
        #expect(E164PhoneNumber("+1\u{00A0}555\u{00A0}123\u{00A0}4567")?.value == "+15551234567")
    }

    @Test func internationalNumbersKeepTheirCountryCode() {
        #expect(E164PhoneNumber("+44 20 7946 0958")?.value == "+442079460958")
        #expect(E164PhoneNumber("0044 20 7946 0958")?.value == "+442079460958")
    }

    @Test func numbersWeCannotSendToAreRejected() {
        #expect(E164PhoneNumber("") == nil)
        #expect(E164PhoneNumber("555") == nil)
        #expect(E164PhoneNumber("call me") == nil)
        #expect(E164PhoneNumber("1-800-CALL-NOW") == nil)
        #expect(E164PhoneNumber("+1+5551234567") == nil)
        // No country code, and not a US-shaped number.
        #expect(E164PhoneNumber("2079460958123") == nil)
        #expect(E164PhoneNumber("+0123456789") == nil)
        #expect(E164PhoneNumber("+1234567890123456") == nil)
    }

    @Test func nonASCIIDigitsAreNotMistakenForDigits() {
        #expect(E164PhoneNumber("٥٥٥١٢٣٤٥٦٧") == nil)
        #expect(E164PhoneNumber("½") == nil)
    }

    /// Characters that read as numbers but are not digits used to fall through as
    /// "formatting" and be dropped in silence, so a number that was not the one on screen
    /// went out and looked like it had been accepted.
    @Test func numericLookingCharactersAreRefusedRatherThanDropped() {
        #expect(E164PhoneNumber("15551234567½") == nil)
        #expect(E164PhoneNumber("①5551234567") == nil)
        #expect(E164PhoneNumber("555123456７") == nil)
        #expect(E164PhoneNumber("555*123*4567") == nil)
        #expect(E164PhoneNumber("555,123,4567") == nil)
    }

    /// A 10-digit string starting "00" is not a US number — NANP area codes never start
    /// with 0 — so it is the trunk-prefixed international reading that applies, whichever
    /// branch is written first.
    @Test func aTrunkPrefixIsNotConfusedWithAUSNumber() {
        #expect(E164PhoneNumber("0012345678")?.value == "+12345678")
        // Still a US number when it actually looks like one.
        #expect(E164PhoneNumber("2125551234")?.value == "+12125551234")
        #expect(E164PhoneNumber("5551234567")?.value == "+15551234567")
        // An area code starting 0 or 1 is not US, and "01…" is not a trunk prefix either.
        #expect(E164PhoneNumber("0125551234") == nil)
        #expect(E164PhoneNumber("1125551234") == nil)
    }

    /// What `LoginPhone` used to cover, now read by the one parser.
    @Test func aBareUSNumberGainsItsCountryCode() {
        #expect(E164PhoneNumber("3475757193")?.value == "+13475757193")
        #expect(E164PhoneNumber("13475757193")?.value == "+13475757193")
        #expect(E164PhoneNumber("+13475757193")?.value == "+13475757193")
        #expect(E164PhoneNumber("3475757") == nil)
        #expect(E164PhoneNumber("347575719") == nil)
    }

    /// Dynamic takes the number split into dial code, region and subscriber number. The split
    /// comes off the value the form already validated, so it cannot disagree with it.
    @Test func theNumberSplitsIntoWhatDynamicSends() throws {
        let us = try #require(E164PhoneNumber("+1 (555) 123-4567"))
        #expect(us.countryCode == "1")
        #expect(us.regionCode == "US")
        #expect(us.nationalNumber == "5551234567")

        let uk = try #require(E164PhoneNumber("+44 20 7946 0958"))
        #expect(uk.countryCode == "44")
        #expect(uk.regionCode == "GB")
        #expect(uk.nationalNumber == "2079460958")

        let portugal = try #require(E164PhoneNumber("+351 912 345 678"))
        #expect(portugal.countryCode == "351")
        #expect(portugal.regionCode == "PT")
        #expect(portugal.nationalNumber == "912345678")
    }

    /// One length rule. The old parser for Dynamic demanded 11 digits on top of E.164's
    /// 8 to 15, which refused every 10-digit international number, Singapore's among them.
    @Test func shortInternationalNumbersAreAccepted() {
        #expect(E164PhoneNumber("+65 9123 4567")?.value == "+6591234567")
        #expect(E164PhoneNumber("+65 9123 4567")?.nationalNumber == "91234567")
    }

    /// A country code we cannot name used to be sent as a US number with the country code
    /// folded into it: a text to a stranger. It is refused before the request instead.
    @Test func aCountryCodeWeCannotNameIsRefused() {
        #expect(E164PhoneNumber("+299 32 1234") == nil)
    }

    @Test func usNumbersReadBackTheWayTheyWereTyped() {
        #expect(E164PhoneNumber("+15551234567")?.displayValue == "(555) 123-4567")
        #expect(E164PhoneNumber("+442079460958")?.displayValue == "+442079460958")
    }
}
