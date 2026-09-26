import XCTest
@testable import MonacoCore

final class MatchupAPITests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    private func makeClient() -> MonacoAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: URLSession(configuration: configuration),
            accessTokenProvider: { TestFixtures.fixtureSessionToken }
        )
    }

    private func respond(_ status: Int, _ data: Data) -> (URLRequest) -> (HTTPURLResponse, Data) {
        { request in
            (HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, data)
        }
    }

    private func fixture(_ name: String) throws -> Data {
        try Data(contentsOf: XCTUnwrap(Bundle.module.url(forResource: name, withExtension: "json")))
    }

    private static let challengeJSON = Data("""
    {"id":"c0ffee00-1111-4a2b-8c3d-000000000001","weekStart":"2026-09-28T00:00:00Z","direction":"outgoing",
     "status":"pending","opponent":{"groupId":"g2","name":"Semis or bust","pictureUrl":null,"memberCount":5},
     "createdAt":"2026-09-23T14:05:00Z","acceptedAt":null}
    """.utf8)

    func testGroupMatchup_getsTheCabalsMatchupWithTheToken() async throws {
        // Arrange
        var captured: URLRequest?
        let data = try fixture("group_matchup")
        MockURLProtocol.requestHandler = { request in
            captured = request
            return self.respond(200, data)(request)
        }

        // Act
        let dto = try await makeClient().groupMatchup(groupId: "g1")

        // Assert
        XCTAssertEqual(captured?.httpMethod, "GET")
        XCTAssertEqual(captured?.url?.path, "/v1/groups/g1/matchup")
        XCTAssertEqual(captured?.value(forHTTPHeaderField: "Authorization"), "Bearer \(TestFixtures.fixtureSessionToken)")
        XCTAssertEqual(dto.record.wins, 4)
    }

    func testHomeMatchupsAndTable_hitTheirRoutes() async throws {
        // Arrange
        var paths: [String] = []
        var queries: [[URLQueryItem]] = []
        let home = try fixture("home_matchups")
        let table = try fixture("matchup_table")
        MockURLProtocol.requestHandler = { request in
            paths.append(request.url!.path)
            queries.append(URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems ?? [])
            return self.respond(200, request.url!.path.hasSuffix("table") ? table : home)(request)
        }
        let client = makeClient()

        // Act
        let homeDTO = try await client.homeMatchups()
        let tableDTO = try await client.matchupTable(limit: 30)

        // Assert
        XCTAssertEqual(paths, ["/v1/home/matchups", "/v1/matchups/table"])
        XCTAssertEqual(queries[1], [URLQueryItem(name: "limit", value: "30")])
        XCTAssertEqual(homeDTO.matchups.count, 2)
        XCTAssertEqual(tableDTO.cabals.count, 2)
    }

    func testChallengeCabal_postsTheOpponentAndAcceptsCreatedOrExisting() async throws {
        // Arrange
        var captured: URLRequest?
        var body: [String: String]?
        var status = 201
        MockURLProtocol.requestHandler = { request in
            captured = request
            if let stream = request.httpBodyStream {
                stream.open()
                var data = Data()
                let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: 1024)
                defer { buffer.deallocate(); stream.close() }
                while stream.hasBytesAvailable {
                    let read = stream.read(buffer, maxLength: 1024)
                    if read <= 0 { break }
                    data.append(buffer, count: read)
                }
                body = try? JSONDecoder().decode([String: String].self, from: data)
            } else if let data = request.httpBody {
                body = try? JSONDecoder().decode([String: String].self, from: data)
            }
            return self.respond(status, Self.challengeJSON)(request)
        }
        let client = makeClient()

        // Act
        let created = try await client.challengeCabal(groupId: "g1", opponentGroupId: "g2")
        status = 200
        let existing = try await client.challengeCabal(groupId: "g1", opponentGroupId: "g2")

        // Assert
        XCTAssertEqual(captured?.httpMethod, "POST")
        XCTAssertEqual(captured?.url?.path, "/v1/groups/g1/matchups/challenge")
        XCTAssertEqual(body, ["groupId": "g2"])
        XCTAssertEqual(created.status, .pending)
        XCTAssertEqual(existing.id, created.id)
    }

    func testChallengeCabal_conflictCarriesTheReason() async throws {
        // Arrange
        let refusal = Data(#"{"error":"that cabal already challenged yours; accept it instead","reason":"incoming_challenge","requestId":"r1"}"#.utf8)
        MockURLProtocol.requestHandler = respond(409, refusal)

        // Act
        var caught: Error?
        do {
            _ = try await makeClient().challengeCabal(groupId: "g1", opponentGroupId: "g2")
        } catch {
            caught = error
        }

        // Assert
        XCTAssertEqual(caught as? MatchupChallengeRefused, MatchupChallengeRefused(reason: .incomingChallenge))
        XCTAssertNil(MatchupChallengeRefusal(errorBody: Data(#"{"error":"nope"}"#.utf8)))
    }

    func testAcceptMatchupChallenge_postsToTheChallengedCabalsRoute() async throws {
        // Arrange
        var captured: URLRequest?
        MockURLProtocol.requestHandler = { request in
            captured = request
            return self.respond(200, Self.challengeJSON)(request)
        }

        // Act
        _ = try await makeClient().acceptMatchupChallenge(groupId: "g2", challengeId: "c1")

        // Assert
        XCTAssertEqual(captured?.httpMethod, "POST")
        XCTAssertEqual(captured?.url?.path, "/v1/groups/g2/matchups/challenges/c1/accept")
    }
}
