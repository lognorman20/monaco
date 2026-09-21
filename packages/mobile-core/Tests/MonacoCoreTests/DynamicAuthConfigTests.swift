import XCTest
@testable import MonacoCore

final class DynamicAuthConfigTests: XCTestCase {
    func testDynamicAuthConfig_readsEnvironmentIDAndFlags() throws {
        let environment: [String: String] = [
            "DYNAMIC_ENVIRONMENT_ID": "env-123",
            "AUTH_SMS_LOGIN_ENABLED": "true",
            "AUTH_EMAIL_LOGIN_ENABLED": "0",
        ]

        let config = try DynamicAuthConfig.fromEnvironment(environment)

        XCTAssertEqual(config.environmentID, "env-123")
        XCTAssertTrue(config.smsLoginEnabled)
        XCTAssertFalse(config.emailLoginEnabled)
        XCTAssertTrue(config.isConfigured)
    }

    func testDynamicAuthConfig_missingEnvironmentID_fails() {
        XCTAssertThrowsError(try DynamicAuthConfig.fromEnvironment([:])) { error in
            XCTAssertEqual(error as? DynamicAuthConfigError, .missingEnvironmentID)
        }
    }

    func testFromEnvironment_emailDefaultsOn() throws {
        let config = try DynamicAuthConfig.fromEnvironment([
            "DYNAMIC_ENVIRONMENT_ID": "env-123",
        ])
        XCTAssertTrue(config.smsLoginEnabled)
        XCTAssertTrue(config.emailLoginEnabled)
    }

    func testFromEnvironment_canDisableLoginMethods() throws {
        let config = try DynamicAuthConfig.fromEnvironment([
            "DYNAMIC_ENVIRONMENT_ID": "env-123",
            "AUTH_SMS_LOGIN_ENABLED": "false",
            "AUTH_EMAIL_LOGIN_ENABLED": "off",
        ])
        XCTAssertFalse(config.smsLoginEnabled)
        XCTAssertFalse(config.emailLoginEnabled)
    }
}
