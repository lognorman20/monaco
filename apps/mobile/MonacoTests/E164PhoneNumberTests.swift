import Testing
@testable import Monaco

/// Privy only accepts E.164. The field invites iOS autofill, which hands back a formatted
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
        // No country code, and not a US-shaped number.
        #expect(E164PhoneNumber("2079460958123") == nil)
        #expect(E164PhoneNumber("+0123456789") == nil)
        #expect(E164PhoneNumber("+1234567890123456") == nil)
    }

    @Test func nonASCIIDigitsAreNotMistakenForDigits() {
        #expect(E164PhoneNumber("٥٥٥١٢٣٤٥٦٧") == nil)
        #expect(E164PhoneNumber("½") == nil)
    }

    @Test func usNumbersReadBackTheWayTheyWereTyped() {
        #expect(E164PhoneNumber("+15551234567")?.displayValue == "(555) 123-4567")
        #expect(E164PhoneNumber("+442079460958")?.displayValue == "+442079460958")
    }
}
