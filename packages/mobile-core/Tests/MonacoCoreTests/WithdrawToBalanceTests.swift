import XCTest
@testable import MonacoCore

final class WithdrawToBalanceTests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    func testLeaveGroup_withWithdrawStake_sendsBody() async throws {
        let token = TestFixtures.fixtureSessionToken
        var capturedRequest: URLRequest?

        MockURLProtocol.requestHandler = { request in
            capturedRequest = request
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 204,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data())
        }

        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { token }
        )

        try await client.leaveGroup(groupId: "g1", withdrawStake: true)

        let request = try XCTUnwrap(capturedRequest)
        let body = try XCTUnwrap(Self.httpBody(from: request))
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Bool])
        XCTAssertEqual(json["withdrawStake"], true)
    }

    func testWithdrawToBalance_callsEndpoint() async throws {
        let token = TestFixtures.fixtureSessionToken
        var capturedPath: String?

        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
            let responseBody = """
            {"id":"job-1","status":"settled","shareUnits":500000,"sliceUsdc":500000,"payoutAddress":"FAKEwallet"}
            """
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, Data(responseBody.utf8))
        }

        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { token }
        )

        let job = try await client.withdrawToBalance(groupId: "g1")
        XCTAssertEqual(capturedPath, "/v1/groups/g1/withdraw-to-balance")
        XCTAssertEqual(job.status, "settled")
        XCTAssertEqual(job.sliceUsdc, 500_000)
    }

    private func makeMockURLSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return URLSession(configuration: configuration)
    }

    private static func httpBody(from request: URLRequest) -> Data? {
        if let body = request.httpBody { return body }
        guard let stream = request.httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: 1024)
        defer { buffer.deallocate() }
        while stream.hasBytesAvailable {
            let read = stream.read(buffer, maxLength: 1024)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return data
    }
}
