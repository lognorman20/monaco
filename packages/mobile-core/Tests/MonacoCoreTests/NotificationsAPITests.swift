import XCTest
@testable import MonacoCore

final class NotificationsAPITests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    private func client() -> MonacoAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: URLSession(configuration: configuration),
            accessTokenProvider: { "token-1" }
        )
    }

    private static func respond(_ request: URLRequest, status: Int, body: String, headers: [String: String] = [:]) -> (HTTPURLResponse, Data) {
        var fields = ["Content-Type": "application/json"]
        fields.merge(headers) { _, new in new }
        return (HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: fields)!, Data(body.utf8))
    }

    private static func bodyJSON(of request: URLRequest) -> [String: Any]? {
        var data = request.httpBody
        if data == nil, let stream = request.httpBodyStream {
            stream.open()
            defer { stream.close() }
            var collected = Data()
            var buffer = [UInt8](repeating: 0, count: 4096)
            while stream.hasBytesAvailable {
                let read = stream.read(&buffer, maxLength: buffer.count)
                if read <= 0 { break }
                collected.append(buffer, count: read)
            }
            data = collected
        }
        guard let data else { return nil }
        return try? JSONSerialization.jsonObject(with: data) as? [String: Any]
    }

    func testListNotifications_sendsCursorAndLimitWithTheToken() async throws {
        // Arrange
        var captured: URLRequest?
        MockURLProtocol.requestHandler = { request in
            captured = request
            return Self.respond(request, status: 200, body: #"{"notifications":[{"id":"n1","kind":"chat_message","category":"chat","title":"Priya in Sunday","body":"hi","groupId":"g1","groupName":"Sunday","groupPictureUrl":null,"proposalId":null,"transactionId":null,"symbol":null,"readAt":null,"createdAt":"2026-09-25T14:03:00Z"}],"unreadCount":4,"nextCursor":"c2"}"#)
        }

        // Act
        let page = try await client().listNotifications(cursor: "c1", limit: 20)

        // Assert
        let request = try XCTUnwrap(captured)
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.url?.path, "/v1/me/notifications")
        let query = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems
        XCTAssertEqual(query, [URLQueryItem(name: "limit", value: "20"), URLQueryItem(name: "cursor", value: "c1")])
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer token-1")
        XCTAssertEqual(page.unreadCount, 4)
        XCTAssertEqual(page.notifications.first?.groupName, "Sunday")
        XCTAssertEqual(page.nextCursor, "c2")
    }

    func testMarkRead_idsAndAll() async throws {
        var bodies: [[String: Any]] = []
        MockURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.url?.path, "/v1/me/notifications/read")
            XCTAssertEqual(request.httpMethod, "POST")
            bodies.append(Self.bodyJSON(of: request) ?? [:])
            return Self.respond(request, status: 200, body: #"{"unreadCount":2}"#)
        }

        let afterIDs = try await client().markNotificationsRead(ids: ["a", "b"])
        let afterAll = try await client().markAllNotificationsRead()

        XCTAssertEqual(afterIDs.unreadCount, 2)
        XCTAssertEqual(afterAll.unreadCount, 2)
        XCTAssertEqual(bodies[0]["ids"] as? [String], ["a", "b"])
        XCTAssertNil(bodies[0]["all"])
        XCTAssertEqual(bodies[1]["all"] as? Bool, true)
        XCTAssertNil(bodies[1]["ids"])
    }

    func testRegisterAndUnregisterDevice() async throws {
        var requests: [URLRequest] = []
        var bodies: [[String: Any]] = []
        MockURLProtocol.requestHandler = { request in
            requests.append(request)
            bodies.append(Self.bodyJSON(of: request) ?? [:])
            return Self.respond(request, status: 204, body: "")
        }

        try await client().registerDevice(token: "abcd", appEnv: .debug)
        try await client().unregisterDevice(token: "abcd")

        XCTAssertEqual(requests[0].httpMethod, "PUT")
        XCTAssertEqual(requests[0].url?.path, "/v1/me/devices")
        XCTAssertEqual(bodies[0]["token"] as? String, "abcd")
        XCTAssertEqual(bodies[0]["platform"] as? String, "ios")
        XCTAssertEqual(bodies[0]["appEnv"] as? String, "debug")
        XCTAssertEqual(requests[1].httpMethod, "DELETE")
        XCTAssertEqual(requests[1].url?.path, "/v1/me/devices/abcd")
    }

    func testNudge_decodesAndMapsTheRateLimit() async throws {
        MockURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.url?.path, "/v1/proposals/p1/nudge")
            return Self.respond(request, status: 200, body: #"{"reminded":2,"waitingOn":3}"#)
        }
        let result = try await client().nudgeProposal(id: "p1")
        XCTAssertEqual(result, NudgeResultDTO(reminded: 2, waitingOn: 3))

        MockURLProtocol.requestHandler = { request in
            Self.respond(request, status: 429, body: #"{"error":"the voters were reminded less than an hour ago"}"#, headers: ["Retry-After": "1800"])
        }
        do {
            _ = try await client().nudgeProposal(id: "p1")
            XCTFail("expected a rate limit")
        } catch let error as MonacoAPIError {
            XCTAssertEqual(error, .rateLimited(retryAfterSeconds: 1800))
            XCTAssertEqual(InboxCopy.remindError(status: error.statusCode), InboxCopy.remindTooSoon)
        }

        MockURLProtocol.requestHandler = { request in
            Self.respond(request, status: 403, body: #"{"error":"vote first to remind the others"}"#)
        }
        do {
            _ = try await client().nudgeProposal(id: "p1")
            XCTFail("expected a refusal")
        } catch let error as MonacoAPIError {
            XCTAssertEqual(error.statusCode, 403)
        }
    }

    func testRegisterDevice_serverRefusalThrows() async {
        MockURLProtocol.requestHandler = { request in
            Self.respond(request, status: 400, body: #"{"error":"invalid device token"}"#)
        }
        do {
            try await client().registerDevice(token: "x", appEnv: .production)
            XCTFail("expected a refusal")
        } catch {
            XCTAssertEqual((error as? MonacoAPIError)?.statusCode, 400)
        }
    }
}
