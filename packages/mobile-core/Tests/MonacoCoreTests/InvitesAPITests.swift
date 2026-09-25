import XCTest
@testable import MonacoCore

final class InvitesAPITests: XCTestCase {
    private let groupId = "5b1f0c9e-0005-4c55-9a51-000000000005"

    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    private func client(token: String? = TestFixtures.fixtureSessionToken) -> MonacoAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: URLSession(configuration: configuration),
            accessTokenProvider: { token }
        )
    }

    private func respond(
        status: Int,
        json: String = "",
        headers: [String: String] = ["Content-Type": "application/json"],
        capture: @escaping (URLRequest) -> Void = { _ in }
    ) {
        MockURLProtocol.requestHandler = { request in
            capture(request)
            let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: headers)!
            return (response, Data(json.utf8))
        }
    }

    private static func body(of request: URLRequest) -> Data? {
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

    // MARK: - The cabal's own code

    func testCurrentInvite_getsTheLiveCodeWithTheSession() async throws {
        // Arrange
        var captured: URLRequest?
        respond(status: 200, json: #"{"code":"K7QM4XPD","url":"https://trymonaco.xyz/join/K7QM4XPD"}"#) { captured = $0 }

        // Act
        let invite = try await client().currentInvite(groupId: groupId)

        // Assert
        XCTAssertEqual(invite, InviteDTO(code: "K7QM4XPD", url: "https://trymonaco.xyz/join/K7QM4XPD"))
        XCTAssertEqual(captured?.httpMethod, "GET")
        XCTAssertEqual(captured?.url?.path, "/v1/groups/\(groupId)/invites")
        XCTAssertEqual(captured?.value(forHTTPHeaderField: "Authorization"), "Bearer \(TestFixtures.fixtureSessionToken)")
    }

    func testCreateInvite_postsAndAcceptsCreated() async throws {
        // Arrange
        var captured: URLRequest?
        respond(status: 201, json: #"{"code":"R8WN3HQT","url":"https://trymonaco.xyz/join/R8WN3HQT"}"#) { captured = $0 }

        // Act
        let invite = try await client().createInvite(groupId: groupId)

        // Assert
        XCTAssertEqual(invite.code, "R8WN3HQT")
        XCTAssertEqual(captured?.httpMethod, "POST")
        XCTAssertEqual(captured?.url?.path, "/v1/groups/\(groupId)/invites")
    }

    func testRevokeInvite_expectsNoContent() async throws {
        // Arrange
        var captured: URLRequest?
        respond(status: 204) { captured = $0 }

        // Act
        try await client().revokeInvite(groupId: groupId)

        // Assert
        XCTAssertEqual(captured?.httpMethod, "POST")
        XCTAssertEqual(captured?.url?.path, "/v1/groups/\(groupId)/invites/revoke")
    }

    func testCurrentInvite_nonMemberIsA404TheCallerCanRead() async {
        // Arrange
        respond(status: 404, json: #"{"error":"group not found"}"#)

        // Act / Assert
        do {
            _ = try await client().currentInvite(groupId: groupId)
            XCTFail("expected a 404")
        } catch let error as MonacoAPIError {
            XCTAssertEqual(error.statusCode, 404)
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    func testCreateInvite_rateLimitKeepsRetryAfter() async {
        // Arrange
        respond(status: 429, json: #"{"error":"too many requests"}"#, headers: ["Retry-After": "7"])

        // Act / Assert
        do {
            _ = try await client().createInvite(groupId: groupId)
            XCTFail("expected a 429")
        } catch let error as MonacoAPIError {
            XCTAssertEqual(error, .rateLimited(retryAfterSeconds: 7))
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    // MARK: - Preview

    func testInvitePreview_isPublicAndCanonicalizesTheCode() async throws {
        // Arrange
        var captured: URLRequest?
        respond(status: 200, json: """
        {"code":"K7QM4XPD","groupId":"\(groupId)","name":"Sunday Investors","memberCount":9,
         "tint":"indigo","pictureUrl":null,"joinPolicy":"open","potValueUsd":"1240.50"}
        """) { captured = $0 }

        // Act
        let preview = try await client().invitePreview(code: "k7qm-4xpd")

        // Assert
        XCTAssertEqual(preview.name, "Sunday Investors")
        XCTAssertEqual(preview.joinPolicy, .open)
        XCTAssertEqual(captured?.url?.path, "/v1/invites/K7QM4XPD")
        XCTAssertNil(captured?.value(forHTTPHeaderField: "Authorization"), "the preview needs no session and is sent without one")
    }

    func testInvitePreview_malformedCodeNeverReachesTheNetwork() async {
        // Arrange
        var called = false
        respond(status: 200) { _ in called = true }

        // Act / Assert
        do {
            _ = try await client().invitePreview(code: "AB12CD34")
            XCTFail("expected a 404")
        } catch let error as MonacoAPIError {
            XCTAssertEqual(error.statusCode, 404)
        } catch {
            XCTFail("unexpected error \(error)")
        }
        XCTAssertFalse(called)
    }

    func testInvitePreview_deadCodeIsA404() async {
        // Arrange
        respond(status: 404, json: #"{"error":"invite not found"}"#)

        // Act / Assert
        do {
            _ = try await client().invitePreview(code: "K7QM4XPD")
            XCTFail("expected a 404")
        } catch let error as MonacoAPIError {
            XCTAssertEqual(error.statusCode, 404)
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    // MARK: - Join by code

    func testJoinByCode_joinedReadsTheCabalFromLocation() async throws {
        // Arrange
        var captured: URLRequest?
        respond(status: 204, headers: ["Location": "/v1/groups/\(groupId)"]) { captured = $0 }

        // Act
        let result = try await client().joinGroup(inviteCode: "K7QM4XPD")

        // Assert
        XCTAssertEqual(result, InviteJoinResult(status: .joined, groupId: groupId))
        XCTAssertEqual(captured?.httpMethod, "POST")
        XCTAssertEqual(captured?.url?.path, "/v1/groups/join-by-code")
        let body = try XCTUnwrap(captured.flatMap(Self.body(of:)))
        XCTAssertEqual(try JSONSerialization.jsonObject(with: body) as? [String: String], ["code": "K7QM4XPD"])
    }

    func testJoinByCode_pendingReadsTheCabalFromTheBody() async throws {
        // Arrange
        respond(status: 202, json: #"{"status":"pending","groupId":"\#(groupId)"}"#)

        // Act
        let result = try await client().joinGroup(inviteCode: "K7QM4XPD")

        // Assert
        XCTAssertEqual(result, InviteJoinResult(status: .pending, groupId: groupId))
    }

    func testJoinByCode_withoutLocationStillJoins() async throws {
        // Arrange
        respond(status: 204, headers: [:])

        // Act
        let result = try await client().joinGroup(inviteCode: "K7QM4XPD")

        // Assert
        XCTAssertEqual(result, InviteJoinResult(status: .joined, groupId: nil))
    }

    func testJoinByCode_failuresKeepTheirStatus() async {
        for status in [401, 403, 404, 500] {
            // Arrange
            respond(status: status, json: #"{"error":"no"}"#)

            // Act / Assert
            do {
                _ = try await client().joinGroup(inviteCode: "K7QM4XPD")
                XCTFail("expected \(status)")
            } catch let error as MonacoAPIError {
                XCTAssertEqual(error.statusCode, status)
            } catch {
                XCTFail("unexpected error \(error)")
            }
        }
    }

    func testGroupIdFromLocation() {
        XCTAssertEqual(MonacoAPIClient.groupId(fromLocation: "/v1/groups/\(groupId)"), groupId)
        XCTAssertNil(MonacoAPIClient.groupId(fromLocation: "/v1/other/\(groupId)"))
        XCTAssertNil(MonacoAPIClient.groupId(fromLocation: ""))
    }
}
