import XCTest
@testable import MonacoCore

final class SolanaAddressTests: XCTestCase {
    /// USDC mint and the system program: two real 32-byte addresses, 44 and 32 characters.
    private let usdcMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
    private let systemProgram = "11111111111111111111111111111111"

    func testValidAddresses_passAndAreTrimmed() {
        XCTAssertEqual(try SolanaAddress.validate("  \(usdcMint)\n").get(), usdcMint)
        XCTAssertEqual(try SolanaAddress.validate(systemProgram).get(), systemProgram)
        XCTAssertEqual(SolanaAddress.decodeBase58(usdcMint)?.count, 32)
    }

    func testEmpty() {
        XCTAssertEqual(SolanaAddress.validate("   "), .failure(.empty))
    }

    func testCharactersOutsideBase58_areNamed() {
        let withZero = "0" + usdcMint.dropFirst()
        XCTAssertEqual(SolanaAddress.validate(withZero), .failure(.badCharacter("0")))
        XCTAssertEqual(SolanaAddress.validate("0x52908400098527886E0F7030069857D2E4169EE7"), .failure(.badCharacter("0")))
        XCTAssertEqual(SolanaAddress.validate("name.sol"), .failure(.badCharacter(".")))
    }

    func testWrongLength_isNotAnAccountAddress() {
        XCTAssertEqual(SolanaAddress.validate(String(usdcMint.dropLast(4))), .failure(.notAnAccountAddress))
        XCTAssertEqual(SolanaAddress.validate(usdcMint + "abcd"), .failure(.notAnAccountAddress))
        // A 64-byte transaction signature is base58 too, but not an address.
        XCTAssertEqual(SolanaAddress.validate(usdcMint + usdcMint), .failure(.notAnAccountAddress))
    }

    func testOwnDepositAddress_isRefused() {
        XCTAssertEqual(
            SolanaAddress.validate(usdcMint, ownDepositAddress: " \(usdcMint) "),
            .failure(.ownDepositAddress)
        )
        XCTAssertTrue(SolanaAddress.isValid(usdcMint, ownDepositAddress: systemProgram))
        XCTAssertTrue(SolanaAddress.isValid(usdcMint, ownDepositAddress: ""))
    }

    func testEveryProblemHasCopy() {
        let problems: [SolanaAddressProblem] = [.empty, .badCharacter("0"), .notAnAccountAddress, .ownDepositAddress]
        for problem in problems {
            XCTAssertFalse(SolanaAddress.message(for: problem).isEmpty)
        }
    }

    func testShortened() {
        XCTAssertEqual(SolanaAddress.shortened(usdcMint), "EPjF…Dt1v")
        XCTAssertEqual(SolanaAddress.shortened("short"), "short")
    }
}
