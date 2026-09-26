import XCTest
@testable import MonacoCore

final class SettingsDTOTests: XCTestCase {
    func testPreferences_decodesTheWholeDocument() throws {
        // Arrange
        let json = #"{"notifications":{"proposals":true,"results":false,"chat":true,"money":false}}"#

        // Act
        let prefs = try JSONDecoder().decode(PreferencesDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertEqual(prefs.notifications, NotificationPreferencesDTO(proposals: true, results: false, chat: true, money: false))
    }

    func testPreferences_missingKeysReadAsOn() throws {
        // Arrange: a server that predates a category, or an empty document.
        let partial = #"{"notifications":{"chat":false}}"#
        let empty = #"{}"#

        // Act
        let fromPartial = try JSONDecoder().decode(PreferencesDTO.self, from: Data(partial.utf8))
        let fromEmpty = try JSONDecoder().decode(PreferencesDTO.self, from: Data(empty.utf8))

        // Assert
        XCTAssertEqual(fromPartial.notifications, NotificationPreferencesDTO(proposals: true, results: true, chat: false, money: true))
        XCTAssertEqual(fromEmpty, .defaults)
    }

    func testNotificationPreferences_subscriptReadsAndWritesEachCategory() {
        // Arrange
        var prefs = NotificationPreferencesDTO.allOn

        // Act
        for category in NotificationCategory.allCases {
            prefs[category] = false
        }

        // Assert
        XCTAssertEqual(prefs, NotificationPreferencesDTO(proposals: false, results: false, chat: false, money: false))
        XCTAssertEqual(NotificationCategory.allCases.map(\.rawValue), ["proposals", "results", "chat", "money"])
    }

    func testDeletionCheck_decodesEveryBlockerKind() throws {
        // Arrange
        let json = """
        {"canDelete":false,"blockers":[
          {"kind":"cabal_slice","groupId":"g-1","groupName":"Sunday Investors","valueUsd":"245.12"},
          {"kind":"cash_out_pending","groupId":"g-2","groupName":"Semis or Bust","valueUsd":"12.00"},
          {"kind":"transfer_pending","valueUsd":"5.50"},
          {"kind":"account_balance","valueUsd":"30.00"},
          {"kind":"cabal_slice","groupId":"g-3","groupName":"Night Owls","valueUsd":null},
          {"kind":"loan_outstanding","valueUsd":"1.00"}
        ]}
        """

        // Act
        let check = try JSONDecoder().decode(DeletionCheckDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertFalse(check.canDelete)
        XCTAssertEqual(check.blockers.map(\.kind), [.cabalSlice, .cashOutPending, .transferPending, .accountBalance, .cabalSlice, .unknown("loan_outstanding")])
        XCTAssertEqual(check.blockers[0], DeletionBlockerDTO(kind: .cabalSlice, groupId: "g-1", groupName: "Sunday Investors", valueUsd: "245.12"))
        XCTAssertNil(check.blockers[2].groupId)
        XCTAssertNil(check.blockers[4].valueUsd, "an unpriced slice has no value, not zero")
    }

    func testDeletionCheck_blockersOverrideAContradictoryFlag() throws {
        // Arrange
        let json = #"{"canDelete":true,"blockers":[{"kind":"account_balance","valueUsd":"1.00"}]}"#

        // Act
        let check = try JSONDecoder().decode(DeletionCheckDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertFalse(check.canDelete)
    }

    func testDeletionCheck_emptyAccountCanDelete() throws {
        // Act
        let check = try JSONDecoder().decode(DeletionCheckDTO.self, from: Data(#"{"canDelete":true,"blockers":[]}"#.utf8))

        // Assert
        XCTAssertTrue(check.canDelete)
        XCTAssertTrue(check.blockers.isEmpty)
    }
}

final class SettingsAPITests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    private func makeClient() -> MonacoAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: URLSession(configuration: configuration),
            accessTokenProvider: { TestFixtures.fixtureSessionToken }
        )
    }

    private static func respond(_ request: URLRequest, status: Int, body: String) -> (HTTPURLResponse, Data) {
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
        return (response, Data(body.utf8))
    }

    private static func body(of request: URLRequest) -> Data? {
        if let body = request.httpBody { return body }
        guard let stream = request.httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 1024)
        while stream.hasBytesAvailable {
            let read = stream.read(&buffer, maxLength: buffer.count)
            guard read > 0 else { break }
            data.append(buffer, count: read)
        }
        return data
    }

    func testGetPreferences_getsTheRouteWithTheToken() async throws {
        // Arrange
        var captured: URLRequest?
        MockURLProtocol.requestHandler = { request in
            captured = request
            return Self.respond(request, status: 200, body: #"{"notifications":{"proposals":true,"results":true,"chat":false,"money":true}}"#)
        }

        // Act
        let prefs = try await makeClient().getPreferences()

        // Assert
        XCTAssertEqual(captured?.httpMethod, "GET")
        XCTAssertEqual(captured?.url?.path, "/v1/me/preferences")
        XCTAssertEqual(captured?.value(forHTTPHeaderField: "Authorization"), "Bearer \(TestFixtures.fixtureSessionToken)")
        XCTAssertFalse(prefs.notifications.chat)
    }

    func testUpdateNotificationPreferences_sendsOnlyTheChangedSwitch() async throws {
        // Arrange
        var captured: URLRequest?
        var sentBody: Data?
        MockURLProtocol.requestHandler = { request in
            captured = request
            sentBody = Self.body(of: request)
            return Self.respond(request, status: 200, body: #"{"notifications":{"proposals":true,"results":true,"chat":false,"money":true}}"#)
        }

        // Act
        let prefs = try await makeClient().updateNotificationPreferences([.chat: false])

        // Assert
        XCTAssertEqual(captured?.httpMethod, "PATCH")
        XCTAssertEqual(captured?.url?.path, "/v1/me/preferences")
        XCTAssertEqual(sentBody.flatMap { String(data: $0, encoding: .utf8) }, #"{"notifications":{"chat":false}}"#)
        XCTAssertEqual(prefs.notifications, NotificationPreferencesDTO(proposals: true, results: true, chat: false, money: true))
    }

    func testUpdateNotificationPreferences_keepsTheServersReasonOnA400() async {
        // Arrange
        MockURLProtocol.requestHandler = { request in
            Self.respond(request, status: 400, body: #"{"error":"unknown preference \"notifications.later\"","reason":"invalid_preferences"}"#)
        }

        // Act
        do {
            _ = try await makeClient().updateNotificationPreferences([.chat: true])
            XCTFail("expected a rejection")
        } catch let error as MonacoAPIError {
            // Assert
            XCTAssertEqual(error.statusCode, 400)
            guard case .rejected(_, let message, _) = error else {
                return XCTFail("error = \(error), want .rejected")
            }
            XCTAssertTrue(message.contains("notifications.later"))
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    func testGetDeletionCheck_decodesBlockers() async throws {
        // Arrange
        var captured: URLRequest?
        MockURLProtocol.requestHandler = { request in
            captured = request
            return Self.respond(request, status: 200, body: #"{"canDelete":false,"blockers":[{"kind":"account_balance","valueUsd":"30.00"}]}"#)
        }

        // Act
        let check = try await makeClient().getDeletionCheck()

        // Assert
        XCTAssertEqual(captured?.url?.path, "/v1/me/deletion-check")
        XCTAssertEqual(check.blockers, [DeletionBlockerDTO(kind: .accountBalance, valueUsd: "30.00")])
    }

    func testDeleteAccount_success() async throws {
        // Arrange
        var captured: URLRequest?
        MockURLProtocol.requestHandler = { request in
            captured = request
            return Self.respond(request, status: 200, body: #"{"deletedAt":"2026-09-25T14:30:00Z"}"#)
        }

        // Act
        let outcome = try await makeClient().deleteAccount()

        // Assert
        XCTAssertEqual(captured?.httpMethod, "DELETE")
        XCTAssertEqual(captured?.url?.path, "/v1/me")
        XCTAssertEqual(outcome, .deleted(deletedAt: MonacoISO8601.date(from: "2026-09-25T14:30:00Z")))
    }

    func testDeleteAccount_409IsTheBlockersNotAnError() async throws {
        // Arrange
        MockURLProtocol.requestHandler = { request in
            Self.respond(request, status: 409, body: """
            {"error":"Move your money out before deleting your account.","reason":"account_not_empty","canDelete":false,
             "blockers":[{"kind":"cabal_slice","groupId":"g-1","groupName":"Sunday Investors","valueUsd":"245.12"}]}
            """)
        }

        // Act
        let outcome = try await makeClient().deleteAccount()

        // Assert
        let expected = DeletionCheckDTO(canDelete: false, blockers: [
            DeletionBlockerDTO(kind: .cabalSlice, groupId: "g-1", groupName: "Sunday Investors", valueUsd: "245.12"),
        ])
        XCTAssertEqual(outcome, .blocked(expected))
    }

    func testDeleteAccount_serverErrorThrows() async {
        // Arrange
        MockURLProtocol.requestHandler = { request in
            Self.respond(request, status: 503, body: #"{"error":"could not check your account balance, try again shortly"}"#)
        }

        // Act
        do {
            _ = try await makeClient().deleteAccount()
            XCTFail("expected an error")
        } catch let error as MonacoAPIError {
            // Assert
            XCTAssertEqual(error.statusCode, 503)
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }
}

final class SettingsRulesTests: XCTestCase {
    func testPhoneMask_keepsCountryCodeAndLastFour() {
        let cases: [(String, String)] = [
            ("+14155557177", "+1 ••• ••• 7177"),
            ("+447700900123", "+44 ••• ••• 0123"),
            ("+35312345678", "+353 ••• ••• 5678"),
            ("+79161234567", "+7 ••• ••• 4567"),
            ("4155557177", "••• ••• 7177"),
            ("+1234", "••• ••• ••••"),
        ]
        for (raw, expected) in cases {
            XCTAssertEqual(SignInIdentity.phone(raw).masked, expected, raw)
        }
    }

    func testEmailMask_keepsFirstLetterAndDomain() {
        XCTAssertEqual(SignInIdentity.email("logan@monacolabs.xyz").masked, "l•••@monacolabs.xyz")
        XCTAssertEqual(SignInIdentity.email("a@b.co").masked, "a•••@b.co")
        XCTAssertEqual(SignInIdentity.email("@nope.com").masked, "•••")
        XCTAssertEqual(SignInIdentity.email("nodomain@").masked, "•••")
    }

    func testIdentityLabels() {
        XCTAssertEqual(SignInIdentity.phone("+14155557177").label, "Phone")
        XCTAssertEqual(SignInIdentity.email("x@y.z").label, "Email")
    }

    func testLockTimeout_locksOnceTheDelayHasPassed() {
        // Arrange
        let away = Date(timeIntervalSince1970: 1_000_000)

        // Assert
        XCTAssertTrue(AppLockTimeout.immediately.requiresUnlock(backgroundedAt: away, now: away))
        XCTAssertFalse(AppLockTimeout.oneMinute.requiresUnlock(backgroundedAt: away, now: away.addingTimeInterval(59)))
        XCTAssertTrue(AppLockTimeout.oneMinute.requiresUnlock(backgroundedAt: away, now: away.addingTimeInterval(60)))
        XCTAssertFalse(AppLockTimeout.fifteenMinutes.requiresUnlock(backgroundedAt: away, now: away.addingTimeInterval(899)))
        XCTAssertTrue(AppLockTimeout.fiveMinutes.requiresUnlock(backgroundedAt: away, now: away.addingTimeInterval(301)))
    }

    func testLockTimeout_clockMovedBackLocks() {
        let away = Date(timeIntervalSince1970: 1_000_000)
        XCTAssertTrue(AppLockTimeout.fifteenMinutes.requiresUnlock(backgroundedAt: away, now: away.addingTimeInterval(-5)))
    }

    func testDeleteConfirmation_needsTheWordInCapitals() {
        XCTAssertTrue(DeleteConfirmation.matches("DELETE"))
        XCTAssertTrue(DeleteConfirmation.matches("  DELETE \n"))
        XCTAssertFalse(DeleteConfirmation.matches("delete"))
        XCTAssertFalse(DeleteConfirmation.matches("DELET"))
        XCTAssertFalse(DeleteConfirmation.matches(""))
    }

    func testSupportEmail_carriesVersionAndAccount() throws {
        // Act
        let url = SettingsLinks.supportEmail(version: "1.4.0", build: "212", userId: "3f0c9a52-user")
        let components = try XCTUnwrap(URLComponents(url: url, resolvingAgainstBaseURL: false))
        let body = components.queryItems?.first { $0.name == "body" }?.value ?? ""

        // Assert
        XCTAssertEqual(components.scheme, "mailto")
        XCTAssertEqual(components.path, "support@monacolabs.xyz")
        XCTAssertTrue(body.contains("Monaco 1.4.0 (212)"))
        XCTAssertTrue(body.contains("Account: 3f0c9a52-user"))
    }

    func testVersionLabel() {
        XCTAssertEqual(SettingsLinks.versionLabel(version: "1.4.0", build: "212"), "1.4.0 (212)")
        XCTAssertEqual(SettingsLinks.versionLabel(version: "1.4.0", build: nil), "1.4.0")
        XCTAssertEqual(SettingsLinks.versionLabel(version: nil, build: "7"), "Unknown (7)")
    }

    func testBlockerCopy_namesTheWayOut() {
        let slice = DeletionBlockerDTO(kind: .cabalSlice, groupId: "g", groupName: "Sunday Investors", valueUsd: "1.00")
        XCTAssertEqual(SettingsCopy.blocker(slice), .init(title: "Sunday Investors", wayOut: "Cash out from Sunday Investors"))
        let balance = DeletionBlockerDTO(kind: .accountBalance, valueUsd: "1.00")
        XCTAssertEqual(SettingsCopy.blocker(balance).wayOut, "Cash out your balance")
    }

    func testSettingsCopy_passesTheProductCopyAudit() {
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean(SettingsCopy.auditedStrings))
        for string in SettingsCopy.auditedStrings {
            XCTAssertTrue(MainFlowCopyAudit.stringsAreClean([string]), "forbidden term in: \(string)")
        }
    }
}
