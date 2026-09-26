import XCTest
@testable import MonacoCore

final class WatchlistDTOTests: XCTestCase {
    func testWatchlistDecodesMarketRowsInOrder() throws {
        // Arrange
        let json = """
        {
          "assets": [
            {"symbol": "TSLAx", "name": "Tesla", "solanaMint": "tsla", "routable": true, "priceUsdcMicros": 412700000},
            {"symbol": "AAPLx", "name": "Apple", "solanaMint": "aapl", "routable": true, "priceUsdcMicros": 232050000,
             "change24h": "0.0124", "spark": [226500000, 232050000], "kind": "stock", "logoUrl": "https://example.test/a.png"}
          ],
          "market": {"session": "open", "isOpen": true, "afterHours": false, "asOf": "2026-09-22T14:00:00Z"}
        }
        """

        // Act
        let response = try JSONDecoder().decode(WatchlistResponseDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertEqual(response.assets.map(\.symbol), ["TSLAx", "AAPLx"])
        XCTAssertEqual(response.assets[1].sparkUsdcMicros, [226_500_000, 232_050_000])
        XCTAssertEqual(response.market?.session, .open)
    }

    func testAnEmptyOrNullWatchlistIsEmpty() throws {
        for json in [#"{"assets": []}"#, #"{"assets": null}"#, "{}"] {
            let response = try JSONDecoder().decode(WatchlistResponseDTO.self, from: Data(json.utf8))
            XCTAssertTrue(response.assets.isEmpty, json)
        }
    }

    func testEntryDecodes() throws {
        let entry = try JSONDecoder().decode(
            WatchlistEntryDTO.self,
            from: Data(#"{"symbol": "AAPLx", "position": 3, "createdAt": "2026-09-25T14:30:00Z"}"#.utf8)
        )
        XCTAssertEqual(entry.symbol, "AAPLx")
        XCTAssertEqual(entry.position, 3)
        XCTAssertEqual(entry.createdAt, SharedFormatters.iso8601Date(from: "2026-09-25T14:30:00Z"))
    }
}

final class PriceAlertDTOTests: XCTestCase {
    private func decode(_ json: String) throws -> PriceAlertDTO {
        try JSONDecoder().decode(PriceAlertDTO.self, from: Data(json.utf8))
    }

    func testWaitingAlertDecodes() throws {
        let alert = try decode("""
        {"id": "a1", "symbol": "GOOGLx", "direction": "above", "priceUsdcMicros": 360000000,
         "active": true, "createdAt": "2026-09-23T10:00:00Z"}
        """)
        XCTAssertEqual(alert.direction, .above)
        XCTAssertEqual(alert.priceUsdcMicros, 360_000_000)
        XCTAssertTrue(alert.active)
        XCTAssertFalse(alert.hasFired)
        XCTAssertNil(alert.triggeredPriceUsdcMicros)
    }

    func testFiredAlertDecodesWithItsPrice() throws {
        let alert = try decode("""
        {"id": "a2", "symbol": "TSLAx", "direction": "below", "priceUsdcMicros": 400000000,
         "active": false, "createdAt": "2026-09-20T10:00:00Z",
         "triggeredAt": "2026-09-25T13:59:00.123456Z", "triggeredPriceUsdcMicros": 398700000}
        """)
        XCTAssertTrue(alert.hasFired)
        XCTAssertFalse(alert.active)
        XCTAssertEqual(alert.triggeredPriceUsdcMicros, 398_700_000)
    }

    func testAFiredAlertIsNeverActive() throws {
        let alert = try decode("""
        {"id": "a3", "symbol": "TSLAx", "direction": "below", "priceUsdcMicros": 1, "active": true,
         "createdAt": "2026-09-20T10:00:00Z", "triggeredAt": "2026-09-25T13:59:00Z"}
        """)
        XCTAssertFalse(alert.active)
    }

    func testAnUnknownDirectionFailsLoudly() {
        XCTAssertThrowsError(try decode("""
        {"id": "a4", "symbol": "TSLAx", "direction": "sideways", "priceUsdcMicros": 1, "createdAt": "2026-09-20T10:00:00Z"}
        """))
    }

    func testRoundTripsThroughEncoding() throws {
        let original = PriceAlertDTO(
            id: "a5", symbol: "AAPLx", direction: .below, priceUsdcMicros: 220_000_000, active: false,
            createdAt: Date(timeIntervalSince1970: 1_790_000_000),
            triggeredAt: Date(timeIntervalSince1970: 1_790_100_000), triggeredPriceUsdcMicros: 219_500_000
        )
        let data = try JSONEncoder().encode(original)
        XCTAssertEqual(try JSONDecoder().decode(PriceAlertDTO.self, from: data), original)
    }

    func testReachedMatchesTheServerRule() {
        XCTAssertTrue(PriceAlertDirection.above.isReached(priceUsdcMicros: 360_000_000, lineUsdcMicros: 360_000_000))
        XCTAssertFalse(PriceAlertDirection.above.isReached(priceUsdcMicros: 359_990_000, lineUsdcMicros: 360_000_000))
        XCTAssertTrue(PriceAlertDirection.below.isReached(priceUsdcMicros: 329_000_000, lineUsdcMicros: 330_000_000))
        XCTAssertFalse(PriceAlertDirection.below.isReached(priceUsdcMicros: 330_010_000, lineUsdcMicros: 330_000_000))
    }

    func testGroupsFollowTheServerOrderAndSplitWaitingFromFired() {
        // Arrange
        let t0 = Date(timeIntervalSince1970: 1_790_000_000)
        let alerts = [
            PriceAlertDTO(id: "g1", symbol: "GOOGLx", direction: .above, priceUsdcMicros: 360_000_000, createdAt: t0.addingTimeInterval(300)),
            PriceAlertDTO(id: "t1", symbol: "TSLAx", direction: .below, priceUsdcMicros: 400_000_000, createdAt: t0),
            PriceAlertDTO(id: "g2", symbol: "googlx", direction: .below, priceUsdcMicros: 330_000_000, active: false,
                          createdAt: t0, triggeredAt: t0.addingTimeInterval(600), triggeredPriceUsdcMicros: 329_000_000),
            PriceAlertDTO(id: "g3", symbol: "GOOGLx", direction: .above, priceUsdcMicros: 370_000_000, createdAt: t0.addingTimeInterval(900)),
        ]
        let assets = [MarketSampleData.listAsset(symbol: "GOOGLx", name: "Alphabet", priceUsdcMicros: 352_100_000, change24h: nil)]

        // Act
        let groups = PriceAlertGroup.make(alerts: alerts, assets: assets)

        // Assert
        XCTAssertEqual(groups.map(\.symbol), ["GOOGLx", "TSLAx"])
        XCTAssertEqual(groups[0].waiting.map(\.id), ["g3", "g1"])
        XCTAssertEqual(groups[0].fired.map(\.id), ["g2"])
        XCTAssertEqual(groups[0].asset?.priceUsdcMicros, 352_100_000)
        XCTAssertNil(groups[1].asset)
    }

    func testWriteFailuresReadTheirStatus() {
        XCTAssertEqual(PriceAlertWriteFailure(MonacoAPIError.rejected(status: 409, message: "full")), .limitReached)
        XCTAssertEqual(PriceAlertWriteFailure(MonacoAPIError.rejected(status: 422, message: "met")), .alreadyReached)
        XCTAssertEqual(PriceAlertWriteFailure(MonacoAPIError.httpStatus(404)), .notFound)
        XCTAssertEqual(PriceAlertWriteFailure(URLError(.notConnectedToInternet)), .other)
        XCTAssertEqual(WatchlistWriteFailure(MonacoAPIError.rejected(status: 409, message: "full")), .full)
        XCTAssertEqual(WatchlistWriteFailure(MonacoAPIError.rejected(status: 409, message: "changed"), isReorder: true), .changed)
        XCTAssertEqual(WatchlistWriteFailure(MonacoAPIError.httpStatus(503)), .other)
    }
}

final class WatchlistAPITests: XCTestCase {
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

    private static func respond(_ request: URLRequest, status: Int, body: String = "") -> (HTTPURLResponse, Data) {
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
        return (response, Data(body.utf8))
    }

    private static func body(of request: URLRequest) -> [String: Any]? {
        var data = request.httpBody
        if data == nil, let stream = request.httpBodyStream {
            stream.open()
            defer { stream.close() }
            var collected = Data()
            var buffer = [UInt8](repeating: 0, count: 1024)
            while stream.hasBytesAvailable {
                let read = stream.read(&buffer, maxLength: buffer.count)
                if read <= 0 { break }
                collected.append(buffer, count: read)
            }
            data = collected
        }
        return data.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
    }

    func testAddSendsPutOnTheSymbolWithAuth() async throws {
        // Arrange
        var method: String?
        var path: String?
        var auth: String?
        MockURLProtocol.requestHandler = { request in
            method = request.httpMethod
            path = request.url?.path
            auth = request.value(forHTTPHeaderField: "Authorization")
            return Self.respond(request, status: 201, body: #"{"symbol":"BRK.Bx","position":2,"createdAt":"2026-09-25T14:30:00Z"}"#)
        }

        // Act
        let entry = try await makeClient().addToWatchlist(symbol: "BRK.Bx")

        // Assert
        XCTAssertEqual(method, "PUT")
        XCTAssertEqual(path, "/v1/me/watchlist/BRK.Bx")
        XCTAssertEqual(auth, "Bearer \(TestFixtures.fixtureSessionToken)")
        XCTAssertEqual(entry.position, 2)
    }

    func testAddingAStockAlreadyWatchedIsNotAnError() async throws {
        MockURLProtocol.requestHandler = { request in
            Self.respond(request, status: 200, body: #"{"symbol":"AAPLx","position":0,"createdAt":"2026-09-25T14:30:00Z"}"#)
        }
        let entry = try await makeClient().addToWatchlist(symbol: "AAPLx")
        XCTAssertEqual(entry.symbol, "AAPLx")
    }

    func testAFullWatchlistSurfacesAs409() async {
        MockURLProtocol.requestHandler = { request in
            Self.respond(request, status: 409, body: #"{"error":"your watchlist is full, remove a stock first","reason":"watchlist_full"}"#)
        }
        do {
            try await makeClient().addToWatchlist(symbol: "AAPLx")
            XCTFail("expected a 409")
        } catch {
            XCTAssertEqual(WatchlistWriteFailure(error), .full)
        }
    }

    func testRemoveAccepts204() async throws {
        var method: String?
        MockURLProtocol.requestHandler = { request in
            method = request.httpMethod
            return Self.respond(request, status: 204)
        }
        try await makeClient().removeFromWatchlist(symbol: "AAPLx")
        XCTAssertEqual(method, "DELETE")
    }

    func testReorderSendsEverySymbolInOrder() async throws {
        // Arrange
        var sent: [String: Any]?
        MockURLProtocol.requestHandler = { request in
            sent = Self.body(of: request)
            return Self.respond(request, status: 200, body: #"{"symbols":["TSLAx","AAPLx"]}"#)
        }

        // Act
        let order = try await makeClient().reorderWatchlist(symbols: ["TSLAx", "AAPLx"])

        // Assert
        XCTAssertEqual(sent?["symbols"] as? [String], ["TSLAx", "AAPLx"])
        XCTAssertEqual(order, ["TSLAx", "AAPLx"])
    }

    func testListAlertsForOneStockSendsTheSymbol() async throws {
        // Arrange
        var query: [URLQueryItem]?
        MockURLProtocol.requestHandler = { request in
            query = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems
            return Self.respond(request, status: 200, body: """
            {"alerts": [{"id": "a1", "symbol": "GOOGLx", "direction": "above", "priceUsdcMicros": 360000000,
                         "active": true, "createdAt": "2026-09-23T10:00:00Z"}],
             "assets": [{"symbol": "GOOGLx", "name": "Alphabet", "solanaMint": "g", "routable": true, "priceUsdcMicros": 352100000}]}
            """)
        }

        // Act
        let response = try await makeClient().listPriceAlerts(symbol: "GOOGLx")

        // Assert
        XCTAssertEqual(query, [URLQueryItem(name: "symbol", value: "GOOGLx")])
        XCTAssertEqual(response.alerts.count, 1)
        XCTAssertEqual(response.assets.first?.priceUsdcMicros, 352_100_000)
    }

    func testCreateAlertPostsTheLine() async throws {
        // Arrange
        var sent: [String: Any]?
        MockURLProtocol.requestHandler = { request in
            sent = Self.body(of: request)
            return Self.respond(request, status: 201, body: """
            {"id": "a9", "symbol": "GOOGLx", "direction": "below", "priceUsdcMicros": 330000000,
             "active": true, "createdAt": "2026-09-25T14:30:00Z"}
            """)
        }

        // Act
        let alert = try await makeClient().createPriceAlert(symbol: "GOOGLx", direction: .below, priceUsdcMicros: 330_000_000)

        // Assert
        XCTAssertEqual(sent?["symbol"] as? String, "GOOGLx")
        XCTAssertEqual(sent?["direction"] as? String, "below")
        XCTAssertEqual((sent?["priceUsdcMicros"] as? NSNumber)?.int64Value, 330_000_000)
        XCTAssertEqual(alert.id, "a9")
    }

    func testCreateAlertRefusalsKeepTheirStatus() async {
        for (status, expected) in [(409, PriceAlertWriteFailure.limitReached), (422, .alreadyReached)] {
            MockURLProtocol.requestHandler = { request in
                Self.respond(request, status: status, body: #"{"error":"no"}"#)
            }
            do {
                _ = try await makeClient().createPriceAlert(symbol: "GOOGLx", direction: .above, priceUsdcMicros: 1)
                XCTFail("expected \(status)")
            } catch {
                XCTAssertEqual(PriceAlertWriteFailure(error), expected)
            }
        }
    }

    func testDeleteAlertHitsItsId() async throws {
        var path: String?
        MockURLProtocol.requestHandler = { request in
            path = request.url?.path
            return Self.respond(request, status: 204)
        }
        try await makeClient().deletePriceAlert(id: "7b0d6c86-21a4-4d0f-9d77-6b1c2b3a4f5e")
        XCTAssertEqual(path, "/v1/me/alerts/7b0d6c86-21a4-4d0f-9d77-6b1c2b3a4f5e")
    }
}

final class AssetDetailWatchStateTests: XCTestCase {
    private let base = """
    "symbol": "AAPLx", "name": "Apple", "solanaMint": "m", "routable": true,
    "liquidity": {"label": "Via Jupiter", "routable": true, "buyProbeUsdcMicros": 1000000}
    """

    func testDetailCarriesTheMembersWatchState() throws {
        let detail = try JSONDecoder().decode(AssetDetailDTO.self, from: Data("{\(base), \"watching\": true, \"alertCount\": 2}".utf8))
        XCTAssertEqual(detail.watching, true)
        XCTAssertEqual(detail.alertCount, 2)
    }

    func testADetailWithoutWatchStateSaysNothing() throws {
        let detail = try JSONDecoder().decode(AssetDetailDTO.self, from: Data("{\(base)}".utf8))
        XCTAssertNil(detail.watching)
        XCTAssertNil(detail.alertCount)
    }
}
