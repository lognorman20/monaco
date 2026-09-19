import XCTest
@testable import MonacoCore

final class ProposalCommentsAPITests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    private func makeClient() -> MonacoAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        let token = TestFixtures.fixtureSessionToken
        return MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: URLSession(configuration: configuration),
            accessTokenProvider: { token }
        )
    }

    private static func respond(_ request: URLRequest, status: Int, body: String) -> (HTTPURLResponse, Data) {
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
        return (response, Data(body.utf8))
    }

    private static func httpBody(from request: URLRequest) -> Data? {
        if let body = request.httpBody { return body }
        guard let stream = request.httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 1024)
        while stream.hasBytesAvailable {
            let read = stream.read(&buffer, maxLength: buffer.count)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return data
    }

    func testPostProposalComment_reply_sendsBodyParentAndAuth() async throws {
        // Arrange
        var capturedPath: String?
        var capturedAuth: String?
        var capturedJSON: [String: Any]?
        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
            capturedAuth = request.value(forHTTPHeaderField: "Authorization")
            capturedJSON = Self.httpBody(from: request).flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
            return Self.respond(request, status: 201, body: """
            {"id":"c2","proposalId":"prop-1","parentId":"c1","authorId":"u1","authorName":"Ada","body":"Lower drawdown.","createdAt":"2026-09-18T01:05:00Z"}
            """)
        }

        // Act
        let created = try await makeClient().postProposalComment(proposalId: "prop-1", body: "Lower drawdown.", parentId: "c1")

        // Assert
        XCTAssertEqual(capturedPath, "/v1/proposals/prop-1/comments")
        XCTAssertEqual(capturedAuth, "Bearer \(TestFixtures.fixtureSessionToken)")
        XCTAssertEqual(capturedJSON?["body"] as? String, "Lower drawdown.")
        XCTAssertEqual(capturedJSON?["parentId"] as? String, "c1")
        XCTAssertEqual(created.parentId, "c1")
    }

    func testPostProposalComment_topLevel_omitsParentId() async throws {
        // Arrange
        var capturedJSON: [String: Any]?
        MockURLProtocol.requestHandler = { request in
            capturedJSON = Self.httpBody(from: request).flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
            return Self.respond(request, status: 201, body: """
            {"id":"c1","proposalId":"prop-1","authorId":"u1","authorName":"Ben","body":"Why Apple?","createdAt":"2026-09-18T01:00:00Z"}
            """)
        }

        // Act
        _ = try await makeClient().postProposalComment(proposalId: "prop-1", body: "Why Apple?")

        // Assert
        XCTAssertNotNil(capturedJSON)
        XCTAssertNil(capturedJSON?["parentId"])
    }

    func testPostProposalComment_serverRejects_throwsHTTPStatus() async {
        // Arrange
        for status in [400, 404, 429] {
            MockURLProtocol.requestHandler = { request in
                Self.respond(request, status: status, body: #"{"error":"nope"}"#)
            }

            // Act / Assert
            do {
                _ = try await makeClient().postProposalComment(proposalId: "prop-1", body: "x")
                XCTFail("expected throw for \(status)")
            } catch {
                XCTAssertEqual(error as? MonacoAPIError, .httpStatus(status))
            }
        }
    }

    func testListProposalComments_decodesThread() async throws {
        // Arrange
        MockURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/v1/proposals/prop-1/comments")
            return Self.respond(request, status: 200, body: #"{"comments":[]}"#)
        }

        // Act
        let response = try await makeClient().listProposalComments(proposalId: "prop-1")

        // Assert
        XCTAssertEqual(response.comments, [])
    }

    func testListProposalComments_malformedPayload_throwsDecodingError() async {
        // Arrange
        MockURLProtocol.requestHandler = { request in
            Self.respond(request, status: 200, body: #"{"comments":[{"id":1}]}"#)
        }

        // Act / Assert
        do {
            _ = try await makeClient().listProposalComments(proposalId: "prop-1")
            XCTFail("expected decoding error")
        } catch {
            XCTAssertTrue(error is DecodingError)
        }
    }

    func testListGroupProposals_sendsTabQuery() async throws {
        // Arrange
        var capturedQuery: String?
        MockURLProtocol.requestHandler = { request in
            capturedQuery = request.url.flatMap { URLComponents(url: $0, resolvingAgainstBaseURL: false)?.query }
            return Self.respond(request, status: 200, body: #"{"proposals":[]}"#)
        }

        // Act
        let response = try await makeClient().listGroupProposals(groupId: "grp-1", tab: .closed)

        // Assert
        XCTAssertEqual(capturedQuery, "tab=closed")
        XCTAssertEqual(response.proposals, [])
    }

    func testGetProposalDetail_notFound_throwsHTTPStatus() async {
        // Arrange
        MockURLProtocol.requestHandler = { request in
            Self.respond(request, status: 404, body: #"{"error":"proposal not found"}"#)
        }

        // Act / Assert
        do {
            _ = try await makeClient().getProposalDetail(proposalId: "missing")
            XCTFail("expected throw")
        } catch {
            XCTAssertEqual(error as? MonacoAPIError, .httpStatus(404))
        }
    }
}
