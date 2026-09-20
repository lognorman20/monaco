import XCTest
@testable import MonacoCore

final class MonacoCoreTests: XCTestCase {
    func testAPIBaseURL_withoutBundleConfig_isLocalhost8080() {
        // Arrange: the host test bundle carries no MONACO_* Info.plist keys.
        let expectedHost = "localhost"
        let expectedPort = 8080

        // Act
        let url = MonacoConfig.apiBaseURL

        // Assert
        XCTAssertEqual(url.host, expectedHost)
        XCTAssertEqual(url.port, expectedPort)
        XCTAssertEqual(MonacoConfig.api.environment, .local)
    }
}
