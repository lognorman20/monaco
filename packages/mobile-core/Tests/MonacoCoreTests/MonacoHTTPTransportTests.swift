import XCTest
@testable import MonacoCore

final class MonacoHTTPTransportTests: XCTestCase {
    private let url = URL(string: "https://api.test/v1/me")!

    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        AccessTokenRefreshRegistry.shared.register(nil)
        super.tearDown()
    }

    private func request(token: String?, method: String = "GET", body: Data? = nil) -> URLRequest {
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.httpBody = body
        if let token {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        return request
    }

    private func respond(_ request: URLRequest, status: Int, body: String = "{}") -> (HTTPURLResponse, Data) {
        (HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: nil)!, Data(body.utf8))
    }

    private func makeMockURLSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return URLSession(configuration: configuration)
    }

    private func status(_ response: URLResponse) -> Int? {
        (response as? HTTPURLResponse)?.statusCode
    }

    func test401_refreshesTokenAndRetriesOnce_withTheFreshToken() async throws {
        let authorizations = Recorder<String?>()
        let rejected = Recorder<String>()
        MockURLProtocol.requestHandler = { [self] request in
            let header = request.value(forHTTPHeaderField: "Authorization")
            authorizations.append(header)
            return respond(request, status: header == "Bearer fresh" ? 200 : 401, body: #"{"ok":true}"#)
        }
        let transport = MonacoHTTPTransport(session: makeMockURLSession()) { token in
            rejected.append(token)
            return "fresh"
        }

        let (data, response) = try await transport.data(for: request(token: "stale", method: "POST"))

        XCTAssertEqual(status(response), 200)
        XCTAssertEqual(String(decoding: data, as: UTF8.self), #"{"ok":true}"#)
        XCTAssertEqual(authorizations.values, ["Bearer stale", "Bearer fresh"])
        XCTAssertEqual(rejected.values, ["stale"])
    }

    func test401_afterRetry_isReturnedAndNotRetriedAgain() async throws {
        let calls = Recorder<String?>()
        MockURLProtocol.requestHandler = { [self] request in
            calls.append(request.value(forHTTPHeaderField: "Authorization"))
            return respond(request, status: 401)
        }
        let transport = MonacoHTTPTransport(session: makeMockURLSession()) { _ in "fresh" }

        let (_, response) = try await transport.data(for: request(token: "stale"))

        XCTAssertEqual(status(response), 401)
        XCTAssertEqual(calls.values.count, 2)
    }

    func test401_whenUserIsSignedOut_returnsThe401WithoutRetry() async throws {
        let calls = Recorder<String?>()
        MockURLProtocol.requestHandler = { [self] request in
            calls.append(request.value(forHTTPHeaderField: "Authorization"))
            return respond(request, status: 401)
        }
        let transport = MonacoHTTPTransport(session: makeMockURLSession()) { _ in nil }

        let (_, response) = try await transport.data(for: request(token: "stale"))

        XCTAssertEqual(status(response), 401)
        XCTAssertEqual(calls.values.count, 1)
    }

    func test401_whenRefreshReturnsTheSameToken_doesNotLoop() async throws {
        let calls = Recorder<String?>()
        MockURLProtocol.requestHandler = { [self] request in
            calls.append(request.value(forHTTPHeaderField: "Authorization"))
            return respond(request, status: 401)
        }
        let transport = MonacoHTTPTransport(session: makeMockURLSession()) { token in token }

        let (_, response) = try await transport.data(for: request(token: "stale"))

        XCTAssertEqual(status(response), 401)
        XCTAssertEqual(calls.values.count, 1)
    }

    func test401_whenRefreshFailsOffline_throwsTheConnectionError_notA401() async {
        MockURLProtocol.requestHandler = { [self] request in respond(request, status: 401) }
        let transport = MonacoHTTPTransport(session: makeMockURLSession()) { _ in
            throw URLError(.notConnectedToInternet)
        }

        do {
            _ = try await transport.data(for: request(token: "stale"))
            XCTFail("expected the refresh failure to propagate")
        } catch {
            XCTAssertEqual((error as? URLError)?.code, .notConnectedToInternet)
            XCTAssertTrue(error.isTokenRefreshFailure)
        }
    }

    func test401_whenRefreshThrowsSomethingElse_isStillMarkedAsNeverSent() async {
        struct AuthProviderDown: Error {}
        MockURLProtocol.requestHandler = { [self] request in respond(request, status: 401) }
        let transport = MonacoHTTPTransport(session: makeMockURLSession()) { _ in throw AuthProviderDown() }

        do {
            _ = try await transport.data(for: request(token: "stale", method: "POST"))
            XCTFail("expected the refresh failure to propagate")
        } catch {
            // A money POST rejected with 401 before the backend claimed its idempotency key
            // provably did not run, so the copy must not call the outcome unknown.
            XCTAssertTrue(error.isTokenRefreshFailure)
            XCTAssertEqual((error as? URLError)?.code, .userAuthenticationRequired)
            XCTAssertTrue((error as NSError).userInfo[NSUnderlyingErrorKey] is AuthProviderDown)
        }
    }

    func testRequestFailure_isNotMistakenForARefreshFailure() async {
        MockURLProtocol.requestHandler = { _ in throw URLError(.timedOut) }
        let transport = MonacoHTTPTransport(session: makeMockURLSession()) { _ in "fresh" }

        do {
            _ = try await transport.data(for: request(token: "stale", method: "POST"))
            XCTFail("expected a transport error")
        } catch {
            XCTAssertFalse(error.isTokenRefreshFailure)
        }
    }

    func testTimeouts_readsGetTheShortBudget_writesWithAKeyGetTheMoneyBudget() async throws {
        let timeouts = Recorder<TimeInterval>()
        MockURLProtocol.requestHandler = { [self] request in
            timeouts.append(request.timeoutInterval)
            return respond(request, status: 200)
        }
        let transport = MonacoHTTPTransport(session: makeMockURLSession())

        _ = try await transport.data(for: request(token: "t"))
        _ = try await transport.send(
            request(token: "t", method: "POST", body: Data(#"{"amount":1}"#.utf8)),
            submission: IdempotentSubmission { "key-1" }
        )
        _ = try await transport.send(request(token: "t"), timeout: MonacoRequestTimeout.upload)

        XCTAssertEqual(
            timeouts.values,
            [MonacoRequestTimeout.standard, MonacoRequestTimeout.moneyWrite, MonacoRequestTimeout.upload]
        )
    }

    /// The central claim of this PR: a money POST whose outcome was never seen can be sent
    /// again without moving the money twice. That only holds if the resend carries the same
    /// key AND the same budget. Nothing pinned it before.
    func testTimedOutMoneyWrite_resendsUnderTheSameKeyAndTheSameBudget() async throws {
        let keys = Recorder<String?>()
        let timeouts = Recorder<TimeInterval>()
        var failFirst = true
        MockURLProtocol.requestHandler = { [self] request in
            keys.append(request.value(forHTTPHeaderField: IdempotentSubmission.keyHeader))
            timeouts.append(request.timeoutInterval)
            if failFirst {
                failFirst = false
                throw URLError(.timedOut)
            }
            return respond(request, status: 200)
        }
        let transport = MonacoHTTPTransport(session: makeMockURLSession())
        let submission = IdempotentSubmission { "money-key" }
        let body = Data(#"{"amount":1}"#.utf8)

        do {
            _ = try await transport.send(
                request(token: "t", method: "POST", body: body), submission: submission
            )
            XCTFail("expected the first attempt to time out")
        } catch {
            XCTAssertEqual((error as? URLError)?.code, .timedOut)
        }
        // A timeout stores no response, so the key stays pending for the resend.
        XCTAssertTrue(submission.hasPendingKey)

        _ = try await transport.send(
            request(token: "t", method: "POST", body: body), submission: submission
        )

        XCTAssertEqual(keys.values, ["money-key", "money-key"], "the resend minted a new key")
        XCTAssertEqual(
            timeouts.values,
            [MonacoRequestTimeout.moneyWrite, MonacoRequestTimeout.moneyWrite]
        )
        // A 200 is a final answer, so the submission is done.
        XCTAssertFalse(submission.hasPendingKey)
    }

    /// The 401 retry rebuilds nothing today — it copies the request and swaps one header —
    /// but nothing said so. If someone ever reconstructs the request there, the key would be
    /// dropped and the retry would become a second submission of the same money.
    func test401Retry_keepsTheIdempotencyKeyAndTheMoneyBudget() async throws {
        let keys = Recorder<String?>()
        let timeouts = Recorder<TimeInterval>()
        MockURLProtocol.requestHandler = { [self] request in
            keys.append(request.value(forHTTPHeaderField: IdempotentSubmission.keyHeader))
            timeouts.append(request.timeoutInterval)
            let header = request.value(forHTTPHeaderField: "Authorization")
            return respond(request, status: header == "Bearer fresh" ? 200 : 401)
        }
        let transport = MonacoHTTPTransport(session: makeMockURLSession()) { _ in "fresh" }

        _ = try await transport.send(
            request(token: "stale", method: "POST", body: Data(#"{"amount":1}"#.utf8)),
            submission: IdempotentSubmission { "money-key" }
        )

        XCTAssertEqual(keys.values, ["money-key", "money-key"], "the 401 retry lost the key")
        XCTAssertEqual(
            timeouts.values,
            [MonacoRequestTimeout.moneyWrite, MonacoRequestTimeout.moneyWrite],
            "the 401 retry lost the money budget"
        )
    }

    func testTimeoutBudget_isNeverTheSharedSessionDefault() {
        XCTAssertLessThan(MonacoRequestTimeout.standard, 60)
        // The session carries the *longest* budget, not the shortest: URLSession does not
        // promise that a request's own timeoutInterval outranks the session's, so the
        // session value has to be a ceiling the per-request stamp can only shorten.
        // Configuring it with `standard` would cap every money write at 15s.
        XCTAssertEqual(URLSession.monaco.configuration.timeoutIntervalForRequest, MonacoRequestTimeout.sessionCeiling)
        XCTAssertGreaterThanOrEqual(MonacoRequestTimeout.sessionCeiling, MonacoRequestTimeout.moneyWrite)
        XCTAssertGreaterThanOrEqual(MonacoRequestTimeout.sessionCeiling, MonacoRequestTimeout.upload)
        XCTAssertEqual(URLSession.monaco.configuration.timeoutIntervalForResource, MonacoRequestTimeout.resource)
        XCTAssertFalse(URLSession.monaco.configuration.waitsForConnectivity)
    }

    /// The budgets are only worth anything if URLSession enforces the per-request one. The
    /// recording test above cannot show that: `MockURLProtocol` answers at once and never
    /// runs a timer, so it passes whichever deadline the session actually applies. This one
    /// stalls every request past the read budget and under the money budget, against the
    /// real `MonacoRequestTimeout.sessionConfiguration()`, and asserts the two outcomes
    /// that matter: the read gives up, the keyed money write survives.
    ///
    /// It costs about `stall` seconds of wall clock, with both requests running concurrently.
    ///
    /// Measured on this runtime, the money write survives even when the session is
    /// configured at `standard`: here a request's own longer `timeoutInterval` does outrank
    /// the session's. That precedence is undocumented and not guaranteed on device, which is
    /// why the session is configured with the ceiling anyway — see
    /// `testTimeoutBudget_isNeverTheSharedSessionDefault`, which is the assertion that pins
    /// it. What this test pins is that the stamp is applied and enforced at all: drop it, or
    /// stamp the wrong budget, and the read stops timing out on time.
    func testTimeoutBudget_isEnforced_moneyWriteOutlivesTheReadBudget() async throws {
        let stall = MonacoRequestTimeout.standard + 5
        try XCTSkipUnless(stall < MonacoRequestTimeout.moneyWrite, "budgets no longer straddle the stall")
        StallingURLProtocol.stall = stall
        defer { StallingURLProtocol.stall = 0 }

        let configuration = MonacoRequestTimeout.sessionConfiguration()
        configuration.protocolClasses = [StallingURLProtocol.self]
        let transport = MonacoHTTPTransport(session: URLSession(configuration: configuration))

        async let read: Void = {
            do {
                _ = try await transport.data(for: request(token: "t"))
                XCTFail("a read should give up after \(MonacoRequestTimeout.standard)s")
            } catch {
                XCTAssertEqual((error as? URLError)?.code, .timedOut)
            }
        }()

        async let write: Void = {
            do {
                _ = try await transport.send(
                    request(token: "t", method: "POST", body: Data(#"{"amount":1}"#.utf8)),
                    submission: IdempotentSubmission { "key-1" }
                )
            } catch {
                XCTFail("a money write must outlive a \(stall)s confirm, but failed: \(error)")
            }
        }()

        _ = await (read, write)
    }

    /// The budgets only reach a real request if the default client is actually on Monaco's
    /// session. Nothing pinned that, so a default argument slipping back to `.shared` would
    /// have put every request on the 60s one-size-fits-all deadline with no test failing.
    func testDefaultClient_runsOnMonacosOwnSession_notTheSharedOne() throws {
        let transportSession = try XCTUnwrap(Self.session(in: MonacoHTTPTransport()))
        XCTAssertTrue(transportSession === URLSession.monaco)
        XCTAssertFalse(transportSession === URLSession.shared)

        let transport = try XCTUnwrap(
            Mirror(reflecting: MonacoAPIClient()).children
                .first { $0.label == "session" }?.value as? MonacoHTTPTransport
        )
        XCTAssertTrue(try XCTUnwrap(Self.session(in: transport)) === URLSession.monaco)
    }

    private static func session(in transport: MonacoHTTPTransport) -> URLSession? {
        Mirror(reflecting: transport).children.first { $0.label == "session" }?.value as? URLSession
    }

    func test401_withoutBearerToken_isNotRetried() async throws {
        let refreshes = Recorder<String>()
        MockURLProtocol.requestHandler = { [self] request in respond(request, status: 401) }
        let transport = MonacoHTTPTransport(session: makeMockURLSession()) { token in
            refreshes.append(token)
            return "fresh"
        }

        let (_, response) = try await transport.data(for: request(token: nil))

        XCTAssertEqual(status(response), 401)
        XCTAssertTrue(refreshes.values.isEmpty)
    }

    func testNon401Failures_areNotRetried() async throws {
        let refreshes = Recorder<String>()
        for code in [200, 403, 429, 500] {
            MockURLProtocol.requestHandler = { [self] request in respond(request, status: code) }
            let transport = MonacoHTTPTransport(session: makeMockURLSession()) { token in
                refreshes.append(token)
                return "fresh"
            }
            let (_, response) = try await transport.data(for: request(token: "stale"))
            XCTAssertEqual(status(response), code)
        }
        XCTAssertTrue(refreshes.values.isEmpty)
    }

    func testTransportFailure_propagatesWithoutRefresh() async {
        let refreshes = Recorder<String>()
        MockURLProtocol.requestHandler = { _ in throw URLError(.timedOut) }
        let transport = MonacoHTTPTransport(session: makeMockURLSession()) { token in
            refreshes.append(token)
            return "fresh"
        }

        do {
            _ = try await transport.data(for: request(token: "stale"))
            XCTFail("expected a transport error")
        } catch {
            XCTAssertEqual((error as NSError).domain, NSURLErrorDomain)
        }
        XCTAssertTrue(refreshes.values.isEmpty)
    }

    func testRegisteredRefresher_isUsedByAPIClient_andMalformedRetryBodyStillFailsCleanly() async {
        AccessTokenRefreshRegistry.shared.register { _ in "fresh" }
        let calls = Recorder<String?>()
        MockURLProtocol.requestHandler = { [self] request in
            let header = request.value(forHTTPHeaderField: "Authorization")
            calls.append(header)
            // The retry succeeds at HTTP level but the body is not the expected JSON.
            return respond(request, status: header == "Bearer fresh" ? 200 : 401, body: "not json")
        }
        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { "stale" }
        )

        do {
            _ = try await client.me()
            XCTFail("expected a decoding error")
        } catch {
            XCTAssertTrue(error is DecodingError, "got \(error)")
        }
        XCTAssertEqual(calls.values, ["Bearer stale", "Bearer fresh"])
    }

    func testAPIClient_surfaces401_whenNoRefresherIsRegistered() async {
        MockURLProtocol.requestHandler = { [self] request in respond(request, status: 401) }
        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { "stale" }
        )

        do {
            _ = try await client.me()
            XCTFail("expected 401")
        } catch {
            XCTAssertEqual(error as? MonacoAPIError, .httpStatus(401))
        }
    }

    func testBearerTokenParsing() {
        XCTAssertEqual(MonacoHTTPTransport.bearerToken(in: request(token: "abc")), "abc")
        XCTAssertNil(MonacoHTTPTransport.bearerToken(in: request(token: nil)))
        var basic = request(token: nil)
        basic.setValue("Basic abc", forHTTPHeaderField: "Authorization")
        XCTAssertNil(MonacoHTTPTransport.bearerToken(in: basic))
        var empty = request(token: nil)
        empty.setValue("Bearer  ", forHTTPHeaderField: "Authorization")
        XCTAssertNil(MonacoHTTPTransport.bearerToken(in: empty))
    }
}

final class SingleFlightTests: XCTestCase {
    func testConcurrentCallers_shareOneRun() async throws {
        let runs = Recorder<Int>()
        let flight = SingleFlight<String?>()

        let results = try await withThrowingTaskGroup(of: String?.self) { group -> [String?] in
            for _ in 0..<8 {
                group.addTask {
                    try await flight.run {
                        runs.append(1)
                        try await Task.sleep(nanoseconds: 100_000_000)
                        return "fresh"
                    }
                }
            }
            var collected: [String?] = []
            for try await value in group { collected.append(value) }
            return collected
        }

        XCTAssertEqual(results.count, 8)
        XCTAssertTrue(results.allSatisfy { $0 == "fresh" })
        XCTAssertEqual(runs.values.count, 1)
    }

    func testFailure_reachesEveryCaller_andTheNextCallRunsAgain() async throws {
        let flight = SingleFlight<String?>()

        do {
            _ = try await flight.run { throw URLError(.notConnectedToInternet) }
            XCTFail("expected failure")
        } catch {
            XCTAssertEqual((error as? URLError)?.code, .notConnectedToInternet)
        }

        let value = try await flight.run { "fresh" }
        XCTAssertEqual(value, "fresh")
    }
}

final class LoginFailureCopyTests: XCTestCase {
    func testWrongCode_keepsTheCodeField_andSaysSo() {
        let failure = LoginFailureCopy.failure(forHTTPStatus: 422, step: .verifyCode, detail: "Invalid code")
        XCTAssertEqual(failure, .codeRejected)
        XCTAssertTrue(failure.keepsCodeEntry)
        XCTAssertEqual(
            LoginFailureCopy.message(for: failure, step: .verifyCode),
            "That code didn't work. Check it, or send a new one."
        )
    }

    func testRateLimit_onEitherStep() {
        for step in [LoginStep.sendCode, .verifyCode] {
            let failure = LoginFailureCopy.failure(forHTTPStatus: 429, step: step, detail: nil)
            XCTAssertEqual(failure, .rateLimited)
            XCTAssertFalse(failure.keepsCodeEntry)
            XCTAssertEqual(
                LoginFailureCopy.message(for: failure, step: step),
                "Too many attempts. Wait a minute, then try again."
            )
        }
    }

    func testOffline_keepsTheCodeField_soTheSameCodeCanBeRetried() {
        XCTAssertTrue(LoginFailure.offline.keepsCodeEntry)
        XCTAssertEqual(
            LoginFailureCopy.message(for: .offline, step: .verifyCode),
            "No connection. Check your internet and try again."
        )
    }

    func testSendRejected_isNotReportedAsAWrongCode_andKeepsProviderDetail() {
        let failure = LoginFailureCopy.failure(forHTTPStatus: 400, step: .sendCode, detail: " Invalid phone number ")
        XCTAssertEqual(failure, .other(detail: " Invalid phone number "))
        XCTAssertEqual(
            LoginFailureCopy.message(for: failure, step: .sendCode),
            "Couldn't send the code. Try again. Invalid phone number"
        )
    }

    func testServerError_fallsBackToGenericCopy() {
        let failure = LoginFailureCopy.failure(forHTTPStatus: 503, step: .verifyCode, detail: nil)
        XCTAssertEqual(failure, .other(detail: nil))
        XCTAssertEqual(LoginFailureCopy.message(for: failure, step: .verifyCode), "Couldn't sign you in. Try again.")
    }
}

/// Thread-safe value recorder for closures called off the test's thread.
final class Recorder<Value>: @unchecked Sendable {
    private let lock = NSLock()
    private var storage: [Value] = []

    func append(_ value: Value) {
        lock.lock()
        defer { lock.unlock() }
        storage.append(value)
    }

    var values: [Value] {
        lock.lock()
        defer { lock.unlock() }
        return storage
    }
}
