import Foundation
import Testing
@testable import Monaco

struct DepositAddressTests {
    @Test func aBaseAddressComesBackTrimmedAndLowercase() {
        #expect(
            DepositAddress.usable("  0x83358384D0c7Ed7B3E4B7D6E5e2F7F00B2913aBc  ")
                == "0x83358384d0c7ed7b3e4b7d6e5e2f7f00b2913abc"
        )
    }

    /// A wallet still being made comes back empty. Nothing that is not a Base address may reach
    /// the clipboard, because real money gets sent to what is on this screen.
    @Test func aPlaceholderIsNotAnAddress() {
        #expect(DepositAddress.usable(nil) == nil)
        #expect(DepositAddress.usable("") == nil)
        #expect(DepositAddress.usable("   ") == nil)
        #expect(DepositAddress.usable("FAKE_WALLET_123") == nil)
    }

    /// Shapes a Base address is not: too short, a character that is not hex, a Solana-style
    /// base58 string left over from an old profile.
    @Test func anythingButZeroXAndFortyHexIsRefused() {
        #expect(DepositAddress.usable("0x8335") == nil)
        #expect(DepositAddress.usable("0x83358384d0c7ed7b3e4b7d6e5e2f7f00b2913abz") == nil)
        #expect(DepositAddress.usable("0x83358384d0c7ed7b3e4b7d6e5e2f7f00b2913abc0") == nil)
        #expect(DepositAddress.usable("7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU") == nil)
    }
}
