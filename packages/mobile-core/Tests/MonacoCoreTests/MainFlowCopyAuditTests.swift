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
        for name in ["Home", "Profile", "Cabals", "Stocks", "Settings"] {
            XCTAssertTrue(strings.contains(name), "missing tab label \(name)")
        }
    }

    func testForbiddenTerms_coverMoneyPlumbingJargon() {
        for term in ["treasury", "route", "quote", "bps", "units", "stake", "sweep", "http", "api key"] {
            XCTAssertTrue(MainFlowCopyAudit.forbiddenTerms.contains(term), "missing forbidden term \(term)")
        }
    }

    func testStringsAreClean_rejectsPlumbingCopy() {
        XCTAssertFalse(MainFlowCopyAudit.stringsAreClean(["No route for this stock right now."]))
        XCTAssertFalse(MainFlowCopyAudit.stringsAreClean(["Loading treasury…"]))
        XCTAssertFalse(MainFlowCopyAudit.stringsAreClean(["Couldn't load (HTTP 500)"]))
        XCTAssertFalse(MainFlowCopyAudit.stringsAreClean(["Stake moved to your balance."]))
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean(["Can't be bought right now"]))
        XCTAssertFalse(MainFlowCopyAudit.stringsAreClean(["Marked at NAV today"]))
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean([PreIpoCopy.referenceUnavailable]))
    }

    func testMainFlowCopyAudit_preIpoStrings_pass() {
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean(PreIpoCopy.auditedStrings))
        XCTAssertTrue(MainFlowCopyManifest.mainFlowStrings.contains(PreIpoCopy.chipLabel))
    }
}
