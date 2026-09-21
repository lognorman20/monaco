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
              "userId": "550e8400-e29b-41d4-a716-446655440000",
              "displayName": "Alfred",
              "memberWalletAddress": "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
              "profilePhotoUrl": null,
              "createdAt": "2026-09-01T14:30:00Z"
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

    func testAPIClient_listMarketAssets_callsV1Assets() async throws {
        var capturedPath: String?
        var capturedQuery: String?
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_assets", withExtension: "json")
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
        let page = try await client.listMarketAssets(query: "AAPL", limit: 25, offset: 0)
        XCTAssertEqual(capturedPath, "/v1/assets")
        XCTAssertTrue(capturedQuery?.contains("query=AAPL") == true)
        XCTAssertEqual(page.assets.count, 1)
        XCTAssertTrue(page.hasMore)
    }

    func testAPIClient_getPopularAssets_callsV1AssetsPopular() async throws {
        var capturedPath: String?
        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
            let body = #"{"assets":[]}"#
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
        let popular = try await client.getPopularAssets(limit: 10)
        XCTAssertEqual(capturedPath, "/v1/assets/popular")
        XCTAssertEqual(popular.assets, [])
    }

    func testAPIClient_getMarketAsset_callsV1AssetsSymbol() async throws {
        var capturedPath: String?
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_asset_detail", withExtension: "json")
        )
        let fixtureData = try Data(contentsOf: fixtureURL)
        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
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
        let detail = try await client.getMarketAsset(symbol: "AAPLc")
        XCTAssertEqual(capturedPath, "/v1/assets/AAPLc")
        // The backend's own label; the venue is not named in it.
        XCTAssertEqual(detail.liquidity.label, "Via DEX")
    }

    func testAPIClient_getMarketAssetChart_callsV1AssetsChart() async throws {
        var capturedPath: String?
        var capturedQuery: String?
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_asset_chart", withExtension: "json")
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
        let chart = try await client.getMarketAssetChart(symbol: "AAPLx", range: .oneWeek)
        XCTAssertEqual(capturedPath, "/v1/assets/AAPLx/chart")
        XCTAssertEqual(capturedQuery, "range=1W")
        XCTAssertEqual(chart.points.count, 2)
    }

    func testAPIClient_getHeldAssets_callsV1AssetsHeldWithTheBearer() async throws {
        var capturedPath: String?
        var capturedAuth: String?
        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
            capturedAuth = request.value(forHTTPHeaderField: "Authorization")
            let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!
            let body = """
            {"held":[{"asset":{"symbol":"AAPLc","name":"Apple","tokenAddress":"0xb2","routable":true},"cabals":[],"totalValueUsd":"1.20","totalDollarPnl":"+0.20","mySliceUsd":"0.60"}],"upForVote":[]}
            """
            return (response, Data(body.utf8))
        }
        let client = MonacoAPIClient(baseURL: URL(string: "https://api.test")!, session: makeMockURLSession(), accessTokenProvider: { TestFixtures.fixtureSessionToken })
        let held = try await client.getHeldAssets()
        XCTAssertEqual(capturedPath, "/v1/assets/held")
        XCTAssertEqual(capturedAuth, "Bearer \(TestFixtures.fixtureSessionToken)")
        XCTAssertEqual(held.held.first?.mySliceUsd, "0.60")
    }

    func testAPIClient_getHeldAssets_401IsAnHTTPStatusNotADecodeError() async throws {
        MockURLProtocol.requestHandler = { request in
            (HTTPURLResponse(url: request.url!, statusCode: 401, httpVersion: nil, headerFields: nil)!, Data("{}".utf8))
        }
        let client = MonacoAPIClient(baseURL: URL(string: "https://api.test")!, session: makeMockURLSession(), accessTokenProvider: { TestFixtures.fixtureSessionToken })
        do {
            _ = try await client.getHeldAssets()
            XCTFail("a 401 decoded")
        } catch MonacoAPIError.httpStatus(let status) {
            XCTAssertEqual(status, 401)
        }
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
        XCTAssertEqual(dto.userId, "550e8400-e29b-41d4-a716-446655440000")
        XCTAssertEqual(dto.displayName, "Alfred")
        XCTAssertEqual(dto.memberWalletAddress, "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
        XCTAssertEqual(
            dto.profilePhotoUrl,
            "https://example.supabase.co/storage/v1/object/public/avatars/550e8400-e29b-41d4-a716-446655440000/3f2a.jpg"
        )
        XCTAssertEqual(dto.createdAt, Date(timeIntervalSince1970: 1_788_273_000))
    }

    private func makeMockURLSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return URLSession(configuration: configuration)
    }
}
