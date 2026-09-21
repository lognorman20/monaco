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
            // The POST was refused with a 401 before the handler ran and was never sent
            // again, so the money copy must not call its outcome unknown.
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

    // MARK: - Timeouts

    /// Nothing is inferred from the method: without an idempotency key there is no header to
    /// read a "money" request off, so a POST that names no budget gets the read one, and
    /// every slow route has to name its own at the call site.
    func testTimeouts_standardUnlessTheCallSiteNamesABudget() async throws {
        let timeouts = Recorder<TimeInterval>()
        MockURLProtocol.requestHandler = { [self] request in
            timeouts.append(request.timeoutInterval)
            return respond(request, status: 200)
        }
        let transport = MonacoHTTPTransport(session: makeMockURLSession())

        _ = try await transport.data(for: request(token: "t"))
        _ = try await transport.data(for: request(token: "t", method: "POST", body: Data(#"{"amount":1}"#.utf8)))
        _ = try await transport.data(for: request(token: "t", method: "POST"), timeout: MonacoRequestTimeout.moneyWrite)
        _ = try await transport.data(for: request(token: "t"), timeout: MonacoRequestTimeout.upload)
        _ = try await transport.data(from: url)

        XCTAssertEqual(timeouts.values, [
            MonacoRequestTimeout.standard,
            MonacoRequestTimeout.standard,
            MonacoRequestTimeout.moneyWrite,
            MonacoRequestTimeout.upload,
            MonacoRequestTimeout.standard,
        ])
    }

    /// The 401 retry copies the request and swaps one header. If it ever rebuilt the request
    /// instead, a money POST would drop to the read budget on the retry.
    func test401Retry_keepsTheBudgetTheCallSiteChose() async throws {
        let timeouts = Recorder<TimeInterval>()
        MockURLProtocol.requestHandler = { [self] request in
            timeouts.append(request.timeoutInterval)
            let header = request.value(forHTTPHeaderField: "Authorization")
            return respond(request, status: header == "Bearer fresh" ? 200 : 401)
        }
        let transport = MonacoHTTPTransport(session: makeMockURLSession()) { _ in "fresh" }

        _ = try await transport.data(
            for: request(token: "stale", method: "POST", body: Data(#"{"amount":1}"#.utf8)),
            timeout: MonacoRequestTimeout.moneyWrite
        )

        XCTAssertEqual(
            timeouts.values,
            [MonacoRequestTimeout.moneyWrite, MonacoRequestTimeout.moneyWrite],
            "the 401 retry lost the money budget"
        )
    }

    /// Every route whose handler moves money on Base inside the request names the money
    /// budget, and every route that prices a trade names the quote budget. The API has no
    /// idempotency key (the restore series that starts at #413 brings one back), so there is
    /// nothing else a budget could be keyed off; a route dropped from this list silently
    /// falls to 15s.
    func testCoreClient_moneyAndQuoteRoutesNameTheirBudget_andReadsKeepTheShortOne() async {
        let timeouts = Recorder<String>()
        MockURLProtocol.requestHandler = { [self] request in
            timeouts.append("\(request.httpMethod ?? "") \(request.url!.path) \(request.timeoutInterval)")
            return respond(request, status: 500)
        }
        let client = MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { "t" }
        )

        _ = try? await client.fundGroup(groupId: "g1", amount: 1)
        _ = try? await client.createPlatformWithdrawal(amount: 1, toAddress: "0xabc")
        _ = try? await client.withdrawToBalance(groupId: "g1")
        try? await client.leaveGroup(groupId: "g1", withdrawStake: true)
        _ = try? await client.postRedeem(groupId: "g1", shareUnits: "1")
        _ = try? await client.devBuy(groupId: "g1", symbol: "AAPLc", usdc: 1)
        _ = try? await client.createProposal(groupId: "g1", symbol: "AAPLc", usdc: 1)
        _ = try? await client.uploadProfilePhoto(imageData: Data([0xFF]), mimeType: "image/jpeg")
        _ = try? await client.me()
        _ = try? await client.postQuote(groupId: "g1", symbol: "AAPLc", usdc: 1)

        let money = MonacoRequestTimeout.moneyWrite
        XCTAssertEqual(timeouts.values, [
            "POST /v1/groups/g1/fund \(money)",
            "POST /v1/me/withdrawals \(money)",
            "POST /v1/groups/g1/withdraw-to-balance \(money)",
            "POST /v1/groups/g1/leave \(money)",
            "POST /v1/groups/g1/redeems \(money)",
            "POST /v1/dev/groups/g1/buy \(money)",
            "POST /v1/groups/g1/proposals \(MonacoRequestTimeout.quote)",
            "POST /v1/me/profile-photo \(MonacoRequestTimeout.upload)",
            "GET /v1/me \(MonacoRequestTimeout.standard)",
            "POST /v1/groups/g1/quotes \(MonacoRequestTimeout.quote)",
        ])
    }

    func testTimeoutBudget_isNeverTheSharedSessionDefault() {
        XCTAssertLessThan(MonacoRequestTimeout.standard, 60)
        // The session carries the *longest* budget, not the shortest: URLSession does not
        // promise that a request's own timeoutInterval outranks the session's, so the
        // session value has to be a ceiling the per-request stamp can only shorten.
        // Configuring it with `standard` would cap every money write at 15s.
        XCTAssertEqual(URLSession.monaco.configuration.timeoutIntervalForRequest, MonacoRequestTimeout.sessionCeiling)
        XCTAssertGreaterThanOrEqual(MonacoRequestTimeout.sessionCeiling, MonacoRequestTimeout.quote)
        XCTAssertGreaterThanOrEqual(MonacoRequestTimeout.sessionCeiling, MonacoRequestTimeout.moneyWrite)
        XCTAssertGreaterThanOrEqual(MonacoRequestTimeout.sessionCeiling, MonacoRequestTimeout.upload)
        XCTAssertGreaterThanOrEqual(MonacoRequestTimeout.sessionCeiling, MonacoRequestTimeout.walletProvisioning)
        XCTAssertEqual(URLSession.monaco.configuration.timeoutIntervalForResource, MonacoRequestTimeout.resource)
        XCTAssertFalse(URLSession.monaco.configuration.waitsForConnectivity)
    }

    /// The budgets are only worth anything if URLSession enforces the per-request one. The
    /// recording tests above cannot show that: `MockURLProtocol` answers at once and never
    /// runs a timer. This one stalls every request past the read budget and under the money
    /// budget, against the real `MonacoRequestTimeout.sessionConfiguration()`, and asserts
    /// the two outcomes that matter: the read gives up, the money write survives.
    ///
    /// It costs about `stall` seconds of wall clock, with both requests running concurrently.
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
                _ = try await transport.data(
                    for: request(token: "t", method: "POST", body: Data(#"{"amount":1}"#.utf8)),
                    timeout: MonacoRequestTimeout.moneyWrite
                )
            } catch {
                XCTFail("a money write must outlive a \(stall)s confirm, but failed: \(error)")
            }
        }()

        _ = await (read, write)
    }

    /// The budgets only reach a real request if the default client is actually on Monaco's
    /// session. A default argument slipping back to `.shared` would put every request on the
    /// 60s one-size-fits-all deadline with no other test failing.
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

    // MARK: - Retry-After

    func testRetryAfter_readsSecondsAndHTTPDates_andRefusesAnythingElse() {
        func response(_ value: String?) -> HTTPURLResponse {
            HTTPURLResponse(
                url: url,
                statusCode: 429,
                httpVersion: nil,
                headerFields: value.map { ["Retry-After": $0] }
            )!
        }
        let now = Date(timeIntervalSince1970: 784_111_777) // Sun, 06 Nov 1994 08:49:37 GMT

        XCTAssertEqual(MonacoHTTPTransport.retryAfterSeconds(in: response("30"), now: now), 30)
        XCTAssertEqual(MonacoHTTPTransport.retryAfterSeconds(in: response(" 0 "), now: now), 0)
        XCTAssertEqual(
            MonacoHTTPTransport.retryAfterSeconds(in: response("Sun, 06 Nov 1994 08:50:37 GMT"), now: now),
            60
        )
        // Absent, malformed, negative, or a date already past: no number rather than an invented one.
        for raw in [nil, "", "soon", "-5", "1.5", "Sun, 06 Nov 1994 08:49:00 GMT"] {
            XCTAssertNil(MonacoHTTPTransport.retryAfterSeconds(in: response(raw), now: now), "\(raw ?? "nil")")
        }
        let notHTTP = URLResponse(url: url, mimeType: nil, expectedContentLength: 0, textEncodingName: nil)
        XCTAssertNil(MonacoHTTPTransport.retryAfterSeconds(in: notHTTP))
    }

    func testStatusCode_readsTheStatusWhicheverCaseCarriesIt() {
        XCTAssertEqual(MonacoAPIError.httpStatus(403).statusCode, 403)
        XCTAssertEqual(MonacoAPIError.rejected(status: 403, message: "not a group member").statusCode, 403)
        XCTAssertEqual(MonacoAPIError.rateLimited(retryAfterSeconds: 30).statusCode, 429)
        XCTAssertEqual(MonacoAPIError.rateLimited(retryAfterSeconds: nil).statusCode, 429)
        XCTAssertNil(MonacoAPIError.invalidResponse.statusCode)
        XCTAssertNil(MonacoAPIError.leaveBlocked(.pendingRedeem).statusCode)
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
