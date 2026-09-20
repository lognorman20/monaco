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
        XCTAssertTrue(titles.allSatisfy { $0.localizedCaseInsensitiveContains("basescan") })
        XCTAssertFalse(titles.contains(where: { $0.localizedCaseInsensitiveContains("wallet") }))
        XCTAssertFalse(titles.contains(where: { $0.localizedCaseInsensitiveContains("private key") }))
    }

    func testSettingsAdvancedLinks_basescan() {
        let links = SettingsAdvancedCatalog.explorerLinks
        XCTAssertEqual(links.count, 2)
        XCTAssertTrue(links.allSatisfy { $0.title == "View on Basescan" })
        XCTAssertTrue(links.contains { $0.urlString.contains("basescan.org/address/") })
        XCTAssertTrue(links.contains { $0.urlString.contains("basescan.org/tx/") })
        XCTAssertTrue(SettingsAdvancedCatalog.containsExplorerLinksOnly())
    }
}
