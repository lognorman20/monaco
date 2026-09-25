import XCTest
@testable import MonacoCore

final class PreIpoCopyTests: XCTestCase {
    /// The relative-time word changes shape, and the caption has to read as a sentence
    /// with every shape of it.
    func testReferenceCaption_readsAtEveryAge() {
        XCTAssertEqual(PreIpoCopy.referenceCaption(companyValue: "$32.1B", age: nil), "Company value $32.1B")
        XCTAssertEqual(PreIpoCopy.referenceCaption(companyValue: "$32.1B", age: ""), "Company value $32.1B")
        XCTAssertEqual(PreIpoCopy.referenceCaption(companyValue: "$32.1B", age: "now"), "Company value $32.1B · updated just now")
        XCTAssertEqual(PreIpoCopy.referenceCaption(companyValue: "$32.1B", age: "15m"), "Company value $32.1B · updated 15m ago")
        XCTAssertEqual(PreIpoCopy.referenceCaption(companyValue: "$32.1B", age: "3h"), "Company value $32.1B · updated 3h ago")
        XCTAssertEqual(PreIpoCopy.referenceCaption(companyValue: "$32.1B", age: "Sep 14"), "Company value $32.1B · updated Sep 14")
    }
}
