import XCTest
@testable import MonacoCore

final class SettingsAdvancedTests: XCTestCase {
    func testSettingsAdvanced_containsExplorerLinksOnly() {
        // Arrange
        let links = SettingsAdvancedCatalog.explorerLinks

        // Act
        let onlyExplorers = SettingsAdvancedCatalog.containsExplorerLinksOnly()
        let titles = links.map(\.title)

        // Assert
        XCTAssertTrue(onlyExplorers)
        XCTAssertEqual(links.count, 2)
        XCTAssertTrue(titles.allSatisfy { $0.localizedCaseInsensitiveContains("explorer") })
        XCTAssertFalse(titles.contains(where: { $0.localizedCaseInsensitiveContains("wallet") }))
        XCTAssertFalse(titles.contains(where: { $0.localizedCaseInsensitiveContains("private key") }))
    }
}
