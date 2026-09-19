import XCTest
@testable import MonacoCore

final class GroupsTabDTOTests: XCTestCase {

    // MARK: - Fixture decoding

    func testGroupSearchResponseDTO_decodesFixturePayload() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "groups_search", withExtension: "json")
        )
        let data = try Data(contentsOf: fixtureURL)

        // Act
        let dto = try monacoISO8601JSONDecoder().decode(GroupSearchResponseDTO.self, from: data)

        // Assert
        XCTAssertEqual(dto.groups.count, 2)
        let joined = dto.groups[0]
        XCTAssertEqual(joined.groupID, "3f9a1b2c-4d5e-4f6a-8b7c-9d0e1f2a3b4c")
        XCTAssertEqual(joined.name, "Weekend investors")
        XCTAssertEqual(joined.memberCount, 8)
        XCTAssertEqual(joined.potValueUsd, "548.20")
        XCTAssertEqual(joined.percentReturn, "0.124")
        XCTAssertEqual(joined.dollarPnl, "+48.20")
        XCTAssertTrue(joined.isJoined)
        XCTAssertEqual(joined.joinMode, .open)

        let requestMode = dto.groups[1]
        XCTAssertEqual(requestMode.name, "Rent money")
        XCTAssertNil(requestMode.percentReturn)
        XCTAssertFalse(requestMode.isJoined)
        XCTAssertEqual(requestMode.joinMode, .request)

        XCTAssertEqual(dto.nextCursor, "eyJvZmZzZXQiOjIwfQ==")
    }

    func testGroupLeaderboardResponseDTO_decodesFixturePayload() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "groups_leaderboard", withExtension: "json")
        )
        let data = try Data(contentsOf: fixtureURL)

        // Act
        let dto = try monacoISO8601JSONDecoder().decode(GroupLeaderboardResponseDTO.self, from: data)

        // Assert
        XCTAssertEqual(dto.groups.count, 2)
        XCTAssertEqual(dto.groups[0].rank, 1)
        XCTAssertEqual(dto.groups[0].name, "Weekend investors")
        XCTAssertEqual(dto.groups[1].rank, 2)
        XCTAssertEqual(dto.groups[1].name, "Rent money")
        XCTAssertEqual(dto.groups[1].joinMode, .request)
    }

    func testMyGroupsPnLHistoryDTO_decodesFixturePayload() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "groups_pnl_history", withExtension: "json")
        )
        let data = try Data(contentsOf: fixtureURL)

        // Act
        let dto = try monacoISO8601JSONDecoder().decode(MyGroupsPnLHistoryDTO.self, from: data)

        // Assert
        XCTAssertEqual(dto.range, "1M")
        XCTAssertEqual(dto.series.count, 2)

        let withPoints = dto.series[0]
        XCTAssertEqual(withPoints.groupID, "3f9a1b2c-4d5e-4f6a-8b7c-9d0e1f2a3b4c")
        XCTAssertEqual(withPoints.points.count, 4)
        XCTAssertEqual(withPoints.points[1].dollarPnl, "-3.10")
        XCTAssertEqual(withPoints.points[1].chartValue, -3.10, accuracy: 0.0001)

        let empty = dto.series[1]
        XCTAssertEqual(empty.name, "Rent money")
        XCTAssertTrue(empty.points.isEmpty)
    }

    func testGroupPnLSeriesDTO_decodesSingleGroupFixturePayload() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "group_pnl_history", withExtension: "json")
        )
        let data = try Data(contentsOf: fixtureURL)

        // Act
        let dto = try monacoISO8601JSONDecoder().decode(GroupPnLSeriesDTO.self, from: data)

        // Assert
        XCTAssertEqual(dto.groupID, "3f9a1b2c-4d5e-4f6a-8b7c-9d0e1f2a3b4c")
        XCTAssertEqual(dto.name, "Weekend investors")
        XCTAssertEqual(dto.range, "1M")
        XCTAssertEqual(dto.points.count, 2)
    }

    // MARK: - GroupJoinMode

    func testGroupJoinMode_unknownRawValue_decodesAsRequest() throws {
        // Arrange
        let json = #"{"joinMode":"password"}"#
        struct Wrapper: Decodable { let joinMode: GroupJoinMode }

        // Act
        let wrapper = try JSONDecoder().decode(Wrapper.self, from: Data(json.utf8))

        // Assert
        XCTAssertEqual(wrapper.joinMode, .request)
    }

    func testGroupJoinMode_knownRawValues_decodeAsExpected() throws {
        struct Wrapper: Decodable { let joinMode: GroupJoinMode }

        let open = try JSONDecoder().decode(Wrapper.self, from: Data(#"{"joinMode":"open"}"#.utf8))
        let request = try JSONDecoder().decode(Wrapper.self, from: Data(#"{"joinMode":"request"}"#.utf8))

        XCTAssertEqual(open.joinMode, .open)
        XCTAssertEqual(request.joinMode, .request)
    }

    // MARK: - GroupSearchResponseDTO.nextCursor

    func testGroupSearchResponseDTO_nullNextCursor_decodesAsNil() throws {
        // Arrange
        let json = #"{"groups":[],"nextCursor":null}"#

        // Act
        let dto = try monacoISO8601JSONDecoder().decode(GroupSearchResponseDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertNil(dto.nextCursor)
        XCTAssertTrue(dto.groups.isEmpty)
    }

    // MARK: - Date decoding

    func testGroupPnLPointDTO_decodesExactUTCInstant_withAndWithoutFraction() throws {
        // Arrange
        let withoutFraction = #"""
        {"at":"2026-09-01T15:00:00Z","potValueUsd":"100.00","netInUsd":"100.00","dollarPnl":"+0.00"}
        """#
        let withFraction = #"""
        {"at":"2026-09-19T15:00:00.500Z","potValueUsd":"148.20","netInUsd":"100.00","dollarPnl":"+48.20"}
        """#

        // Act
        let decoder = monacoISO8601JSONDecoder()
        let plain = try decoder.decode(GroupPnLPointDTO.self, from: Data(withoutFraction.utf8))
        let fractional = try decoder.decode(GroupPnLPointDTO.self, from: Data(withFraction.utf8))

        // Assert
        var utcCalendar = Calendar(identifier: .gregorian)
        utcCalendar.timeZone = TimeZone(identifier: "UTC")!

        let plainComponents = utcCalendar.dateComponents([.year, .month, .day, .hour, .minute, .second], from: plain.at)
        XCTAssertEqual(plainComponents.year, 2026)
        XCTAssertEqual(plainComponents.month, 9)
        XCTAssertEqual(plainComponents.day, 1)
        XCTAssertEqual(plainComponents.hour, 15)
        XCTAssertEqual(plainComponents.minute, 0)
        XCTAssertEqual(plainComponents.second, 0)

        XCTAssertEqual(fractional.at.timeIntervalSince1970, plain.at.timeIntervalSince1970 + (18 * 24 * 3600) + 0.5, accuracy: 0.001)
    }

    // MARK: - chartValue

    func testGroupPnLPointDTO_chartValue_parsesSignedDollarPnl() {
        // Arrange / Act / Assert
        XCTAssertEqual(makePoint(dollarPnl: "+15.00").chartValue, 15.00, accuracy: 0.0001)
        XCTAssertEqual(makePoint(dollarPnl: "-3.10").chartValue, -3.10, accuracy: 0.0001)
        XCTAssertEqual(makePoint(dollarPnl: "garbage").chartValue, 0)
    }

    private func makePoint(dollarPnl: String) -> GroupPnLPointDTO {
        GroupPnLPointDTO(at: Date(), potValueUsd: "0.00", netInUsd: "0.00", dollarPnl: dollarPnl)
    }

    // MARK: - GroupDiscoveryDestination

    func testGroupDiscoveryDestination_isJoined_alwaysDetail() {
        // Arrange / Act / Assert
        XCTAssertEqual(GroupDiscoveryDestination(isJoined: true, joinMode: .open), .detail)
        XCTAssertEqual(GroupDiscoveryDestination(isJoined: true, joinMode: .request), .detail)
    }

    func testGroupDiscoveryDestination_notJoined_openMode_isJoin() {
        // Arrange / Act / Assert
        XCTAssertEqual(GroupDiscoveryDestination(isJoined: false, joinMode: .open), .join)
    }

    func testGroupDiscoveryDestination_notJoined_requestMode_isRequestToJoin() {
        // Arrange / Act / Assert
        XCTAssertEqual(GroupDiscoveryDestination(isJoined: false, joinMode: .request), .requestToJoin)
    }

    // MARK: - GroupPnLChartModel.drawable

    func testGroupPnLChartModel_drawable_filtersOutSeriesWithFewerThanTwoPoints() {
        // Arrange
        let zeroPoints = makeSeries(groupID: "a", pointCount: 0)
        let onePoint = makeSeries(groupID: "b", pointCount: 1)
        let twoPoints = makeSeries(groupID: "c", pointCount: 2)
        let fourPoints = makeSeries(groupID: "d", pointCount: 4)

        // Act
        let drawable = GroupPnLChartModel.drawable([zeroPoints, onePoint, twoPoints, fourPoints])

        // Assert
        XCTAssertEqual(drawable.map(\.groupID), ["c", "d"])
    }

    private func makeSeries(groupID: String, pointCount: Int) -> GroupPnLSeriesDTO {
        let points = (0..<pointCount).map { offset in
            GroupPnLPointDTO(
                at: Date(timeIntervalSince1970: TimeInterval(offset)),
                potValueUsd: "1.00",
                netInUsd: "1.00",
                dollarPnl: "+0.00"
            )
        }
        return GroupPnLSeriesDTO(groupID: groupID, name: groupID, range: "1M", points: points)
    }

    // MARK: - GroupSearchQuery.normalized

    func testGroupSearchQuery_normalized_emptyString_isNil() {
        // Arrange / Act / Assert
        XCTAssertNil(GroupSearchQuery.normalized(""))
    }

    func testGroupSearchQuery_normalized_singleTrimmedCharacter_isNil() {
        // Arrange / Act / Assert
        XCTAssertNil(GroupSearchQuery.normalized(" a "))
    }

    func testGroupSearchQuery_normalized_twoCharacters_isAccepted() {
        // Arrange / Act / Assert
        XCTAssertEqual(GroupSearchQuery.normalized("ab"), "ab")
    }

    func testGroupSearchQuery_normalized_trimsWhitespace() {
        // Arrange / Act / Assert
        XCTAssertEqual(GroupSearchQuery.normalized("  weekend  "), "weekend")
    }

    func testGroupSearchQuery_normalized_cappedAt64Characters() {
        // Arrange
        let raw = String(repeating: "x", count: 100)

        // Act
        let normalized = GroupSearchQuery.normalized(raw)

        // Assert
        XCTAssertEqual(normalized?.count, 64)
        XCTAssertEqual(normalized, String(repeating: "x", count: 64))
    }

    func testSignedUsdFormatter_formatsGainsLossesAndZero() {
        XCTAssertEqual(SignedUsdFormatter.format("+48.20"), "+$48.20")
        XCTAssertEqual(SignedUsdFormatter.format("-3.10"), "-$3.10")
        XCTAssertEqual(SignedUsdFormatter.format("+1234.5"), "+$1,234.50")
        XCTAssertEqual(SignedUsdFormatter.format("-0.00"), "+$0.00")
    }

    func testSignedUsdFormatter_isLossOnlyBelowZero() {
        XCTAssertTrue(SignedUsdFormatter.isLoss("-3.10"))
        XCTAssertFalse(SignedUsdFormatter.isLoss("+3.10"))
        XCTAssertFalse(SignedUsdFormatter.isLoss("-0.00"))
        XCTAssertFalse(SignedUsdFormatter.isLoss("garbage"))
    }
}
