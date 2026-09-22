import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// The destination field on Cash out. A Base address is 42 characters; the copy must say so, and
/// must never carry the Solana-era "32 to 44 characters".
struct WithdrawAddressCopyTests {
    private func message(_ pasted: String, own: String? = nil) -> String? {
        guard case .failure(let problem) = EVMAddress.validate(pasted, ownDepositAddress: own) else { return nil }
        return WithdrawAddressCopy.message(for: problem, pasted: pasted)
    }

    @Test func aTruncatedAddressIsToldTheLengthABaseAddressHas() {
        // The last three characters were lost in the copy.
        let pasted = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02"
        #expect(message(pasted) == "A Base address is 42 characters: 0x, then 40 letters and numbers. This one is 39.")
    }

    /// Forty hex characters with the 0x dropped is the other common paste.
    @Test func anAddressMissingItsPrefixIsCountedAsPasted() {
        let pasted = "  833589fcd6edb6e08f4c7c32d4f71b54bda02913 "
        #expect(message(pasted)?.hasSuffix("This one is 40.") == true)
    }

    @Test func aValidBaseAddressHasNoProblem() {
        #expect(message("0x833589fcd6edb6e08f4c7c32d4f71b54bda02913") == nil)
        #expect(WithdrawAddressCopy.baseAddressLength == 42)
    }

    @Test func theOtherProblemsKeepTheirOwnWords() {
        #expect(message("0x833589fcd6edb6e08f4c7c32d4f71b54bda0291z") == EVMAddress.message(for: .badCharacter("z")))
        let own = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
        #expect(message(own.uppercased().replacingOccurrences(of: "0X", with: "0x"), own: own) == EVMAddress.message(for: .ownDepositAddress))
    }

    @Test func nothingSaysSolanaOrItsLengths() {
        for pasted in ["0x12", "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "0x833589fcd6edb6e08f4c7c32d4f71b54bda0291"] {
            let copy = message(pasted) ?? ""
            #expect(!copy.contains("Solana"))
            #expect(!copy.contains("32 to 44"))
        }
    }
}
