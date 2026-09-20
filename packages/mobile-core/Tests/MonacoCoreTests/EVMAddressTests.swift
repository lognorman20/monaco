import XCTest
@testable import MonacoCore

final class EVMAddressTests: XCTestCase {
    private let usdc = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"

    func testValidAddresses_passAndAreTrimmedLowercased() {
        XCTAssertEqual(try EVMAddress.validate("  \(usdc)\n").get(), usdc.lowercased())
    }

    func testEmpty() {
        XCTAssertEqual(EVMAddress.validate("   "), .failure(.empty))
    }

    func testCharactersOutsideHex_areNamed() {
        XCTAssertEqual(EVMAddress.validate("0xzzzz"), .failure(.badCharacter("z")))
        XCTAssertEqual(EVMAddress.validate("zz.sol"), .failure(.badCharacter("z")))
    }

    func testWrongLength_isNotAnAccountAddress() {
        XCTAssertEqual(EVMAddress.validate("0x8335"), .failure(.notAnAccountAddress))
        XCTAssertEqual(EVMAddress.validate(usdc + "abcd"), .failure(.notAnAccountAddress))
    }

    func testOwnDepositAddress_isRefused() {
        XCTAssertEqual(
            EVMAddress.validate(usdc, ownDepositAddress: " \(usdc) "),
            .failure(.ownDepositAddress)
        )
        XCTAssertTrue(EVMAddress.isValid(usdc, ownDepositAddress: "0x000000000000000000000000000000000000dEaD"))
        XCTAssertTrue(EVMAddress.isValid(usdc, ownDepositAddress: ""))
    }

    func testEveryProblemHasCopy() {
        let problems: [EVMAddressProblem] = [.empty, .badCharacter("z"), .notAnAccountAddress, .ownDepositAddress]
        for problem in problems {
            XCTAssertFalse(EVMAddress.message(for: problem).isEmpty)
        }
    }

    func testShortened() {
        XCTAssertEqual(EVMAddress.shortened(usdc.lowercased()), "0x83…2913")
        XCTAssertEqual(EVMAddress.shortened("short"), "short")
    }
}
