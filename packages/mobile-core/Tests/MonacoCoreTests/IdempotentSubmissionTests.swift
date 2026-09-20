import XCTest
@testable import MonacoCore

final class IdempotentSubmissionTests: XCTestCase {
    private let fundResponse = #"{"depositId":"dep-1","groupId":"g1","amount":5000000,"status":"pending","fromAddress":"Wallet111"}"#
    private let withdrawalResponse = #"{"withdrawalId":"w-1","amount":5000000,"toAddress":"Dest111","status":"pending","createdAt":"2026-01-01T00:00:00Z"}"#

    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    // MARK: - Header on every money POST

    func testEveryMoneyPostSendsIdempotencyKey() async throws {
        let recorder = RequestRecorder()
        MockURLProtocol.requestHandler = { request in
            recorder.record(request)
            let path = request.url?.path ?? ""
            if path.hasSuffix("/leave") {
                return (Self.response(for: request, status: 204), Data())
            }
            let body: String
            if path.hasSuffix("/fund") {
                body = self.fundResponse
            } else if path.hasSuffix("/withdrawals") {
                body = self.withdrawalResponse
            } else if path.hasSuffix("/proposals") {
                body = #"{"proposalId":"prop-1"}"#
            } else {
                body = #"{"id":"job-1","status":"settled","shareUnits":1,"sliceUsdc":1,"payoutAddress":"Dest111"}"#
            }
            return (Self.response(for: request, status: 200), Data(body.utf8))
        }
        let client = makeClient()

        _ = try await client.fundGroup(groupId: "g1", amount: 5_000_000, submission: IdempotentSubmission())
        _ = try await client.createPlatformWithdrawal(amount: 5_000_000, toAddress: "Dest111", submission: IdempotentSubmission())
        _ = try await client.withdrawToBalance(groupId: "g1", shareAmountMicros: 10, submission: IdempotentSubmission())
        _ = try await client.createProposal(groupId: "g1", symbol: "AAPLx", usdc: 5_000_000, submission: IdempotentSubmission())
        try await client.leaveGroup(groupId: "g1", withdrawStake: true, submission: IdempotentSubmission())
        _ = try await client.postRedeem(
            groupId: "g1", shareUnits: "1", payoutAddress: "Dest111", payoutProof: "proof", submission: IdempotentSubmission()
        )

        let keys = recorder.requests.map { $0.value(forHTTPHeaderField: IdempotentSubmission.keyHeader) }
        XCTAssertEqual(keys.count, 6)
        for (request, key) in zip(recorder.requests, keys) {
            let key = try XCTUnwrap(key, "no Idempotency-Key on \(request.url?.path ?? "")")
            XCTAssertNotNil(UUID(uuidString: key), "\(key) is not a UUID")
        }
        XCTAssertEqual(Set(keys.compactMap { $0 }).count, 6, "separate submissions must not share a key")
    }

    // MARK: - Retry of the same submission

    func testRetryAfterLostResponseReusesKey() async throws {
        let recorder = RequestRecorder()
        MockURLProtocol.requestHandler = { request in
            recorder.record(request)
            if recorder.requests.count == 1 {
                throw URLError(.timedOut)
            }
            return (Self.response(for: request, status: 200), Data(self.fundResponse.utf8))
        }
        let client = makeClient()
        let submission = IdempotentSubmission()

        do {
            _ = try await client.fundGroup(groupId: "g1", amount: 5_000_000, submission: submission)
            XCTFail("first attempt must time out")
        } catch let error as URLError {
            XCTAssertEqual(error.code, .timedOut)
        }
        _ = try await client.fundGroup(groupId: "g1", amount: 5_000_000, submission: submission)

        XCTAssertEqual(recorder.idempotencyKeys.count, 2)
        XCTAssertEqual(recorder.idempotencyKeys[0], recorder.idempotencyKeys[1])
        // The request id names one attempt, the idempotency key names the submission.
        let requestIDs = recorder.requests.map { $0.value(forHTTPHeaderField: monacoRequestIDHeader) }
        XCTAssertNotNil(requestIDs[0])
        XCTAssertNotNil(requestIDs[1])
        XCTAssertNotEqual(requestIDs[0], requestIDs[1])
    }

    func testRetryAfterServerErrorReusesKey() async throws {
        let recorder = RequestRecorder()
        MockURLProtocol.requestHandler = { request in
            recorder.record(request)
            let status = recorder.requests.count == 1 ? 502 : 200
            return (Self.response(for: request, status: status), Data(self.withdrawalResponse.utf8))
        }
        let client = makeClient()
        let submission = IdempotentSubmission()

        do {
            _ = try await client.createPlatformWithdrawal(amount: 5_000_000, toAddress: "Dest111", submission: submission)
            XCTFail("first attempt must fail")
        } catch {
            XCTAssertEqual(error as? MonacoAPIError, .httpStatus(502))
        }
        _ = try await client.createPlatformWithdrawal(amount: 5_000_000, toAddress: "Dest111", submission: submission)

        XCTAssertEqual(recorder.idempotencyKeys[0], recorder.idempotencyKeys[1])
    }

    func testRetryWhileFirstAttemptStillRunningReusesKey() async throws {
        let recorder = RequestRecorder()
        MockURLProtocol.requestHandler = { request in
            recorder.record(request)
            if recorder.requests.count == 1 {
                let headers = [IdempotentSubmission.statusHeader: IdempotentSubmission.inProgressStatus]
                return (Self.response(for: request, status: 409, headers: headers), Data())
            }
            return (Self.response(for: request, status: 200), Data(self.fundResponse.utf8))
        }
        let client = makeClient()
        let submission = IdempotentSubmission()

        _ = try? await client.fundGroup(groupId: "g1", amount: 5_000_000, submission: submission)
        _ = try await client.fundGroup(groupId: "g1", amount: 5_000_000, submission: submission)

        XCTAssertEqual(recorder.idempotencyKeys[0], recorder.idempotencyKeys[1])
    }

    func testTokenRefreshRetryKeepsKey() async throws {
        let recorder = RequestRecorder()
        MockURLProtocol.requestHandler = { request in
            recorder.record(request)
            let status = recorder.requests.count == 1 ? 401 : 200
            return (Self.response(for: request, status: status), Data(self.fundResponse.utf8))
        }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        let transport = MonacoHTTPTransport(session: URLSession(configuration: configuration), refresher: { _ in "fresh-token" })
        let client = MonacoAPIClient(baseURL: URL(string: "https://api.test")!, transport: transport, accessTokenProvider: { "stale-token" })

        _ = try await client.fundGroup(groupId: "g1", amount: 5_000_000, submission: IdempotentSubmission())

        XCTAssertEqual(recorder.requests.count, 2)
        XCTAssertEqual(recorder.requests[1].value(forHTTPHeaderField: "Authorization"), "Bearer fresh-token")
        XCTAssertEqual(recorder.idempotencyKeys[0], recorder.idempotencyKeys[1])
    }

    /// The app target's client calls the transport directly; it must get both headers too.
    func testTransportDataForSubmissionSendsRequestIDAndReusedKey() async throws {
        let recorder = RequestRecorder()
        MockURLProtocol.requestHandler = { request in
            recorder.record(request)
            let status = recorder.requests.count == 1 ? 503 : 200
            return (Self.response(for: request, status: status), Data())
        }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        let transport = MonacoHTTPTransport(session: URLSession(configuration: configuration))
        let submission = IdempotentSubmission()
        let request = Self.request(path: "/v1/transactions/t1/retry", body: "")

        _ = try await transport.data(for: request, submission: submission)
        _ = try await transport.data(for: request, submission: submission)

        XCTAssertNotNil(recorder.idempotencyKeys[0])
        XCTAssertEqual(recorder.idempotencyKeys[0], recorder.idempotencyKeys[1])
        let requestIDs = recorder.requests.map { $0.value(forHTTPHeaderField: monacoRequestIDHeader) }
        XCTAssertNotNil(requestIDs[0])
        XCTAssertNotEqual(requestIDs[0], requestIDs[1])
    }

    // MARK: - New submission

    func testNextSubmissionAfterSuccessGetsNewKey() async throws {
        let recorder = RequestRecorder()
        MockURLProtocol.requestHandler = { request in
            recorder.record(request)
            return (Self.response(for: request, status: 200), Data(self.fundResponse.utf8))
        }
        let client = makeClient()
        let submission = IdempotentSubmission()

        _ = try await client.fundGroup(groupId: "g1", amount: 5_000_000, submission: submission)
        _ = try await client.fundGroup(groupId: "g1", amount: 5_000_000, submission: submission)

        XCTAssertNotEqual(recorder.idempotencyKeys[0], recorder.idempotencyKeys[1])
    }

    func testNextSubmissionAfterRejectionGetsNewKey() async throws {
        let recorder = RequestRecorder()
        MockURLProtocol.requestHandler = { request in
            recorder.record(request)
            // A business 409 (a withdrawal is already pending) is a final answer.
            return (Self.response(for: request, status: 409), Data(#"{"error":"a platform withdrawal is already in progress"}"#.utf8))
        }
        let client = makeClient()
        let submission = IdempotentSubmission()

        _ = try? await client.createPlatformWithdrawal(amount: 5_000_000, toAddress: "Dest111", submission: submission)
        _ = try? await client.createPlatformWithdrawal(amount: 5_000_000, toAddress: "Dest111", submission: submission)

        XCTAssertNotEqual(recorder.idempotencyKeys[0], recorder.idempotencyKeys[1])
    }

    /// A leave the server refuses is a final answer, so the next attempt must reach the handler
    /// rather than replay the stored 409. The member is told to fix something first ("cash out
    /// your slice", "vote on the open proposals") and then taps Leave again with a byte-identical
    /// body: if that retried under the same key, the backend would replay the refusal for the
    /// full 24 h key TTL and the member could never leave.
    func testBlockedLeaveGetsNewKeyOnTheNextAttempt() async throws {
        let recorder = RequestRecorder()
        MockURLProtocol.requestHandler = { request in
            recorder.record(request)
            if recorder.requests.count == 1 {
                // What the handler returns for a blocked leave: a 409 carrying a reason and no
                // `Idempotency-Status`, which marks it as the request's own answer.
                return (Self.response(for: request, status: 409), Data(#"{"error":"cash out your slice first","reason":"share_units_remaining"}"#.utf8))
            }
            return (Self.response(for: request, status: 204), Data())
        }
        let client = makeClient()
        let submission = IdempotentSubmission()

        try? await client.leaveGroup(groupId: "g1", withdrawStake: false, submission: submission)
        try await client.leaveGroup(groupId: "g1", withdrawStake: false, submission: submission)

        XCTAssertEqual(recorder.idempotencyKeys.count, 2)
        XCTAssertNotEqual(
            recorder.idempotencyKeys[0], recorder.idempotencyKeys[1],
            "a refused leave is answered; the retry must be a new submission, not a replay"
        )
    }

    /// The other half: a 409 that only means "your first attempt is still running" is not an
    /// answer, so the retry has to stay under the same key or it would start a second leave.
    func testLeaveStillRunningRetriesUnderTheSameKey() async throws {
        let recorder = RequestRecorder()
        MockURLProtocol.requestHandler = { request in
            recorder.record(request)
            if recorder.requests.count == 1 {
                let headers = [IdempotentSubmission.statusHeader: IdempotentSubmission.inProgressStatus]
                return (Self.response(for: request, status: 409, headers: headers), Data())
            }
            return (Self.response(for: request, status: 204), Data())
        }
        let client = makeClient()
        let submission = IdempotentSubmission()

        try? await client.leaveGroup(groupId: "g1", withdrawStake: true, submission: submission)
        try await client.leaveGroup(groupId: "g1", withdrawStake: true, submission: submission)

        XCTAssertEqual(recorder.idempotencyKeys[0], recorder.idempotencyKeys[1])
    }

    func testChangedAmountAfterLostResponseGetsNewKey() async throws {
        let recorder = RequestRecorder()
        MockURLProtocol.requestHandler = { request in
            recorder.record(request)
            if recorder.requests.count == 1 {
                throw URLError(.networkConnectionLost)
            }
            return (Self.response(for: request, status: 200), Data(self.fundResponse.utf8))
        }
        let client = makeClient()
        let submission = IdempotentSubmission()

        _ = try? await client.fundGroup(groupId: "g1", amount: 5_000_000, submission: submission)
        _ = try await client.fundGroup(groupId: "g1", amount: 9_000_000, submission: submission)

        XCTAssertNotEqual(recorder.idempotencyKeys[0], recorder.idempotencyKeys[1])
    }

    // MARK: - Key lifecycle

    func testKeyIsStableForIdenticalRequestsUntilFinalAnswer() throws {
        let counter = KeyCounter()
        let submission = IdempotentSubmission(makeKey: { counter.next() })
        let request = Self.request(path: "/v1/groups/g1/fund", body: #"{"amount":1}"#)

        XCTAssertEqual(submission.key(for: request), "key-1")
        XCTAssertEqual(submission.key(for: request), "key-1")

        for status in [401, 429, 500, 503] {
            submission.record(response: Self.response(for: request, status: status), forKey: "key-1")
            XCTAssertEqual(submission.key(for: request), "key-1", "status \(status) must keep the key")
        }

        submission.record(response: Self.response(for: request, status: 400), forKey: "key-1")
        XCTAssertEqual(submission.key(for: request), "key-2")
    }

    func testLateAnswerForSupersededKeyDoesNotDropCurrentKey() throws {
        let counter = KeyCounter()
        let submission = IdempotentSubmission(makeKey: { counter.next() })
        let first = Self.request(path: "/v1/groups/g1/fund", body: #"{"amount":1}"#)
        let second = Self.request(path: "/v1/groups/g1/fund", body: #"{"amount":2}"#)

        XCTAssertEqual(submission.key(for: first), "key-1")
        XCTAssertEqual(submission.key(for: second), "key-2")
        submission.record(response: Self.response(for: first, status: 200), forKey: "key-1")

        XCTAssertEqual(submission.key(for: second), "key-2")
    }

    /// `hasPendingKey` is what the money screens are asked to gate an edit on, so its two
    /// edges matter: it is false until a key is actually minted, and it stays true across
    /// every non-final answer.
    func testHasPendingKey_followsTheKeyFromMintToFinalAnswer() throws {
        let submission = IdempotentSubmission { "key-1" }
        var request = URLRequest(url: URL(string: "https://api.test/v1/groups/g1/fund")!)
        request.httpMethod = "POST"
        request.httpBody = Data(#"{"amount":1}"#.utf8)

        // The key is minted inside the send, so a screen reading this between the tap and
        // the request being built sees false. That gap is why the doc pins it to the actor
        // that owns the submission.
        XCTAssertFalse(submission.hasPendingKey)

        let key = submission.key(for: request)
        XCTAssertTrue(submission.hasPendingKey)

        // A 5xx is not the request's result, so the key stays pending.
        submission.record(response: response(status: 502, for: request), forKey: key)
        XCTAssertTrue(submission.hasPendingKey)

        // An in-progress 409 is not final either.
        submission.record(
            response: response(
                status: 409,
                for: request,
                headers: [IdempotentSubmission.statusHeader: IdempotentSubmission.inProgressStatus]
            ),
            forKey: key
        )
        XCTAssertTrue(submission.hasPendingKey)

        submission.record(response: response(status: 200, for: request), forKey: key)
        XCTAssertFalse(submission.hasPendingKey)
    }

    private func response(
        status: Int,
        for request: URLRequest,
        headers: [String: String]? = nil
    ) -> HTTPURLResponse {
        HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: headers)!
    }

    func testMoneyBodyEncodingIsByteStableAcrossRetries() throws {
        let first = try MonacoHTTPTransport.idempotentBodyEncoder().encode(
            CreatePlatformWithdrawalRequestDTO(amount: 5_000_000, toAddress: "Dest111")
        )
        let second = try MonacoHTTPTransport.idempotentBodyEncoder().encode(
            CreatePlatformWithdrawalRequestDTO(amount: 5_000_000, toAddress: "Dest111")
        )
        XCTAssertEqual(first, second)
        XCTAssertEqual(String(decoding: first, as: UTF8.self), #"{"amount":5000000,"toAddress":"Dest111"}"#)
    }

    // MARK: - Helpers

    private func makeClient() -> MonacoAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: URLSession(configuration: configuration),
            accessTokenProvider: { TestFixtures.fixtureSessionToken }
        )
    }

    private static func request(path: String, body: String) -> URLRequest {
        var request = URLRequest(url: URL(string: "https://api.test\(path)")!)
        request.httpMethod = "POST"
        request.httpBody = Data(body.utf8)
        return request
    }

    private static func response(for request: URLRequest, status: Int, headers: [String: String] = [:]) -> HTTPURLResponse {
        var fields = ["Content-Type": "application/json"]
        fields.merge(headers) { _, new in new }
        return HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: fields)!
    }
}

private final class RequestRecorder: @unchecked Sendable {
    private let lock = NSLock()
    private var recorded: [URLRequest] = []

    func record(_ request: URLRequest) {
        lock.lock()
        defer { lock.unlock() }
        recorded.append(request)
    }

    var requests: [URLRequest] {
        lock.lock()
        defer { lock.unlock() }
        return recorded
    }

    var idempotencyKeys: [String?] {
        requests.map { $0.value(forHTTPHeaderField: IdempotentSubmission.keyHeader) }
    }
}

private final class KeyCounter: @unchecked Sendable {
    private let lock = NSLock()
    private var count = 0

    func next() -> String {
        lock.lock()
        defer { lock.unlock() }
        count += 1
        return "key-\(count)"
    }
}
