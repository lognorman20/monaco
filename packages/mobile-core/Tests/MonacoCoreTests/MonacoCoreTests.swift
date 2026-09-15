import XCTest
@testable import MonacoCore

final class MonacoCoreTests: XCTestCase {
    func testDefaultAPIBaseURL_isLocalhost8080() {
        // Arrange
        let expectedHost = "localhost"
        let expectedPort = 8080

        // Act
        let url = MonacoConfig.defaultAPIBaseURL

        // Assert
        XCTAssertEqual(url.host, expectedHost)
        XCTAssertEqual(url.port, expectedPort)
    }
}
