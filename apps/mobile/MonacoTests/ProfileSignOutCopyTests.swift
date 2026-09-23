import Testing
@testable import Monaco

struct ProfileSignOutCopyTests {
    @Test func bothChannelsNameTextAndEmail() {
        let copy = ProfileSignOutCopy.message(smsLoginEnabled: true, emailLoginEnabled: true)
        #expect(copy.contains("text"))
        #expect(copy.contains("email"))
        #expect(copy.hasPrefix("Your money stays where it is."))
    }

    @Test func smsOnlyDoesNotMentionEmail() {
        let copy = ProfileSignOutCopy.message(smsLoginEnabled: true, emailLoginEnabled: false)
        #expect(copy.contains("by text"))
        #expect(!copy.lowercased().contains("email"))
    }

    @Test func emailOnlyDoesNotPromiseAText() {
        let copy = ProfileSignOutCopy.message(smsLoginEnabled: false, emailLoginEnabled: true)
        #expect(copy.contains("by email"))
        #expect(!copy.contains("text"))
    }

    @Test func noChannelNamesNeither() {
        let copy = ProfileSignOutCopy.message(smsLoginEnabled: false, emailLoginEnabled: false)
        #expect(!copy.contains("text"))
        #expect(!copy.lowercased().contains("email"))
        #expect(copy.contains("code"))
    }
}
