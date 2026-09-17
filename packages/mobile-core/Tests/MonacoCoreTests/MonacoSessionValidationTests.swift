import XCTest
@testable import MonacoCore

final class MonacoSessionValidationTests: XCTestCase {
    func testShouldInvalidateSession_noStoredUserId_isFalse() {
        XCTAssertFalse(MonacoSessionValidation.shouldInvalidateSession(storedUserId: nil, serverUserId: "user-1"))
    }

    func testShouldInvalidateSession_matchingUserId_isFalse() {
        XCTAssertFalse(MonacoSessionValidation.shouldInvalidateSession(storedUserId: "user-1", serverUserId: "user-1"))
    }

    func testShouldInvalidateSession_mismatchedUserId_isTrue() {
        XCTAssertTrue(MonacoSessionValidation.shouldInvalidateSession(storedUserId: "user-old", serverUserId: "user-new"))
    }
}
