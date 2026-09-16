import XCTest
@testable import MonacoCore

final class HomeViewDTOTests: XCTestCase {
    func testHomeViewDTO_decodesGroupAndPeopleBoards() throws {
        // Arrange
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "home_view", withExtension: "json")
        )
        let data = try Data(contentsOf: fixtureURL)

        // Act
        let dto = try JSONDecoder().decode(HomeViewDTO.self, from: data)

        // Assert
        XCTAssertEqual(dto.groups.count, 1)
        XCTAssertEqual(dto.people.count, 1)
        XCTAssertEqual(dto.groups[0].name, "Weekend investors")
        XCTAssertEqual(dto.people[0].displayName, "Alfred")
    }
}
