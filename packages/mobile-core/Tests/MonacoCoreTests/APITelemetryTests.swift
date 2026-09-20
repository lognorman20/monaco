import XCTest
@testable import MonacoCore

final class APITelemetryTests: XCTestCase {
    private let baseURL = URL(string: "https://api.test")!
    private let groupID = "550e8400-e29b-41d4-a716-446655440000"
    private let token = "secret-bearer-token-value"

    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        APITelemetryRegistry.shared.register(nil)
        super.tearDown()
    }

    // MARK: Request id

    func testEveryRequestCarriesAFreshUUIDRequestID() async throws {
        let sent = Recorder<String?>()
        MockURLProtocol.requestHandler = { [self] request in
            sent.append(request.value(forHTTPHeaderField: "X-Request-Id"))
            return respond(request, status: 200, body: #"{"groups":[]}"#)
        }
        let (client, events) = makeClient()

        _ = try await client.groupLeaderboard()
        _ = try await client.groupLeaderboard()

        let ids = sent.values.compactMap { $0 }
        XCTAssertEqual(ids.count, 2)
        XCTAssertNotEqual(ids[0], ids[1])
        for id in ids {
            XCTAssertNotNil(UUID(uuidString: id), "\(id) is not a UUID")
        }
        XCTAssertEqual(events.values.map(\.requestID), ids)
    }

    func testTokenRefreshRetryKeepsTheRequestIDAndReportsOnce() async throws {
        let sent = Recorder<String?>()
        MockURLProtocol.requestHandler = { [self] request in
            sent.append(request.value(forHTTPHeaderField: "X-Request-Id"))
            let fresh = request.value(forHTTPHeaderField: "Authorization") == "Bearer fresh"
            return respond(request, status: fresh ? 200 : 401)
        }
        let events = Recorder<APIRequestEvent>()
        let transport = MonacoHTTPTransport(
            session: makeMockURLSession(),
            refresher: { _ in "fresh" },
            telemetry: RecordingTelemetry(events: events)
        )
        var request = URLRequest(url: baseURL.appending(path: "v1/me"))
        request.setValue("Bearer stale", forHTTPHeaderField: "Authorization")

        _ = try await transport.data(for: request)

        XCTAssertEqual(sent.values.count, 2)
        XCTAssertEqual(sent.values[0], sent.values[1])
        XCTAssertEqual(events.values.map(\.outcome), [.status(200)])
    }

    // MARK: Observer fields

    func testSuccessReportsTemplateStatusDurationAndEchoedID() async throws {
        MockURLProtocol.requestHandler = { [self] request in
            respond(request, status: 200, body: #"{"groupBalance":1,"platformBalance":2}"#, echoRequestID: true)
        }
        let (client, events) = makeClient()

        _ = try? await client.fundGroup(groupId: groupID, amount: 5_000_000)

        let event = try XCTUnwrap(events.values.first)
        XCTAssertEqual(events.values.count, 1)
        XCTAssertEqual(event.method, "POST")
        XCTAssertEqual(event.route, "/v1/groups/{id}/fund")
        XCTAssertEqual(event.outcome, .status(200))
        XCTAssertGreaterThan(event.durationMs, 0)
        XCTAssertEqual(event.serverRequestID, event.requestID)
        XCTAssertFalse(event.isFailure)
    }

    func testClientAndServerErrorsReportTheirStatus() async throws {
        for status in [404, 500] {
            MockURLProtocol.requestHandler = { [self] request in
                respond(request, status: status)
            }
            let (client, events) = makeClient()

            do {
                _ = try await client.getGroupView(groupId: groupID)
                XCTFail("Expected \(status) to throw")
            } catch {
                XCTAssertEqual(error as? MonacoAPIError, .httpStatus(status))
            }

            let event = try XCTUnwrap(events.values.first)
            XCTAssertEqual(events.values.count, 1)
            XCTAssertEqual(event.method, "GET")
            XCTAssertEqual(event.route, "/v1/groups/{id}/view")
            XCTAssertEqual(event.outcome, .status(status))
            XCTAssertGreaterThan(event.durationMs, 0)
            XCTAssertNil(event.serverRequestID)
            XCTAssertTrue(event.isFailure)
        }
    }

    func testMalformedJSONStillReportsTheServerStatus() async throws {
        MockURLProtocol.requestHandler = { [self] request in
            respond(request, status: 200, body: "<html>not json</html>")
        }
        let (client, events) = makeClient()

        do {
            _ = try await client.me()
            XCTFail("Expected decoding to throw")
        } catch {
            XCTAssertTrue(error is DecodingError)
        }

        let event = try XCTUnwrap(events.values.first)
        XCTAssertEqual(events.values.count, 1)
        XCTAssertEqual(event.route, "/v1/me")
        XCTAssertEqual(event.outcome, .status(200))
        XCTAssertGreaterThan(event.durationMs, 0)
    }

    func testTransportErrorsReportTheirCategory() async throws {
        let cases: [(URLError.Code, APITransportErrorCategory)] = [
            (.timedOut, .timeout),
            (.notConnectedToInternet, .offline),
            (.networkConnectionLost, .offline),
            (.serverCertificateUntrusted, .tls),
            (.cannotFindHost, .other),
        ]
        for (code, category) in cases {
            MockURLProtocol.requestHandler = { _ in throw URLError(code) }
            let (client, events) = makeClient()

            do {
                _ = try await client.getProposalDetail(proposalId: "42")
                XCTFail("Expected \(code) to throw")
            } catch {
                XCTAssertEqual((error as? URLError)?.code, code)
            }

            let event = try XCTUnwrap(events.values.first)
            XCTAssertEqual(events.values.count, 1)
            XCTAssertEqual(event.route, "/v1/proposals/{id}")
            XCTAssertEqual(event.outcome, .transportError(category))
            XCTAssertGreaterThan(event.durationMs, 0)
            XCTAssertNil(event.statusCode)
            XCTAssertTrue(event.isFailure)
        }
    }

    func testCancelledTaskReportsCancelledAndIsNotAFailure() async throws {
        MockURLProtocol.requestHandler = { [self] request in
            respond(request, status: 200)
        }
        let (client, events) = makeClient()

        let task = Task {
            withUnsafeCurrentTask { $0?.cancel() }
            _ = try await client.me()
        }
        let result = await task.result

        guard case .failure = result else {
            return XCTFail("Expected the cancelled request to throw")
        }
        let event = try XCTUnwrap(events.values.first)
        XCTAssertEqual(events.values.count, 1)
        XCTAssertEqual(event.outcome, .transportError(.cancelled))
        XCTAssertFalse(event.isFailure)
    }

    func testCategoryFromNonURLErrors() {
        XCTAssertEqual(APITransportErrorCategory(CancellationError()), .cancelled)
        XCTAssertEqual(APITransportErrorCategory(MonacoAPIError.invalidResponse), .other)
    }

    func testRegistryTelemetryIsUsedWhenNoneIsInjected() async throws {
        let events = Recorder<APIRequestEvent>()
        APITelemetryRegistry.shared.register(RecordingTelemetry(events: events))
        MockURLProtocol.requestHandler = { [self] request in
            respond(request, status: 200, body: #"{"groups":[]}"#)
        }
        let client = MonacoAPIClient(baseURL: baseURL, session: makeMockURLSession())

        _ = try await client.groupLeaderboard()

        XCTAssertEqual(events.values.map(\.route), ["/v1/groups/leaderboard"])
    }

    // MARK: Nothing sensitive is recorded

    func testNoTokenQueryValueOrPathIDReachesTheObserver() async throws {
        let queryValue = "whales-only-search"
        let cursor = "cursor-9f8e7d"
        MockURLProtocol.requestHandler = { [self] request in
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer \(token)")
            return respond(request, status: 500, body: #"{"error":"boom \#(queryValue)"}"#)
        }
        let (client, events) = makeClient()

        _ = try? await client.searchGroups(query: queryValue, cursor: cursor)
        _ = try? await client.searchAssets(groupId: groupID, query: queryValue)
        _ = try? await client.fundGroup(groupId: groupID, amount: 123_456_789)

        XCTAssertEqual(events.values.count, 3)
        for event in events.values {
            let recorded = String(describing: event)
            for secret in [token, queryValue, cursor, groupID, "?"] {
                XCTAssertFalse(recorded.contains(secret), "\(secret) leaked into \(recorded)")
            }
        }
    }

    func testUntemplatedRequestsAreRedactedByTheTransport() async throws {
        let wallet = "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
        MockURLProtocol.requestHandler = { [self] request in
            respond(request, status: 200)
        }
        let events = Recorder<APIRequestEvent>()
        let transport = MonacoHTTPTransport(
            session: makeMockURLSession(),
            telemetry: RecordingTelemetry(events: events)
        )
        var components = URLComponents(
            url: baseURL.appending(path: "v1/groups/\(groupID)/members/\(wallet)"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [URLQueryItem(name: "agentKey", value: "sk-agent-123")]
        var request = URLRequest(url: components.url!)
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")

        _ = try await transport.data(for: request)

        let event = try XCTUnwrap(events.values.first)
        XCTAssertEqual(event.route, "/v1/groups/{id}/members/{id}")
        let recorded = String(describing: event)
        for secret in [token, wallet, groupID, "sk-agent-123", "agentKey"] {
            XCTAssertFalse(recorded.contains(secret), "\(secret) leaked into \(recorded)")
        }
    }

    func testRouteTemplateRedaction() {
        XCTAssertEqual(APIRouteTemplate.redacting(path: "/v1/me"), "/v1/me")
        XCTAssertEqual(APIRouteTemplate.redacting(path: "/health"), "/health")
        XCTAssertEqual(APIRouteTemplate.redacting(path: ""), "/")
        XCTAssertEqual(
            APIRouteTemplate.redacting(path: "/v1/groups/\(groupID)/withdraw-to-balance"),
            "/v1/groups/{id}/withdraw-to-balance"
        )
        XCTAssertEqual(APIRouteTemplate.redacting(path: "/v1/deposits/981234"), "/v1/deposits/{id}")
        XCTAssertEqual(APIRouteTemplate.redacting(path: "/v1/assets/SOL/chart"), "/v1/assets/{id}/chart")
        XCTAssertEqual(APIRouteTemplate.redacting(path: "/v1/users/did:privy:cm3x9/groups"), "/v1/users/{id}/groups")
        XCTAssertEqual(
            APIRouteTemplate.redacting(path: "/v1/groups/abcdefab-abcd-abcd-abcd-abcdefabcdef"),
            "/v1/groups/{id}"
        )
    }

    // MARK: Request id on errors

    func testHTTPFailureCarriesTheRequestIDThatWasSent() async throws {
        let sent = Recorder<String?>()
        MockURLProtocol.requestHandler = { [self] request in
            sent.append(request.value(forHTTPHeaderField: "X-Request-Id"))
            return respond(request, status: 503)
        }
        let (client, _) = makeClient()

        do {
            _ = try await client.platformBalance()
            XCTFail("Expected 503 to throw")
        } catch {
            let id = try XCTUnwrap(sent.values.first ?? nil)
            XCTAssertEqual((error as? MonacoAPIError)?.requestID, id)
            XCTAssertEqual(error.apiRequestID, id)
            XCTAssertEqual(error.apiSupportReference, "ref: \(id.prefix(8))")
        }
    }

    func testRejectedAndRateLimitedCarryTheRequestID() async throws {
        for (status, body) in [(400, #"{"error":"name is taken"}"#), (429, "{}")] {
            let sent = Recorder<String?>()
            MockURLProtocol.requestHandler = { [self] request in
                sent.append(request.value(forHTTPHeaderField: "X-Request-Id"))
                return respond(request, status: status, body: body)
            }
            let (client, _) = makeClient()

            do {
                _ = try await client.updateProfile(displayName: "Ada")
                XCTFail("Expected \(status) to throw")
            } catch {
                XCTAssertNotNil(error.apiRequestID)
                XCTAssertEqual(error.apiRequestID, sent.values.first ?? nil)
            }
        }
    }

    func testConnectionFailureStaysAURLErrorAndCarriesTheRequestID() async throws {
        MockURLProtocol.requestHandler = { _ in throw URLError(.timedOut) }
        let (client, events) = makeClient()

        do {
            _ = try await client.me()
            XCTFail("Expected the timeout to throw")
        } catch {
            XCTAssertEqual((error as? URLError)?.code, .timedOut)
            XCTAssertEqual(error.apiRequestID, events.values.first?.requestID)
            XCTAssertNotNil(error.apiSupportReference)
        }
    }

    func testRequestIDDoesNotAffectErrorEquality() {
        XCTAssertEqual(MonacoAPIError.httpStatus(500, requestID: "a"), .httpStatus(500))
        XCTAssertNotEqual(MonacoAPIError.httpStatus(500, requestID: "a"), .httpStatus(502, requestID: "a"))
        XCTAssertNil(MonacoAPIError.invalidResponse.requestID)
        XCTAssertNil(CancellationError().apiSupportReference)
    }

    // MARK: Helpers

    private struct RecordingTelemetry: APITelemetry {
        let events: Recorder<APIRequestEvent>

        func record(_ event: APIRequestEvent) {
            events.append(event)
        }
    }

    private func makeClient() -> (MonacoAPIClient, Recorder<APIRequestEvent>) {
        let events = Recorder<APIRequestEvent>()
        let token = token
        let client = MonacoAPIClient(
            baseURL: baseURL,
            session: makeMockURLSession(),
            accessTokenProvider: { token },
            telemetry: RecordingTelemetry(events: events)
        )
        return (client, events)
    }

    private func respond(
        _ request: URLRequest,
        status: Int,
        body: String = "{}",
        echoRequestID: Bool = false
    ) -> (HTTPURLResponse, Data) {
        var headers: [String: String] = [:]
        if echoRequestID, let id = request.value(forHTTPHeaderField: "X-Request-Id") {
            headers["X-Request-Id"] = id
        }
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: headers)!
        return (response, Data(body.utf8))
    }

    private func makeMockURLSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return URLSession(configuration: configuration)
    }
}
