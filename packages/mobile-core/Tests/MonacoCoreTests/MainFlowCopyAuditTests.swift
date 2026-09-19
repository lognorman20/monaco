import XCTest
@testable import MonacoCore

final class MainFlowCopyAuditTests: XCTestCase {
    func testMainFlowStrings_excludeWalletGasSeedPhraseMintAndNav() {
        // Arrange
        let strings = MainFlowCopyManifest.mainFlowStrings

        // Act
        let clean = MainFlowCopyAudit.stringsAreClean(strings)

        // Assert
        XCTAssertTrue(clean)
        for name in ["Home", "Profile", "Cabals", "Assets", "Settings"] {
            XCTAssertTrue(strings.contains(name), "missing tab label \(name)")
        }
        for term in MainFlowCopyAudit.forbiddenTerms {
            XCTAssertFalse(
                strings.contains { $0.lowercased().contains(term.trimmingCharacters(in: .whitespaces)) },
                "found forbidden term: \(term)"
            )
        }
    }
}
