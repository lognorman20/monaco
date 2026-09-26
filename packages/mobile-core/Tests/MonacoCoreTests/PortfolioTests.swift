import XCTest
@testable import MonacoCore

final class PortfolioDTOTests: XCTestCase {
    private func fixture(_ name: String) throws -> Data {
        let url = try XCTUnwrap(Bundle.module.url(forResource: name, withExtension: "json"))
        return try Data(contentsOf: url)
    }

    func testPortfolioDecodesTheServersShape() throws {
        // Arrange
        let data = try fixture("me_portfolio")

        // Act
        let portfolio = try monacoISO8601JSONDecoder().decode(PortfolioDTO.self, from: data)

        // Assert
        XCTAssertEqual(portfolio.totalUsd, "1250.00")
        XCTAssertEqual(portfolio.cashUsd, "220.00")
        XCTAssertEqual(portfolio.accountBalanceUsd, "42.50")
        XCTAssertEqual(portfolio.percentReturn, "0.136364")
        XCTAssertEqual(portfolio.holdings.map(\.symbol), ["AAPLx", "TSLAx"])
        let apple = portfolio.holdings[0]
        XCTAssertEqual(apple.cabals.map(\.name), ["Sunday Investors", "Semis or bust"])
        XCTAssertEqual(apple.cabals[0].quantity, "2.4")
        XCTAssertEqual(apple.cabals[1].pictureUrl, "https://cdn.test/semis.png")
        XCTAssertNil(apple.logoURL)
        XCTAssertEqual(portfolio.holdings[1].logoURL, URL(string: "https://cdn.test/tsla.png"))
        XCTAssertFalse(portfolio.isEmpty)
    }

    func testHoldingNamesDropTheCatalogueBranding() throws {
        let portfolio = try monacoISO8601JSONDecoder().decode(PortfolioDTO.self, from: fixture("me_portfolio"))

        XCTAssertEqual(portfolio.holdings[0].displayName, "Apple")
        XCTAssertEqual(portfolio.holdings[0].displayTicker, "AAPL")
    }

    func testAnUnreadBalanceStaysUnread() throws {
        // A balance the server could not read is null, not zero.
        let json = #"{"totalUsd":"0.00","cashUsd":"0.00","accountBalanceUsd":null,"dollarPnl":"+0.00","percentReturn":null,"holdings":[],"unvaluedCabals":0}"#

        let portfolio = try JSONDecoder().decode(PortfolioDTO.self, from: Data(json.utf8))

        XCTAssertNil(portfolio.accountBalanceUsd)
        XCTAssertTrue(portfolio.isEmpty)
    }

    func testAnOlderServerWithoutTheOptionalFieldsStillDecodes() throws {
        let json = #"{"totalUsd":"10.00","holdings":[{"symbol":"AAPLx","valueUsd":"10.00","cabals":[]}]}"#

        let portfolio = try JSONDecoder().decode(PortfolioDTO.self, from: Data(json.utf8))

        XCTAssertEqual(portfolio.unvaluedCabals, 0)
        XCTAssertEqual(portfolio.holdings[0].name, "AAPLx")
        XCTAssertEqual(portfolio.holdings[0].kind, .stock)
        XCTAssertNil(portfolio.holdings[0].percentReturn)
    }

    func testCashOnlyIsNotEmpty() {
        let portfolio = PortfolioDTO(totalUsd: "50.00", cashUsd: "50.00", accountBalanceUsd: "0.00", dollarPnl: "+0.00", percentReturn: "0", holdings: [])

        XCTAssertFalse(portfolio.isEmpty, "money sitting in a pot is a portfolio")
    }
}

final class PortfolioAllocationTests: XCTestCase {
    func testStocksLargestFirstThenCashLast() throws {
        // Arrange
        let portfolio = try monacoISO8601JSONDecoder().decode(
            PortfolioDTO.self,
            from: Data(contentsOf: XCTUnwrap(Bundle.module.url(forResource: "me_portfolio", withExtension: "json")))
        )

        // Act
        let segments = PortfolioMath.allocation(for: portfolio)

        // Assert
        XCTAssertEqual(segments.map(\.label), ["AAPL", "TSLA", "Cash"])
        XCTAssertEqual(segments[0].fraction, 0.68, accuracy: 0.0001)
        XCTAssertEqual(segments[1].fraction, 0.144, accuracy: 0.0001)
        XCTAssertEqual(segments[2].fraction, 0.176, accuracy: 0.0001)
        XCTAssertTrue(segments[2].isCash)
    }

    func testANothingPortfolioDrawsNoBar() {
        let empty = PortfolioDTO(totalUsd: "0.00", cashUsd: "0.00", accountBalanceUsd: nil, dollarPnl: "+0.00", percentReturn: nil, holdings: [])

        XCTAssertTrue(PortfolioMath.allocation(for: empty).isEmpty)
    }

    func testSliversAreLeftToTheRows() {
        let holdings = [
            PortfolioHoldingDTO(symbol: "AAPLx", name: "Apple", valueUsd: "999.00", shareOfTotal: "0.999", dollarPnl: "+0.00", percentReturn: nil, cabals: []),
            PortfolioHoldingDTO(symbol: "TSLAx", name: "Tesla", valueUsd: "1.00", shareOfTotal: "0.001", dollarPnl: "+0.00", percentReturn: nil, cabals: []),
        ]
        let portfolio = PortfolioDTO(totalUsd: "1000.00", cashUsd: "0.00", accountBalanceUsd: nil, dollarPnl: "+0.00", percentReturn: nil, holdings: holdings)

        XCTAssertEqual(PortfolioMath.allocation(for: portfolio).map(\.label), ["AAPL"])
    }

    func testPercentLabels() {
        XCTAssertEqual(PortfolioMath.percentLabel(0.68), "68%")
        XCTAssertEqual(PortfolioMath.percentLabel(0.004), "<1%")
    }

    func testQuantityLabels() {
        XCTAssertEqual(PortfolioMath.quantityLabel("2.4", kind: .stock), "2.4 shares")
        XCTAssertEqual(PortfolioMath.quantityLabel("1", kind: .stock), "1 share")
        XCTAssertEqual(PortfolioMath.quantityLabel("not a number", kind: .stock), "not a number")
    }
}

final class HistoryPageDTOTests: XCTestCase {
    func testHistoryPageDecodesTheServersShape() throws {
        // Arrange
        let url = try XCTUnwrap(Bundle.module.url(forResource: "me_transactions", withExtension: "json"))

        // Act
        let page = try monacoISO8601JSONDecoder().decode(HistoryPageDTO.self, from: Data(contentsOf: url))

        // Assert
        XCTAssertEqual(page.items.count, 3)
        XCTAssertNotNil(page.nextCursor)
        let buy = page.items[0]
        XCTAssertEqual(buy.resolvedKind, .buy)
        XCTAssertEqual(buy.resolvedStatus, .done)
        XCTAssertEqual(buy.at, ISO8601DateFormatter().date(from: "2026-09-24T23:31:47Z"))
        XCTAssertEqual(buy.transactionId, buy.id)
        XCTAssertNil(page.items[2].groupId)
    }

    func testTheLastPageHasNoCursor() throws {
        for json in [#"{"items":[],"nextCursor":null}"#, #"{"items":[],"nextCursor":""}"#, #"{"items":[]}"#] {
            let page = try JSONDecoder().decode(HistoryPageDTO.self, from: Data(json.utf8))
            XCTAssertNil(page.nextCursor, json)
        }
    }

    func testUnknownKindsAndStatusesDoNotBreakTheList() {
        XCTAssertEqual(HistoryKind(raw: "airdrop"), .unknown)
        XCTAssertEqual(HistoryKind(raw: " BOT_SELL "), .botSell)
        XCTAssertEqual(HistoryStatus(raw: "reverting"), .pending)
    }
}

final class HistoryRowCopyTests: XCTestCase {
    private let now = ISO8601DateFormatter().date(from: "2026-09-25T15:00:00Z")!

    private func item(
        _ kind: String,
        _ status: String = "done",
        amount: String? = "480.00",
        quantity: String? = nil,
        group: String? = "Sunday Investors",
        symbol: String? = "AAPLx",
        name: String? = "Apple",
        transactionId: String? = nil,
        at: Date? = nil
    ) -> HistoryItemDTO {
        HistoryItemDTO(
            id: "row-\(kind)-\(status)",
            kind: kind,
            status: status,
            groupId: group == nil ? nil : "g1",
            groupName: group,
            symbol: HistoryKind(raw: kind).isTrade ? symbol : nil,
            name: HistoryKind(raw: kind).isTrade ? name : nil,
            amountUsd: amount,
            quantity: quantity,
            at: at ?? now,
            transactionId: transactionId
        )
    }

    func testTitlesSayWhatHappenedForEveryKindAndStatus() {
        let cases: [(String, String, String)] = [
            ("buy", "done", "Bought Apple"),
            ("buy", "pending", "Buying Apple"),
            ("buy", "failed", "Couldn't buy Apple"),
            ("sell", "done", "Sold Apple"),
            ("bot_buy", "done", "Agent bought Apple"),
            ("bot_sell", "pending", "Agent selling Apple"),
            ("fund", "done", "Added money to Sunday Investors"),
            ("fund", "pending", "Adding money to Sunday Investors"),
            ("fund", "failed", "Couldn't add money to Sunday Investors"),
            ("cash_out", "done", "Cashed out of Sunday Investors"),
            ("cash_out", "pending", "Cashing out of Sunday Investors"),
            ("withdrawal", "done", "Cashed out"),
            ("deposit", "done", "Added money"),
            ("airdrop", "done", "Money moved"),
        ]
        for (kind, status, want) in cases {
            XCTAssertEqual(HistoryRowCopy.title(for: item(kind, status)), want, "\(kind)/\(status)")
        }
    }

    func testStockNamesComeFromTheCatalogueWithoutBranding() {
        let row = item("buy", name: "Tesla xStock")
        XCTAssertEqual(HistoryRowCopy.title(for: HistoryItemDTO(
            id: "t", kind: "buy", status: "done", groupName: "g", symbol: "TSLAx", name: "Tesla xStock", amountUsd: "1", at: now
        )), "Bought Tesla")
        XCTAssertEqual(HistoryRowCopy.stockName(row), "Apple")
    }

    func testCaptionsSayWhereTheMoneyWasNeverRepeatingTheTitle() {
        XCTAssertEqual(HistoryRowCopy.caption(for: item("buy")), "Sunday Investors")
        XCTAssertEqual(HistoryRowCopy.caption(for: item("fund")), "From your account")
        XCTAssertEqual(HistoryRowCopy.caption(for: item("cash_out")), "To your account")
        XCTAssertEqual(HistoryRowCopy.caption(for: item("withdrawal", group: nil)), "From your account")
        XCTAssertEqual(HistoryRowCopy.caption(for: item("buy", group: nil)), "a cabal")
    }

    func testStatusIsOnlySaidWhenTheMoneyHasNotMoved() {
        XCTAssertNil(HistoryRowCopy.statusLabel(for: item("buy", "done")))
        XCTAssertEqual(HistoryRowCopy.statusLabel(for: item("buy", "pending")), "Pending")
        XCTAssertEqual(HistoryRowCopy.statusLabel(for: item("buy", "failed")), "Failed")
    }

    func testMoneyInIsGreenWithAPlusOnlyOnceItLanded() {
        XCTAssertEqual(HistoryRowCopy.amountTone(for: item("fund", "done")), .moneyIn)
        XCTAssertEqual(HistoryRowCopy.amountText(for: item("fund", "done", amount: "600.00")), "+$600.00")
        XCTAssertEqual(HistoryRowCopy.amountTone(for: item("fund", "pending")), .plain)
        XCTAssertEqual(HistoryRowCopy.amountText(for: item("fund", "pending", amount: "50.00")), "$50.00")
        XCTAssertEqual(HistoryRowCopy.amountTone(for: item("buy", "done")), .plain)
        XCTAssertEqual(HistoryRowCopy.amountText(for: item("buy", amount: "1480.00")), "$1,480.00")
        XCTAssertEqual(HistoryRowCopy.amountTone(for: item("cash_out", "failed")), .failed)
    }

    func testASellWithoutProceedsHasNoDollarFigure() {
        XCTAssertNil(HistoryRowCopy.amountText(for: item("sell", "pending", amount: nil)))
        XCTAssertNil(HistoryRowCopy.amountText(for: item("sell", "pending", amount: "")))
    }

    func testQuantityOnlyOnFilledTrades() {
        XCTAssertEqual(HistoryRowCopy.quantityText(for: item("buy", quantity: "2.4")), "2.4 shares")
        XCTAssertNil(HistoryRowCopy.quantityText(for: item("buy", "pending", quantity: nil)))
        XCTAssertNil(HistoryRowCopy.quantityText(for: item("fund", quantity: "5")))
    }

    func testReceiptsOpenWhereOneExists() {
        XCTAssertEqual(HistoryRowCopy.receipt(for: item("buy", transactionId: "tx-1")), .transaction("tx-1"))
        XCTAssertEqual(HistoryRowCopy.receipt(for: item("bot_sell", transactionId: "tx-2")), .transaction("tx-2"))
        XCTAssertNil(HistoryRowCopy.receipt(for: item("buy", transactionId: nil)))
        XCTAssertEqual(HistoryRowCopy.receipt(for: item("fund")), .deposit("row-fund-done"))
        XCTAssertNil(HistoryRowCopy.receipt(for: item("cash_out")))
        XCTAssertNil(HistoryRowCopy.receipt(for: item("withdrawal")))
    }

    func testFilterTitlesAreTheChips() {
        XCTAssertEqual(HistoryFilter.allCases.map(\.title), ["All", "Money in", "Cash outs", "Buys", "Sells"])
        XCTAssertEqual(HistoryFilter.allCases.map(\.rawValue), ["all", "money_in", "cash_out", "buy", "sell"])
    }

    func testAHoldingSaysWhereItSits() {
        XCTAssertEqual(PortfolioCopy.cabalsSummary(["Sunday Investors"]), "Sunday Investors")
        XCTAssertEqual(PortfolioCopy.cabalsSummary(["Sunday Investors", "Semis or bust"]), "2 cabals")
        XCTAssertEqual(PortfolioCopy.cabalsSummary([]), "")
        XCTAssertEqual(PortfolioCopy.unvalued(0), nil)
        XCTAssertEqual(PortfolioCopy.unvalued(1), "1 cabal couldn't be valued just now")
    }

    func testEveryStringPassesTheCopyAudit() {
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean(PortfolioCopy.auditedStrings))
        for string in PortfolioCopy.auditedStrings {
            XCTAssertFalse(string.contains("P&L"), string)
        }
    }
}

final class HistoryDayGroupingTests: XCTestCase {
    private var calendar: Calendar = {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "America/New_York")!
        return calendar
    }()

    private let now = ISO8601DateFormatter().date(from: "2026-09-25T15:00:00Z")!

    private func row(_ id: String, _ iso: String) -> HistoryItemDTO {
        HistoryItemDTO(id: id, kind: "fund", status: "done", amountUsd: "1", at: ISO8601DateFormatter().date(from: iso)!)
    }

    func testRowsGroupUnderTodayYesterdayThenTheDate() {
        // Arrange
        let items = [
            row("a", "2026-09-25T14:00:00Z"),
            row("b", "2026-09-25T05:00:00Z"),
            row("c", "2026-09-24T20:00:00Z"),
            row("d", "2026-09-22T12:00:00Z"),
            row("e", "2025-12-31T18:00:00Z"),
        ]

        // Act
        let sections = HistoryDayGrouping.sections(items, now: now, calendar: calendar)

        // Assert
        XCTAssertEqual(sections.map(\.title), ["Today", "Yesterday", "Sep 22", "Dec 31, 2025"])
        XCTAssertEqual(sections.map { $0.items.map(\.id) }, [["a", "b"], ["c"], ["d"], ["e"]])
    }

    /// The day is the member's, not UTC's: 01:00 UTC on the 25th is still the 24th in New York.
    func testTheDayIsTheMembersLocalDay() {
        let sections = HistoryDayGrouping.sections([row("late", "2026-09-25T01:00:00Z")], now: now, calendar: calendar)

        XCTAssertEqual(sections.first?.title, "Yesterday")
    }

    func testADaySpanningTwoPagesStaysOneSection() {
        let firstPage = [row("a", "2026-09-22T20:00:00Z")]
        let secondPage = [row("b", "2026-09-22T13:00:00Z")]

        let sections = HistoryDayGrouping.sections(firstPage + secondPage, now: now, calendar: calendar)

        XCTAssertEqual(sections.count, 1)
        XCTAssertEqual(sections[0].items.count, 2)
    }
}

final class PortfolioAPITests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    private func client() -> MonacoAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: URLSession(configuration: configuration),
            accessTokenProvider: { TestFixtures.fixtureSessionToken }
        )
    }

    private func respond(_ data: Data, status: Int = 200, capture: @escaping (URLRequest) -> Void = { _ in }) {
        MockURLProtocol.requestHandler = { request in
            capture(request)
            return (HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: nil)!, data)
        }
    }

    func testGetPortfolioCallsTheRouteWithTheSessionToken() async throws {
        // Arrange
        var captured: URLRequest?
        let data = try Data(contentsOf: XCTUnwrap(Bundle.module.url(forResource: "me_portfolio", withExtension: "json")))
        respond(data) { captured = $0 }

        // Act
        let portfolio = try await client().getPortfolio()

        // Assert
        XCTAssertEqual(captured?.url?.path, "/v1/me/portfolio")
        XCTAssertEqual(captured?.value(forHTTPHeaderField: "Authorization"), "Bearer \(TestFixtures.fixtureSessionToken)")
        XCTAssertEqual(portfolio.holdings.count, 2)
    }

    func testGetHistorySendsTheFilterCursorAndLimit() async throws {
        // Arrange
        var query: [URLQueryItem]?
        respond(Data(#"{"items":[],"nextCursor":null}"#.utf8)) {
            query = URLComponents(url: $0.url!, resolvingAgainstBaseURL: false)?.queryItems
        }

        // Act
        _ = try await client().getHistory(filter: .cashOut, cursor: "abc", limit: 30)

        // Assert
        XCTAssertEqual(query, [
            URLQueryItem(name: "type", value: "cash_out"),
            URLQueryItem(name: "limit", value: "30"),
            URLQueryItem(name: "cursor", value: "abc"),
        ])
    }

    func testTheFirstPageSendsNoCursorAndTheLimitIsClamped() {
        let items = MonacoAPIClient.historyQuery(filter: .all, cursor: "  ", limit: 500)

        XCTAssertEqual(items, [URLQueryItem(name: "type", value: "all"), URLQueryItem(name: "limit", value: "100")])
    }

    func testExportReturnsTheFilesBytes() async throws {
        // Arrange
        let csv = "date,kind,status,cabal,stock,amount_usd,quantity,id\n"
        var path: String?
        respond(Data(csv.utf8)) { path = $0.url?.path }

        // Act
        let data = try await client().exportHistoryCSV(filter: .buy)

        // Assert
        XCTAssertEqual(path, "/v1/me/transactions/export.csv")
        XCTAssertEqual(String(data: data, encoding: .utf8), csv)
    }

    func testAFailedReadThrowsTheStatus() async {
        respond(Data(#"{"error":"internal server error"}"#.utf8), status: 500)

        do {
            _ = try await client().getPortfolio()
            XCTFail("expected a failure")
        } catch let error as MonacoAPIError {
            XCTAssertEqual(error.statusCode, 500)
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }
}
