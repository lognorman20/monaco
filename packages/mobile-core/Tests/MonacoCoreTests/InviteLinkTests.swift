import XCTest
@testable import MonacoCore

final class InviteLinkTests: XCTestCase {
    private let uuid = "5b1f0c9e-0005-4c55-9a51-000000000005"

    // MARK: - Links

    func testParse_appSchemeLink() {
        XCTAssertEqual(InviteLink.parse("monaco://join/K7QM4XPD"), .code("K7QM4XPD"))
        XCTAssertEqual(InviteLink.parse("MONACO://JOIN/k7qm4xpd"), .code("K7QM4XPD"))
    }

    func testParse_webLinkInEveryShapeAMessageCarries() {
        let forms = [
            "https://trymonaco.xyz/join/K7QM4XPD",
            "https://www.trymonaco.xyz/join/K7QM4XPD",
            "http://trymonaco.xyz/join/k7qm4xpd/",
            "trymonaco.xyz/join/K7QM4XPD",
            "https://trymonaco.xyz/join/K7QM4XPD?utm_source=imessage",
            "  https://trymonaco.xyz/join/K7QM4XPD\n",
        ]
        for form in forms {
            XCTAssertEqual(InviteLink.parse(form), .code("K7QM4XPD"), form)
        }
    }

    func testParse_theWholeShareText() {
        let text = InviteShareText.message(cabalName: "Sunday Investors", link: InviteLink.webURL(for: "K7QM4XPD"))

        XCTAssertEqual(InviteLink.parse(text), .code("K7QM4XPD"))
    }

    func testParse_bareCodesAsPeopleTypeThem() {
        for form in ["K7QM4XPD", "k7qm4xpd", "K7QM 4XPD", "k7qm-4xpd", " K7QM4XPD "] {
            XCTAssertEqual(InviteLink.parse(form), .code("K7QM4XPD"), form)
        }
    }

    func testParse_legacyCabalIdStillWorks() {
        XCTAssertEqual(InviteLink.parse(uuid), .groupId(uuid))
        XCTAssertEqual(InviteLink.parse(uuid.uppercased()), .groupId(uuid))
        XCTAssertEqual(InviteLink.parse("monaco://join/\(uuid)"), .groupId(uuid))
        XCTAssertEqual(InviteLink.parse("https://trymonaco.xyz/join/\(uuid)"), .groupId(uuid))
    }

    /// The alphabet has no 0, O, 1 or I, so a string using them is not a code.
    func testParse_rejectsAmbiguousSymbolsAndWrongLengths() {
        let rejected = [
            "", "   ", "AB12CD34", "K7QM4XP0", "K7QM4XPO", "K7QM4XPI", "K7QM4XP", "K7QM4XPDA",
            "monaco://join/", "monaco://cabal/K7QM4XPD",
            "https://example.com/join/K7QM4XPD", "https://trymonaco.xyz/cabal/K7QM4XPD",
            "https://eviltrymonaco.xyz.example/other", "5b1f0c9e-0005-4c55-9a51",
            "hello there",
        ]
        for raw in rejected {
            XCTAssertNil(InviteLink.parse(raw), raw)
        }
    }

    func testParseURL_opensOnlyInviteLinks() throws {
        XCTAssertEqual(InviteLink.parse(url: try XCTUnwrap(URL(string: "monaco://join/K7QM4XPD"))), .code("K7QM4XPD"))
        XCTAssertEqual(InviteLink.parse(url: try XCTUnwrap(URL(string: "https://trymonaco.xyz/join/k7qm-4xpd"))), .code("K7QM4XPD"))
        XCTAssertNil(InviteLink.parse(url: try XCTUnwrap(URL(string: "https://trymonaco.xyz/"))))
        XCTAssertNil(InviteLink.parse(url: try XCTUnwrap(URL(string: "https://example.com/join/K7QM4XPD"))))
        XCTAssertNil(InviteLink.parse(url: try XCTUnwrap(URL(string: "monaco://settings"))))
    }

    // MARK: - Codes

    func testPartialCode_isQuietWhileTyping() {
        XCTAssertTrue(InviteLink.isPartialCode("K7Q"))
        XCTAssertTrue(InviteLink.isPartialCode("k7qm-4x"))
        XCTAssertFalse(InviteLink.isPartialCode(""))
        XCTAssertFalse(InviteLink.isPartialCode("K7QM4XPD"), "a whole code is not partial")
        XCTAssertFalse(InviteLink.isPartialCode("K7Q0"), "0 can never become a code")
        XCTAssertFalse(InviteLink.isPartialCode("https://"))
    }

    func testDisplayCode_groupsTheCodeInFours() {
        XCTAssertEqual(InviteLink.displayCode("K7QM4XPD"), "K7QM 4XPD")
        XCTAssertEqual(InviteLink.displayCode("SHORT"), "SHORT")
    }

    func testLinksForACode() {
        XCTAssertEqual(InviteLink.webURL(for: "K7QM4XPD").absoluteString, "https://trymonaco.xyz/join/K7QM4XPD")
        XCTAssertEqual(InviteLink.appURL(for: "K7QM4XPD").absoluteString, "monaco://join/K7QM4XPD")
        XCTAssertEqual(InviteLink.code("K7QM4XPD").code, "K7QM4XPD")
        XCTAssertNil(InviteLink.code("K7QM4XPD").groupId)
        XCTAssertEqual(InviteLink.groupId(uuid).groupId, uuid)
    }

    func testShareText_namesTheCabalAndCarriesTheLink() {
        let link = InviteLink.webURL(for: "K7QM4XPD")

        XCTAssertEqual(
            InviteShareText.message(cabalName: "Sunday Investors", link: link),
            "Join Sunday Investors on Monaco: https://trymonaco.xyz/join/K7QM4XPD"
        )
        XCTAssertEqual(InviteShareText.subject(cabalName: "Sunday Investors"), "Join Sunday Investors on Monaco")
    }

    // MARK: - DTOs

    func testInvitePreviewDTO_decodesTheAPIShape() throws {
        let json = Data("""
        {"code":"K7QM4XPD","groupId":"\(uuid)","name":"Sunday Investors","memberCount":9,
         "tint":"indigo","pictureUrl":null,"joinPolicy":"request","potValueUsd":"1240.50"}
        """.utf8)

        let preview = try JSONDecoder().decode(InvitePreviewDTO.self, from: json)

        XCTAssertEqual(preview, InvitePreviewDTO(
            code: "K7QM4XPD", groupId: uuid, name: "Sunday Investors", memberCount: 9,
            tint: "indigo", pictureUrl: nil, joinPolicy: .request, potValueUsd: "1240.50"
        ))
    }

    func testInvitePreviewDTO_unknownPolicyNeverOffersAOneTapJoin() throws {
        let json = Data("""
        {"code":"K7QM4XPD","groupId":"\(uuid)","name":"S","memberCount":1,
         "tint":"pine","pictureUrl":"https://cdn.test/p.png","joinPolicy":"invite_only","potValueUsd":"0.00"}
        """.utf8)

        let preview = try JSONDecoder().decode(InvitePreviewDTO.self, from: json)

        XCTAssertEqual(preview.joinPolicy, .request)
        XCTAssertEqual(preview.pictureUrl, "https://cdn.test/p.png")
    }

    func testInviteDTO_linkFallsBackToTheCanonicalURL() throws {
        let invite = try JSONDecoder().decode(InviteDTO.self, from: Data(#"{"code":"K7QM4XPD","url":"https://trymonaco.xyz/join/K7QM4XPD"}"#.utf8))

        XCTAssertEqual(invite.link.absoluteString, "https://trymonaco.xyz/join/K7QM4XPD")
        XCTAssertEqual(InviteDTO(code: "K7QM4XPD", url: "").link.absoluteString, "https://trymonaco.xyz/join/K7QM4XPD")
    }
}
