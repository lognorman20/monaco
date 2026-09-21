import Foundation
import Testing
@testable import Monaco

struct LoginPhoneTests {
    @Test func tenDigitUSNumberBecomesE164() {
        #expect(LoginPhone.normalizedE164("3475757193") == "+13475757193")
        #expect(LoginPhone.isComplete("3475757193"))
    }

    @Test func incompleteNumberCannotSend() {
        #expect(!LoginPhone.isComplete("3475757"))
        #expect(!LoginPhone.isComplete("347575719"))
    }

    @Test func alreadyE164IsUnchanged() {
        #expect(LoginPhone.normalizedE164("+13475757193") == "+13475757193")
        #expect(LoginPhone.isComplete("+13475757193"))
    }
}
