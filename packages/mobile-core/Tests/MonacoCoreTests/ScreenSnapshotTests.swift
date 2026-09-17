import XCTest
@testable import MonacoCore

final class ScreenSnapshotTests: XCTestCase {
    func testGroupScreen_snapshot_matchesFixture() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "group_view", withExtension: "json")
        )
        let data = try Data(contentsOf: fixtureURL)
        let dto = try JSONDecoder().decode(GroupViewDTO.self, from: data)
        let expectedURL = try XCTUnwrap(
            Bundle.module.url(forResource: "group_screen_snapshot", withExtension: "txt")
        )
        let expected = try String(contentsOf: expectedURL, encoding: .utf8)

        // Act
        let snapshot = ScreenSnapshotRenderer.groupScreen(from: dto)

        // Assert
        XCTAssertEqual(snapshot, expected)
    }

    func testAppHome_snapshot_matchesFixture() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "home_view", withExtension: "json")
        )
        let data = try Data(contentsOf: fixtureURL)
        let dto = try JSONDecoder().decode(HomeViewDTO.self, from: data)
        let expectedURL = try XCTUnwrap(
            Bundle.module.url(forResource: "app_home_snapshot", withExtension: "txt")
        )
        let expected = try String(contentsOf: expectedURL, encoding: .utf8)

        // Act
        let snapshot = ScreenSnapshotRenderer.appHome(from: dto)

        // Assert
        XCTAssertEqual(snapshot, expected)
    }
}
