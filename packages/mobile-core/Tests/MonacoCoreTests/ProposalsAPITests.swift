import XCTest
@testable import MonacoCore

final class ProposalsAPITests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    func testAPIClient_searchAssets_usesGroupsAssetsQueryParam() async throws {
        // Arrange
        let token = TestFixtures.fixtureSessionToken
        var capturedPath: String?
        var capturedQuery: String?

        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
            if let url = request.url {
                capturedQuery = URLComponents(url: url, resolvingAgainstBaseURL: false)?.query
            }
            let body = """
            {"assets":[{"symbol":"AAPLx","name":"Apple xStock"}],"hasMore":false}
            """
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
            accessTokenProvider: { token }
        )

        // Act
        let result = try await client.searchAssets(groupId: "grp-1", query: "AAPL")

        // Assert
        XCTAssertEqual(capturedPath, "/v1/groups/grp-1/assets")
        XCTAssertTrue(capturedQuery?.contains("query=AAPL") == true)
        XCTAssertEqual(result.assets.count, 1)
        XCTAssertEqual(result.assets[0].symbol, "AAPLx")
        XCTAssertEqual(result.assets[0].name, "Apple xStock")
        XCTAssertFalse(result.hasMore)
    }

    func testAPIClient_postQuotes_sendsSymbolAndUsdc() async throws {
        // Arrange
        let token = TestFixtures.fixtureSessionToken
        var capturedPath: String?
        var capturedBody: Data?

        MockURLProtocol.requestHandler = { request in
            capturedPath = request.url?.path
            capturedBody = Self.httpBody(from: request)
            let body = """
            {"symbol":"AAPLx","usdcMicros":"5000000","routable":true}
            """
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
            accessTokenProvider: { token }
        )

        // Act
        let quote = try await client.postQuote(groupId: "grp-1", symbol: "AAPLx", usdc: 5_000_000)

        // Assert
        XCTAssertEqual(capturedPath, "/v1/groups/grp-1/quotes")
        let json = try XCTUnwrap(capturedBody.flatMap { try JSONSerialization.jsonObject(with: $0) as? [String: Any] })
        XCTAssertEqual(json["symbol"] as? String, "AAPLx")
        XCTAssertEqual(json["usdc"] as? Int64, 5_000_000)
        XCTAssertTrue(quote.routable)
    }

    func testAPIClient_postProposal_doesNotCallWhenRoutableFalse() {
        // Arrange
        let gate = ProposalSubmitGate()
        let quote = BuyQuoteDTO(symbol: "AAPLx", usdcMicros: "1000000", routable: false)

        // Act
        let maySubmit = gate.maySubmitProposal(quote: quote)

        // Assert
        XCTAssertFalse(maySubmit)
    }

    func testProductFeatures_noDirectXStocksJupiterPythOrSolanaRpcUrls() {
        // Arrange
        let featureSources = ProductFeatureSourceManifest.sampleFeatureSources

        // Act
        let clean = ProductBoundaryScanner.featureSourcesAreClean(featureSources)

        // Assert
        XCTAssertTrue(clean)
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
