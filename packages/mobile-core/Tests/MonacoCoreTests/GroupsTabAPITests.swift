import XCTest
@testable import MonacoCore

final class GroupsTabAPITests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    // MARK: - searchGroups

    func testSearchGroups_sendsQueryLimitAndCursor_andDecodesResponse() async throws {
        // Arrange
        let token = TestFixtures.fixtureSessionToken
        var capturedPath: String?
        var capturedQueryItems: [URLQueryItem]?
        var capturedAuthorization: String?

        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "groups_search", withExtension: "json")
        )
        let fixtureData = try Data(contentsOf: fixtureURL)

        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
            capturedQueryItems = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems
            capturedAuthorization = request.value(forHTTPHeaderField: "Authorization")
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, fixtureData)
        }

        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { token }
        )

        // Act
        let result = try await client.searchGroups(query: "weekend", limit: 10, cursor: "page2")

        // Assert
        XCTAssertEqual(capturedPath, "/v1/groups/search")
        XCTAssertEqual(capturedQueryItems, [
            URLQueryItem(name: "q", value: "weekend"),
            URLQueryItem(name: "limit", value: "10"),
            URLQueryItem(name: "cursor", value: "page2"),
        ])
        XCTAssertEqual(capturedAuthorization, "Bearer \(token)")
        XCTAssertEqual(result.groups.count, 2)
        XCTAssertEqual(result.nextCursor, "eyJvZmZzZXQiOjIwfQ==")
    }

    func testSearchGroups_withoutCursor_omitsCursorQueryItem() async throws {
        // Arrange
        var capturedQueryItems: [URLQueryItem]?

        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "groups_search", withExtension: "json")
        )
        let fixtureData = try Data(contentsOf: fixtureURL)

        MockURLProtocol.requestHandler = { request in
            capturedQueryItems = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, fixtureData)
        }

        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { TestFixtures.fixtureSessionToken }
        )

        // Act
        _ = try await client.searchGroups(query: "weekend", limit: 20, cursor: nil)

        // Assert
        XCTAssertEqual(capturedQueryItems, [
            URLQueryItem(name: "q", value: "weekend"),
            URLQueryItem(name: "limit", value: "20"),
        ])
    }

    // MARK: - groupLeaderboard

    func testGroupLeaderboard_sendsLimit_andDecodesResponse() async throws {
        // Arrange
        let token = TestFixtures.fixtureSessionToken
        var capturedPath: String?
        var capturedQueryItems: [URLQueryItem]?
        var capturedAuthorization: String?

        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "groups_leaderboard", withExtension: "json")
        )
        let fixtureData = try Data(contentsOf: fixtureURL)

        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
            capturedQueryItems = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems
            capturedAuthorization = request.value(forHTTPHeaderField: "Authorization")
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, fixtureData)
        }

        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { token }
        )

        // Act
        let result = try await client.groupLeaderboard(limit: 50)

        // Assert
        XCTAssertEqual(capturedPath, "/v1/groups/leaderboard")
        XCTAssertEqual(capturedQueryItems, [URLQueryItem(name: "limit", value: "50")])
        XCTAssertEqual(capturedAuthorization, "Bearer \(token)")
        XCTAssertEqual(result.groups.count, 2)
        XCTAssertEqual(result.groups[0].rank, 1)
    }

    // MARK: - myGroupsPnLHistory

    func testMyGroupsPnLHistory_sendsRange_andDecodesResponse() async throws {
        // Arrange
        let token = TestFixtures.fixtureSessionToken
        var capturedPath: String?
        var capturedQueryItems: [URLQueryItem]?
        var capturedAuthorization: String?

        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "groups_pnl_history", withExtension: "json")
        )
        let fixtureData = try Data(contentsOf: fixtureURL)

        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
            capturedQueryItems = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems
            capturedAuthorization = request.value(forHTTPHeaderField: "Authorization")
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, fixtureData)
        }

        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { token }
        )

        // Act
        let result = try await client.myGroupsPnLHistory(range: .oneMonth)

        // Assert
        XCTAssertEqual(capturedPath, "/v1/groups/pnl-history")
        XCTAssertEqual(capturedQueryItems, [URLQueryItem(name: "range", value: "1M")])
        XCTAssertEqual(capturedAuthorization, "Bearer \(token)")
        XCTAssertEqual(result.series.count, 2)
        XCTAssertEqual(result.series[0].points.count, 4)
    }

    // MARK: - groupPnLHistory

    func testGroupPnLHistory_sendsGroupIdInPathAndRangeInQuery_andDecodesResponse() async throws {
        // Arrange
        let token = TestFixtures.fixtureSessionToken
        var capturedPath: String?
        var capturedQueryItems: [URLQueryItem]?
        var capturedAuthorization: String?

        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "group_pnl_history", withExtension: "json")
        )
        let fixtureData = try Data(contentsOf: fixtureURL)

        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
            capturedQueryItems = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems
            capturedAuthorization = request.value(forHTTPHeaderField: "Authorization")
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, fixtureData)
        }

        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { token }
        )

        // Act
        let result = try await client.groupPnLHistory(groupId: "3f9a1b2c-4d5e-4f6a-8b7c-9d0e1f2a3b4c", range: .threeMonths)

        // Assert
        XCTAssertEqual(capturedPath, "/v1/groups/3f9a1b2c-4d5e-4f6a-8b7c-9d0e1f2a3b4c/pnl-history")
        XCTAssertEqual(capturedQueryItems, [URLQueryItem(name: "range", value: "3M")])
        XCTAssertEqual(capturedAuthorization, "Bearer \(token)")
        XCTAssertEqual(result.groupID, "3f9a1b2c-4d5e-4f6a-8b7c-9d0e1f2a3b4c")
        XCTAssertEqual(result.points.count, 2)
    }

    // MARK: - Error handling

    func testGroupsTabEndpoints_on401_throwHttpStatusError() async throws {
        // Arrange
        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 401,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data())
        }

        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { TestFixtures.fixtureSessionToken }
        )

        // Act / Assert
        do {
            _ = try await client.searchGroups(query: "weekend", limit: 20, cursor: nil)
            XCTFail("Expected httpStatus(401) to be thrown")
        } catch let MonacoAPIError.httpStatus(code) {
            XCTAssertEqual(code, 401)
        }
    }

    func testGroupsTabEndpoints_onMalformedJSON_throwDecodingError() async throws {
        // Arrange
        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, Data(#"{"groups": [}"#.utf8))
        }

        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { TestFixtures.fixtureSessionToken }
        )

        // Act / Assert
        do {
            _ = try await client.searchGroups(query: "weekend", limit: 20, cursor: nil)
            XCTFail("Expected a DecodingError to be thrown")
        } catch is DecodingError {
            // expected
        }
    }

    private func makeMockURLSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return URLSession(configuration: configuration)
    }
}
