import XCTest
@testable import MonacoCore

final class RedeemTests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    func testRedeemSlider_dustMinimum_disablesSubmitBelowThreshold() {
        // Arrange
        let gate = RedeemSliderGate()
        let below = Int64(50_000)
        let atMinimum = RedeemDustMinimum.usdcMicros

        // Act
        let belowOk = gate.maySubmit(selectedMicros: below)
        let atOk = gate.maySubmit(selectedMicros: atMinimum)

        // Assert
        XCTAssertFalse(belowOk)
        XCTAssertTrue(atOk)
    }

    func testAPIClient_postRedeem_includesPayoutProofPayload() async throws {
        // Arrange
        let token = TestFixtures.fixtureSessionToken
        var capturedBody: Data?

        MockURLProtocol.requestHandler = { request in
            capturedBody = Self.httpBody(from: request)
            let body = """
            {"id":"job-1","status":"debited"}
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
        _ = try await client.postRedeem(
            groupId: "grp-1",
            shareUnits: "500000",
            payoutAddress: "PayoutAddr1111111111111111111111111111",
            payoutProof: "signed-proof-base64",
            submission: IdempotentSubmission()
        )

        // Assert
        let json = try JSONSerialization.jsonObject(with: XCTUnwrap(capturedBody)) as? [String: Any]
        XCTAssertEqual(json?["payoutProof"] as? String, "signed-proof-base64")
        XCTAssertEqual(json?["payoutAddress"] as? String, "PayoutAddr1111111111111111111111111111")
    }

    func testAPIClient_afterRedeemSuccess_refreshesGroupAndHome() async throws {
        // Arrange
        let token = TestFixtures.fixtureSessionToken
        var paths: [String] = []

        MockURLProtocol.requestHandler = { request in
            paths.append(request.url?.path ?? "")
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: ["Content-Type": "application/json"]
            )!
            if request.url?.path == "/v1/home" {
                return (response, Data("{\"groups\":[],\"people\":[]}".utf8))
            }
            if request.url?.path.contains("/view") == true {
                return (response, Data("""
                {"id":"g1","name":"Club","pot":[],"you":{"shareUnits":"0","equityUsd":"0","slicePercent":"0","dollarPnl":"0","percentReturn":null},"members":[]}
                """.utf8))
            }
            return (response, Data("{\"id\":\"job-1\",\"status\":\"debited\"}".utf8))
        }

        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { token }
        )
        let plan = RedeemBoardRefreshPlan.afterSuccess()

        // Act
        _ = try await client.postRedeem(
            groupId: "g1",
            shareUnits: "1000000",
            payoutAddress: "addr",
            payoutProof: "proof",
            submission: IdempotentSubmission()
        )
        if plan.refreshGroup {
            _ = try await client.getGroupView(groupId: "g1")
        }
        if plan.refreshHome {
            _ = try await client.getHome()
        }

        // Assert
        XCTAssertTrue(paths.contains("/v1/groups/g1/redeems"))
        XCTAssertTrue(paths.contains { $0.hasSuffix("/view") })
        XCTAssertTrue(paths.contains("/v1/home"))
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
            if read > 0 { data.append(buffer, count: read) }
        }
        return data.isEmpty ? nil : data
    }
}
