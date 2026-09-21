import Testing
@testable import Monaco

struct DynamicLoginChannelTests {
    @Test func smsCopyMentionsTextNotEmail() {
        let copy = DynamicLoginChannel.sms.deviceRegistrationCopy
        #expect(copy.contains("text"))
        #expect(!copy.lowercased().contains("email"))
    }

    @Test func emailCopyMentionsEmail() {
        #expect(DynamicLoginChannel.email.deviceRegistrationCopy.contains("email"))
    }
}
