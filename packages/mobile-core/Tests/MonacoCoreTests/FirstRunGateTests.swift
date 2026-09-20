import XCTest
@testable import MonacoCore

final class FirstRunGateTests: XCTestCase {
    private func me(displayName: String) -> MeDTO {
        MeDTO(
            userId: "550e8400-e29b-41d4-a716-446655440000",
            displayName: displayName,
            memberWalletAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
        )
    }

    // MARK: - needsDisplayName

    func testNeedsDisplayName_emptyString() {
        XCTAssertTrue(FirstRunGate.needsDisplayName(""))
    }

    func testNeedsDisplayName_whitespaceOnly() {
        for raw in [" ", "   ", "\t", "\n", " \t\n "] {
            XCTAssertTrue(
                FirstRunGate.needsDisplayName(raw),
                "whitespace-only name \(raw.debugDescription) should count as missing"
            )
        }
    }

    func testNeedsDisplayName_realName() {
        XCTAssertFalse(FirstRunGate.needsDisplayName("Logan Norman"))
    }

    func testNeedsDisplayName_nameWithSurroundingWhitespace() {
        XCTAssertFalse(FirstRunGate.needsDisplayName("  Logan  "))
    }

    func testNeedsDisplayName_singleCharacterAndEmoji() {
        XCTAssertFalse(FirstRunGate.needsDisplayName("L"))
        XCTAssertFalse(FirstRunGate.needsDisplayName("🦈"))
    }

    /// "Member" is only what the boards print for an empty name; `/v1/me` never sends it
    /// for an unnamed account. Someone who typed it must not be asked again on every launch.
    func testNeedsDisplayName_literalMemberIsARealName() {
        XCTAssertFalse(FirstRunGate.needsDisplayName("Member"))
    }

    // MARK: - destination

    func testDestination_noProfileYet_waitsOnTheSession() {
        XCTAssertEqual(FirstRunGate.destination(for: nil), .session)
    }

    func testDestination_emptyName_asksForAName() {
        XCTAssertEqual(FirstRunGate.destination(for: me(displayName: "")), .nameSetup)
    }

    func testDestination_whitespaceName_asksForAName() {
        XCTAssertEqual(FirstRunGate.destination(for: me(displayName: "   ")), .nameSetup)
    }

    func testDestination_realName_opensTheTabs() {
        XCTAssertEqual(FirstRunGate.destination(for: me(displayName: "Logan Norman")), .app)
    }

    /// The decision comes from the decoded `/v1/me` body, so a returning user is routed
    /// by what the server says and a brand new one (`display_name` null) is caught.
    func testDestination_decodedFromMeResponse() throws {
        let decoder = JSONDecoder()
        let named = try decoder.decode(MeDTO.self, from: Data(#"""
        {"userId":"u1","displayName":"Ana","memberWalletAddress":"7xKX"}
        """#.utf8))
        XCTAssertEqual(FirstRunGate.destination(for: named), .app)

        let unnamed = try decoder.decode(MeDTO.self, from: Data(#"""
        {"userId":"u1","displayName":"","memberWalletAddress":"7xKX"}
        """#.utf8))
        XCTAssertEqual(FirstRunGate.destination(for: unnamed), .nameSetup)

        for body in [#"{"userId":"u1","displayName":null}"#, #"{"userId":"u1"}"#] {
            let missing = try decoder.decode(MeDTO.self, from: Data(body.utf8))
            XCTAssertEqual(FirstRunGate.destination(for: missing), .nameSetup, body)
        }
    }

    /// The screen saves, `me` comes back named, and the very next evaluation must route
    /// to the tabs — this is what makes the transition happen without a relaunch.
    func testDestination_flipsToAppOnceTheNameIsSaved() {
        let before = me(displayName: "")
        XCTAssertEqual(FirstRunGate.destination(for: before), .nameSetup)
        XCTAssertEqual(FirstRunGate.destination(for: before.withDisplayName("Ana")), .app)
    }

    /// Anything `DisplayNameRules` accepts must get the user through the gate, or first
    /// run could reject a name the server is happy to store and strand the account.
    func testDestination_everyNameTheRulesAcceptOpensTheTabs() throws {
        for raw in ["Ana", "Logan Norman", "J. R. R. T", "x Æ 12", "🦈 shark", "Zoë"] {
            let normalized = try DisplayNameRules.normalize(raw).get()
            XCTAssertEqual(
                FirstRunGate.destination(for: me(displayName: normalized)),
                .app,
                "\(raw.debugDescription) normalizes to \(normalized.debugDescription) and should pass the gate"
            )
        }
    }
}
