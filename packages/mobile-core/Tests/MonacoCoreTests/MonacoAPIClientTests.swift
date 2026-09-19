import XCTest
@testable import MonacoCore

final class MonacoAPIClientTests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    func testAPIClient_afterLogin_sendsAuthorizationHeader() async throws {
        // Arrange
        let token = TestFixtures.fixtureSessionToken
        let expectedAuthorization = "Bearer \(token)"
        var capturedAuthorization: String?

        MockURLProtocol.requestHandler = { request in
            capturedAuthorization = request.value(forHTTPHeaderField: "Authorization")
            let responseBody = """
            {
              "user_id": "550e8400-e29b-41d4-a716-446655440000",
              "display_name": "Alfred",
              "member_wallet_address": "FAKEabcdef1234567890abcdef1234567890"
            }
            """
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, Data(responseBody.utf8))
        }

        let session = makeMockURLSession()
        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: session,
            accessTokenProvider: { token }
        )

        // Act
        _ = try await client.me()

        // Assert
        XCTAssertEqual(capturedAuthorization, expectedAuthorization)
    }

    func testAPIClient_getHome_callsV1Home() async throws {
        // Arrange
        let token = TestFixtures.fixtureSessionToken
        var capturedPath: String?
        var capturedAuthorization: String?

        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
            capturedAuthorization = request.value(forHTTPHeaderField: "Authorization")
            let responseBody = """
            {
              "groups": [],
              "people": []
            }
            """
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, Data(responseBody.utf8))
        }

        let session = makeMockURLSession()
        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: session,
            accessTokenProvider: { token }
        )

        // Act
        let home = try await client.getHome()

        // Assert
        XCTAssertEqual(capturedPath, "/v1/home")
        XCTAssertEqual(capturedAuthorization, "Bearer \(token)")
        XCTAssertEqual(home.groups, [])
        XCTAssertEqual(home.people, [])
    }

    func testAPIClient_getHomeDashboard_callsV1HomeDashboard() async throws {
        // Arrange
        var capturedPath: String?
        var capturedQuery: String?

        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "home_dashboard", withExtension: "json")
        )
        let fixtureData = try Data(contentsOf: fixtureURL)

        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
            capturedQuery = request.url?.query
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
        let dashboard = try await client.getHomeDashboard(leaderboardRange: .all)

        // Assert
        XCTAssertEqual(capturedPath, "/v1/home/dashboard")
        XCTAssertEqual(capturedQuery, "leaderboardRange=ALL")
        XCTAssertEqual(dashboard.myGroups.count, 1)
        XCTAssertEqual(dashboard.myGroups[0].groupID, "g1")
    }

    func testAPIClient_getHomePnLSeries_callsV1HomePnlSeries() async throws {
        var capturedPath: String?
        var capturedQuery: String?
        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
            capturedQuery = request.url?.query
            let body = #"{"points":[]}"#
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, Data(body.utf8))
        }
        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { TestFixtures.fixtureSessionToken }
        )
        let series = try await client.getHomePnLSeries(range: .oneHour)
        XCTAssertEqual(capturedPath, "/v1/home/pnl-series")
        XCTAssertEqual(capturedQuery, "range=1H")
        XCTAssertEqual(series.points, [])
    }

    func testAPIClient_leaveGroup_callsV1Leave() async throws {
        MockURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(url: request.url!, statusCode: 204, httpVersion: nil, headerFields: nil)!
            return (response, Data())
        }
        let client = MonacoAPIClient(baseURL: URL(string: "https://api.test")!, session: makeMockURLSession(), accessTokenProvider: { TestFixtures.fixtureSessionToken })
        try await client.leaveGroup(groupId: "550e8400-e29b-41d4-a716-446655440000")
    }

    func testMeDTO_decodesFixtureJSON() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "me", withExtension: "json")
        )
        let data = try Data(contentsOf: fixtureURL)

        // Act
        let dto = try JSONDecoder().decode(MeDTO.self, from: data)

        // Assert
        XCTAssertEqual(dto.userID, "550e8400-e29b-41d4-a716-446655440000")
        XCTAssertEqual(dto.displayName, "Alfred")
        XCTAssertEqual(dto.memberWalletAddress, "FAKEabcdef1234567890abcdef1234567890")
    }

    private func makeMockURLSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return URLSession(configuration: configuration)
    }
}
