import XCTest
@testable import MonacoCore

final class DevBuyAPITests: XCTestCase {
    private let forbiddenHosts = ["jupiter", "xstocks"]

    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    func testDevBuy_postsToMonacoBackendOnly_notJupiterOrXStocks() async throws {
        // Arrange
        let token = TestFixtures.fixtureSessionToken
        let groupId = "550e8400-e29b-41d4-a716-446655440001"
        var capturedURL: URL?
        var capturedAuthorization: String?
        var capturedBody: Data?

        MockURLProtocol.requestHandler = { request in
            capturedURL = request.url
            capturedAuthorization = request.value(forHTTPHeaderField: "Authorization")
            capturedBody = Self.httpBody(from: request)
            let responseBody = """
            {
              "transactionId": "tx-1",
              "groupId": "\(groupId)",
              "symbol": "AAPLx",
              "status": "confirmed",
              "txSignature": "sig-abc",
              "created": true
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
        let result = try await client.devBuy(groupId: groupId, symbol: "AAPLx", usdc: 100_000)

        // Assert
        let url = try XCTUnwrap(capturedURL)
        XCTAssertEqual(url.host, "api.test")
        XCTAssertEqual(url.path, "/v1/dev/groups/\(groupId)/buy")
        for forbidden in forbiddenHosts {
            XCTAssertFalse(
                url.absoluteString.lowercased().contains(forbidden),
                "dev buy must not call \(forbidden) hosts"
            )
        }
        XCTAssertEqual(capturedAuthorization, "Bearer \(token)")

        let body = try XCTUnwrap(capturedBody)
        let json = try JSONSerialization.jsonObject(with: body) as? [String: Any]
        XCTAssertEqual(json?["symbol"] as? String, "AAPLx")
        XCTAssertEqual((json?["usdc"] as? NSNumber)?.int64Value, 100_000)

        XCTAssertEqual(result.transactionId, "tx-1")
        XCTAssertEqual(result.groupId, groupId)
        XCTAssertEqual(result.symbol, "AAPLx")
        XCTAssertEqual(result.status, "confirmed")
        XCTAssertEqual(result.txSignature, "sig-abc")
        XCTAssertTrue(result.created)
    }

    private func makeMockURLSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return URLSession(configuration: configuration)
    }

    private static func httpBody(from request: URLRequest) -> Data? {
        if let body = request.httpBody {
            return body
        }
        guard let stream = request.httpBodyStream else {
            return nil
        }
        stream.open()
        defer { stream.close() }
        var data = Data()
        let bufferSize = 1024
        let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: bufferSize)
        defer { buffer.deallocate() }
        while stream.hasBytesAvailable {
            let read = stream.read(buffer, maxLength: bufferSize)
            if read > 0 {
                data.append(buffer, count: read)
            }
        }
        return data.isEmpty ? nil : data
    }
}
