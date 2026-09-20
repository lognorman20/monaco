import XCTest
@testable import MonacoCore

final class MonacoAPIConfigurationTests: XCTestCase {
    private func plist(_ environment: String?, _ baseURL: String?) -> [String: Any] {
        var info: [String: Any] = [:]
        if let environment { info[MonacoAPIConfiguration.environmentKey] = environment }
        if let baseURL { info[MonacoAPIConfiguration.baseURLKey] = baseURL }
        return info
    }

    private func assertRejects(
        _ expected: MonacoAPIConfigurationError,
        info: [String: Any],
        processEnvironment: [String: String] = [:],
        build: MonacoBuildKind,
        file: StaticString = #filePath,
        line: UInt = #line
    ) {
        XCTAssertThrowsError(
            try MonacoAPIConfiguration.resolve(
                infoDictionary: info,
                processEnvironment: processEnvironment,
                build: build
            ),
            file: file,
            line: line
        ) { error in
            XCTAssertEqual(error as? MonacoAPIConfigurationError, expected, file: file, line: line)
        }
    }

    // MARK: Release

    func testRelease_acceptsHTTPS() throws {
        let config = try MonacoAPIConfiguration.resolve(
            infoDictionary: plist("production", "https://api.monaco.test"),
            processEnvironment: [:],
            build: .release
        )

        XCTAssertEqual(config.environment, .production)
        XCTAssertEqual(config.baseURL.absoluteString, "https://api.monaco.test")
        XCTAssertEqual(config.source, .infoPlist)
    }

    func testRelease_acceptsStagingHTTPSWithPortAndPath() throws {
        let config = try MonacoAPIConfiguration.resolve(
            infoDictionary: plist("staging", " https://staging.monaco.test:8443/api "),
            processEnvironment: [:],
            build: .release
        )

        XCTAssertEqual(config.environment, .staging)
        XCTAssertEqual(config.baseURL.absoluteString, "https://staging.monaco.test:8443/api")
    }

    func testRelease_rejectsHTTP() {
        assertRejects(
            .insecureScheme("http://api.monaco.test"),
            info: plist("production", "http://api.monaco.test"),
            build: .release
        )
    }

    func testRelease_rejectsLocalHostsEvenOverHTTPS() {
        for host in ["localhost", "LOCALHOST", "127.0.0.1", "[::1]", "0.0.0.0", "my-mac.local"] {
            let raw = "https://\(host):8080"
            assertRejects(.localHost(raw), info: plist("production", raw), build: .release)
        }
    }

    func testRelease_rejectsLocalEnvironment() {
        assertRejects(
            .localEnvironmentInRelease,
            info: plist("local", "https://api.monaco.test"),
            build: .release
        )
    }

    func testRelease_rejectsMissingOrEmptyBaseURL() {
        assertRejects(.missingBaseURL, info: plist("production", nil), build: .release)
        assertRejects(.missingBaseURL, info: plist("production", ""), build: .release)
        assertRejects(.missingBaseURL, info: plist("production", "   "), build: .release)
    }

    func testRelease_rejectsUnexpandedBuildSetting() {
        assertRejects(
            .malformedBaseURL("$(MONACO_API_BASE_URL)"),
            info: plist("production", "$(MONACO_API_BASE_URL)"),
            build: .release
        )
    }

    func testRelease_rejectsMalformedBaseURL() {
        for raw in ["api.monaco.test", "https://", "https:/api.monaco.test", "ftp://api.monaco.test", "not a url"] {
            assertRejects(.malformedBaseURL(raw), info: plist("production", raw), build: .release)
        }
    }

    func testRelease_rejectsCredentialsQueryAndFragment() {
        for raw in [
            "https://user:pw@api.monaco.test",
            "https://api.monaco.test?token=1",
            "https://api.monaco.test#frag",
        ] {
            assertRejects(.malformedBaseURL(raw), info: plist("production", raw), build: .release)
        }
    }

    func testRelease_rejectsMissingOrUnknownEnvironment() {
        assertRejects(.missingEnvironment, info: plist(nil, "https://api.monaco.test"), build: .release)
        assertRejects(.unknownEnvironment("prod"), info: plist("prod", "https://api.monaco.test"), build: .release)
    }

    func testRelease_ignoresProcessEnvironmentOverride() throws {
        let config = try MonacoAPIConfiguration.resolve(
            infoDictionary: plist("production", "https://api.monaco.test"),
            processEnvironment: [MonacoAPIConfiguration.baseURLKey: "http://localhost:8080"],
            build: .release
        )

        XCTAssertEqual(config.baseURL.absoluteString, "https://api.monaco.test")
        XCTAssertEqual(config.source, .infoPlist)
    }

    // MARK: Debug

    func testDebug_defaultsToLocalhostWhenNothingIsConfigured() throws {
        let config = try MonacoAPIConfiguration.resolve(infoDictionary: [:], processEnvironment: [:], build: .debug)

        XCTAssertEqual(config.environment, .local)
        XCTAssertEqual(config.baseURL.absoluteString, "http://localhost:8080")
        XCTAssertEqual(config.source, .builtInLocalDefault)
    }

    func testDebug_readsInfoPlist() throws {
        let config = try MonacoAPIConfiguration.resolve(
            infoDictionary: plist("local", "http://localhost:8080"),
            processEnvironment: [:],
            build: .debug
        )

        XCTAssertEqual(config.environment, .local)
        XCTAssertEqual(config.baseURL.absoluteString, "http://localhost:8080")
        XCTAssertEqual(config.source, .infoPlist)
    }

    func testDebug_processEnvironmentOverridesInfoPlist() throws {
        let config = try MonacoAPIConfiguration.resolve(
            infoDictionary: plist("local", "http://localhost:8080"),
            processEnvironment: [
                MonacoAPIConfiguration.baseURLKey: "https://tunnel.monaco.test",
                MonacoAPIConfiguration.environmentKey: "staging",
            ],
            build: .debug
        )

        XCTAssertEqual(config.environment, .staging)
        XCTAssertEqual(config.baseURL.absoluteString, "https://tunnel.monaco.test")
        XCTAssertEqual(config.source, .processEnvironment)
    }

    func testDebug_blankProcessEnvironmentOverrideIsIgnored() throws {
        let config = try MonacoAPIConfiguration.resolve(
            infoDictionary: plist("local", "http://localhost:8080"),
            processEnvironment: [MonacoAPIConfiguration.baseURLKey: "  "],
            build: .debug
        )

        XCTAssertEqual(config.source, .infoPlist)
    }

    func testDebug_rejectsMalformedOverrideInsteadOfFallingBack() {
        assertRejects(
            .malformedBaseURL("tunnel.monaco.test"),
            info: plist("local", "http://localhost:8080"),
            processEnvironment: [MonacoAPIConfiguration.baseURLKey: "tunnel.monaco.test"],
            build: .debug
        )
    }

    func testDebug_rejectsStagingSelectedWithoutURL() {
        assertRejects(.missingBaseURL, info: plist("staging", ""), build: .debug)
    }

    func testDebug_rejectsUnknownEnvironment() {
        assertRejects(.unknownEnvironment("qa"), info: plist("qa", "https://api.monaco.test"), build: .debug)
    }

    func testDebugSummary_namesEnvironmentURLAndSource() throws {
        let config = try MonacoAPIConfiguration.resolve(
            infoDictionary: plist("staging", "https://staging.monaco.test"),
            processEnvironment: [:],
            build: .debug
        )

        XCTAssertEqual(config.debugSummary, "env staging — https://staging.monaco.test (Info.plist)")
    }

    func testErrorDescriptions_nameTheBuildSettingToFix() {
        let errors: [MonacoAPIConfigurationError] = [
            .missingEnvironment, .unknownEnvironment("x"), .missingBaseURL, .malformedBaseURL("x"),
            .insecureScheme("x"), .localHost("x"), .localEnvironmentInRelease,
        ]
        for error in errors {
            XCTAssertTrue(error.errorDescription?.contains("MONACO_") == true, "\(error)")
        }
    }
}
