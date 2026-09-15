import XCTest
@testable import MonacoCore

final class PrivyAuthConfigTests: XCTestCase {
    func testFromEnvironment_readsPrivyIDsAndLoginFlags() {
        // Arrange
        let environment: [String: String] = [
            "PRIVY_APP_ID": "app-123",
            "PRIVY_APP_CLIENT_ID": "client-456",
            "PRIVY_SMS_LOGIN_ENABLED": "true",
            "PRIVY_EMAIL_LOGIN_ENABLED": "1",
        ]

        // Act
        let config = PrivyAuthConfig.fromEnvironment(environment)

        // Assert
        XCTAssertEqual(config.appID, "app-123")
        XCTAssertEqual(config.appClientID, "client-456")
        XCTAssertTrue(config.smsLoginEnabled)
        XCTAssertTrue(config.emailLoginEnabled)
        XCTAssertTrue(config.isConfigured)
    }

    func testFromEnvironment_missingIDs_isNotConfigured() {
        // Arrange
        let environment: [String: String] = [:]

        // Act
        let config = PrivyAuthConfig.fromEnvironment(environment)

        // Assert
        XCTAssertFalse(config.isConfigured)
        XCTAssertTrue(config.smsLoginEnabled)
        XCTAssertTrue(config.emailLoginEnabled)
    }

    func testFromEnvironment_canDisableLoginMethods() {
        // Arrange
        let environment: [String: String] = [
            "PRIVY_APP_ID": "app-123",
            "PRIVY_APP_CLIENT_ID": "client-456",
            "PRIVY_SMS_LOGIN_ENABLED": "false",
            "PRIVY_EMAIL_LOGIN_ENABLED": "off",
        ]

        // Act
        let config = PrivyAuthConfig.fromEnvironment(environment)

        // Assert
        XCTAssertFalse(config.smsLoginEnabled)
        XCTAssertFalse(config.emailLoginEnabled)
    }
}
