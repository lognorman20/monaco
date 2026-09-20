import Foundation
import Testing
@testable import Monaco

struct DepositAddressTests {
    @Test func aRealAddressComesBackTrimmed() {
        #expect(DepositAddress.usable("  7Yk3Qn5wF2c  ") == "7Yk3Qn5wF2c")
    }

    /// A wallet still being made comes back empty, and dev environments hand out "FAKE…".
    /// Neither may reach the clipboard, because real money gets sent to what is on this screen.
    @Test func aPlaceholderIsNotAnAddress() {
        #expect(DepositAddress.usable(nil) == nil)
        #expect(DepositAddress.usable("") == nil)
        #expect(DepositAddress.usable("   ") == nil)
        #expect(DepositAddress.usable("FAKE_WALLET_123") == nil)
        #expect(DepositAddress.usable("  FAKE123  ") == nil)
    }
}
