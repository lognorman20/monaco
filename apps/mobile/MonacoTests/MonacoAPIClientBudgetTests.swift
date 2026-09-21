import MonacoCore
import XCTest
@testable import Monaco

/// The app's own API client carries the money screens (fund, cash out, cash out of a cabal),
/// so the per-request budgets and the 429 countdown have to hold here too, not only in
/// MonacoCore's client.
@MainActor
final class MonacoAPIClientBudgetTests: XCTestCase {
    private let baseURL = URL(string: "https://api.test")!

    override func setUp() {
        super.setUp()
        BudgetStubProtocol.reset()
    }

    override func tearDown() {
        BudgetStubProtocol.reset()
        super.tearDown()
    }

    /// Main has no idempotency key, so nothing on the wire says "this moves money": every
    /// route whose handler submits on Base inside the request has to name the money budget
    /// where it is built, and a route dropped from this list silently falls to 15s.
    func testMoneyAndProvisioningRoutes_nameTheirBudget_readsKeepTheShortOne() async {
        BudgetStubProtocol.respond(status: 500, body: Data("{}".utf8))
        let client = MonacoAPIClient(baseURL: baseURL, session: BudgetStubProtocol.session())

        _ = try? await client.openSession(accessToken: "t")
        _ = try? await client.createGroup(accessToken: "t", name: "Night Owls")
        _ = try? await client.fundGroup(accessToken: "t", groupId: "g1", amount: 1)
        _ = try? await client.createPlatformWithdrawal(accessToken: "t", amount: 1, toAddress: "0xabc")
        _ = try? await client.withdrawToBalance(accessToken: "t", groupId: "g1")
        try? await client.leaveGroup(accessToken: "t", groupId: "g1", withdrawStake: true)
        _ = try? await client.createDeposit(accessToken: "t", groupId: "g1", amount: 1)
        _ = try? await client.retryTransaction(accessToken: "t", transactionId: "tx1")
        _ = try? await client.postRedeem(accessToken: "t", groupId: "g1", shareUnits: "1")
        _ = try? await client.devBuy(accessToken: "t", groupId: "g1", symbol: "AAPLc", usdc: 1)
        _ = try? await client.createProposal(accessToken: "t", groupId: "g1", symbol: "AAPLc", usdcMicros: 1)
        _ = try? await client.me(accessToken: "t")
        _ = try? await client.postQuote(accessToken: "t", groupId: "g1", symbol: "AAPLc", usdc: 1)
        _ = try? await client.joinGroup(accessToken: "t", groupId: "g1")

        let money = MonacoRequestTimeout.moneyWrite
        let provisioning = MonacoRequestTimeout.walletProvisioning
        let read = MonacoRequestTimeout.standard
        XCTAssertEqual(BudgetStubProtocol.seen(), [
            "POST /v1/auth/session \(provisioning)",
            "POST /v1/groups \(provisioning)",
            "POST /v1/groups/g1/fund \(money)",
            "POST /v1/me/withdrawals \(money)",
            "POST /v1/groups/g1/withdraw-to-balance \(money)",
            "POST /v1/groups/g1/leave \(money)",
            "POST /v1/groups/g1/deposits \(money)",
            "POST /v1/transactions/tx1/retry \(money)",
            "POST /v1/groups/g1/redeems \(money)",
            "POST /v1/dev/groups/g1/buy \(money)",
            "POST /v1/groups/g1/proposals \(money)",
            "GET /v1/me \(read)",
            "POST /v1/groups/g1/quotes \(read)",
            "POST /v1/groups/g1/join \(read)",
        ])
    }

    /// The money screens used to show "Wait a moment" for a 429 whatever the server said,
    /// because this client turned the response into a bare status and dropped the header.
    func testMoneyRoute429_keepsTheServersRetryAfter_allTheWayToTheCopy() async {
        BudgetStubProtocol.respond(
            status: 429,
            body: Data(#"{"error":"too many requests, try again shortly"}"#.utf8),
            headers: ["Retry-After": "30"]
        )
        let client = MonacoAPIClient(baseURL: baseURL, session: BudgetStubProtocol.session())

        do {
            _ = try await client.fundGroup(accessToken: "t", groupId: "g1", amount: 1)
            XCTFail("expected a 429")
        } catch {
            guard case Monaco.MonacoAPIError.rateLimited(let seconds) = error else {
                return XCTFail("expected rateLimited, got \(error)")
            }
            XCTAssertEqual(seconds, 30)
            XCTAssertEqual(MoneyFlowCopy.fundCabalFailure(FlowErrorInput(error)).nextStep, "Try again in 30 seconds.")
        }
    }

    /// A refusal still carries the server's reason, so the copy can say why.
    func testMoneyRoute4xx_keepsTheServerMessage() async {
        BudgetStubProtocol.respond(
            status: 400,
            body: Data(#"{"error":"amount exceeds available platform balance"}"#.utf8)
        )
        let client = MonacoAPIClient(baseURL: baseURL, session: BudgetStubProtocol.session())

        do {
            _ = try await client.createPlatformWithdrawal(accessToken: "t", amount: 1, toAddress: "0xabc")
            XCTFail("expected a 400")
        } catch {
            XCTAssertEqual(
                MoneyFlowCopy.cashOutFailure(FlowErrorInput(error)).message,
                "That's more than your account balance."
            )
        }
    }

    /// The default client must be on Monaco's own session: a default argument slipping back
    /// to `.shared` would put every request on the 60s one-size-fits-all deadline.
    func testDefaultClient_runsOnMonacosOwnSession() throws {
        let transport = try XCTUnwrap(
            Mirror(reflecting: MonacoAPIClient(baseURL: baseURL)).children
                .first { $0.label == "session" }?.value as? MonacoHTTPTransport
        )
        let session = try XCTUnwrap(
            Mirror(reflecting: transport).children.first { $0.label == "session" }?.value as? URLSession
        )
        XCTAssertTrue(session === URLSession.monaco)
    }
}

/// Answers every request with one canned response and records "METHOD path timeout".
private final class BudgetStubProtocol: URLProtocol, @unchecked Sendable {
    private struct Canned {
        let status: Int
        let body: Data
        let headers: [String: String]
    }

    nonisolated(unsafe) private static var canned = Canned(status: 500, body: Data(), headers: [:])
    nonisolated(unsafe) private static var log: [String] = []
    private static let lock = NSLock()

    nonisolated static func session() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [BudgetStubProtocol.self]
        return URLSession(configuration: configuration)
    }

    nonisolated static func reset() {
        lock.lock()
        canned = Canned(status: 500, body: Data(), headers: [:])
        log = []
        lock.unlock()
    }

    nonisolated static func respond(status: Int, body: Data, headers: [String: String] = [:]) {
        lock.lock()
        canned = Canned(status: status, body: body, headers: headers)
        lock.unlock()
    }

    nonisolated static func seen() -> [String] {
        lock.lock()
        defer { lock.unlock() }
        return log
    }

    nonisolated override class func canInit(with request: URLRequest) -> Bool { true }
    nonisolated override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    nonisolated override func startLoading() {
        guard let url = request.url else { return }
        Self.lock.lock()
        Self.log.append("\(request.httpMethod ?? "") \(url.path) \(request.timeoutInterval)")
        let canned = Self.canned
        Self.lock.unlock()

        let response = HTTPURLResponse(
            url: url,
            statusCode: canned.status,
            httpVersion: nil,
            headerFields: canned.headers.merging(["Content-Type": "application/json"]) { current, _ in current }
        )!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: canned.body)
        client?.urlProtocolDidFinishLoading(self)
    }

    nonisolated override func stopLoading() {}
}
