import XCTest
@testable import MonacoCore

final class ProfileAPITests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    private let meBody = """
    {
      "userId": "550e8400-e29b-41d4-a716-446655440000",
      "displayName": "Logan Norman",
      "memberWalletAddress": "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
      "profilePhotoUrl": null,
      "createdAt": "2026-09-01T14:30:00Z"
    }
    """

    func testUpdateProfile_sendsPatchWithJSONBodyAndAuth() async throws {
        // Arrange
        var captured: URLRequest?
        var capturedBody: Data?
        MockURLProtocol.requestHandler = { request in
            captured = request
            capturedBody = Self.bodyData(of: request)
            return (Self.response(request, status: 200), Data(self.meBody.utf8))
        }
        let client = makeClient()

        // Act
        let me = try await client.updateProfile(displayName: "Logan Norman")

        // Assert
        let request = try XCTUnwrap(captured)
        XCTAssertEqual(request.httpMethod, "PATCH")
        XCTAssertEqual(request.url?.absoluteString, "https://api.test/v1/me")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Content-Type"), "application/json")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer \(TestFixtures.fixtureSessionToken)")
        let body = try XCTUnwrap(capturedBody)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: String])
        XCTAssertEqual(json, ["displayName": "Logan Norman"])
        XCTAssertEqual(me.displayName, "Logan Norman")
        XCTAssertNil(me.profilePhotoUrl)
    }

    func testUpdateProfile_400_surfacesServerMessage() async {
        MockURLProtocol.requestHandler = { request in
            (Self.response(request, status: 400), Data(#"{"error":"Display name must include a letter or number."}"#.utf8))
        }

        do {
            _ = try await makeClient().updateProfile(displayName: "...")
            XCTFail("expected rejection")
        } catch {
            XCTAssertEqual(
                error as? MonacoAPIError,
                .rejected(status: 400, message: "Display name must include a letter or number.")
            )
        }
    }

    func testUpdateProfile_429_readsRetryAfter() async {
        MockURLProtocol.requestHandler = { request in
            (Self.response(request, status: 429, headers: ["Retry-After": "12"]), Data(#"{"error":"too many requests"}"#.utf8))
        }

        do {
            _ = try await makeClient().updateProfile(displayName: "Logan")
            XCTFail("expected rate limit")
        } catch {
            XCTAssertEqual(error as? MonacoAPIError, .rateLimited(retryAfterSeconds: 12))
        }
    }

    func testUpdateProfile_429_withoutHeader_hasNilRetryAfter() async {
        MockURLProtocol.requestHandler = { request in
            (Self.response(request, status: 429), Data())
        }

        do {
            _ = try await makeClient().updateProfile(displayName: "Logan")
            XCTFail("expected rate limit")
        } catch {
            XCTAssertEqual(error as? MonacoAPIError, .rateLimited(retryAfterSeconds: nil))
        }
    }

    func testUpdateProfile_401And500_mapToHTTPStatus() async {
        for status in [401, 500] {
            MockURLProtocol.requestHandler = { request in
                (Self.response(request, status: status), Data(#"{"error":"nope"}"#.utf8))
            }
            do {
                _ = try await makeClient().updateProfile(displayName: "Logan")
                XCTFail("expected failure for \(status)")
            } catch {
                XCTAssertEqual(error as? MonacoAPIError, .httpStatus(status))
            }
        }
    }

    func testUpdateProfile_malformedResponse_throwsDecodingError() async {
        MockURLProtocol.requestHandler = { request in
            (Self.response(request, status: 200), Data(#"{"displayName":"Logan"}"#.utf8))
        }

        do {
            _ = try await makeClient().updateProfile(displayName: "Logan")
            XCTFail("expected decoding failure without userId")
        } catch {
            XCTAssertTrue(error is DecodingError, "got \(error)")
        }
    }

    func testUploadProfilePhoto_sendsMultipartPhotoField() async throws {
        var captured: URLRequest?
        var capturedBody: Data?
        MockURLProtocol.requestHandler = { request in
            captured = request
            capturedBody = Self.bodyData(of: request)
            return (Self.response(request, status: 200), Data(self.meBody.utf8))
        }
        let image = Data([0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10])

        _ = try await makeClient().uploadProfilePhoto(imageData: image, mimeType: "image/jpeg")

        let request = try XCTUnwrap(captured)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/v1/me/profile-photo")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer \(TestFixtures.fixtureSessionToken)")
        let contentType = try XCTUnwrap(request.value(forHTTPHeaderField: "Content-Type"))
        XCTAssertTrue(contentType.hasPrefix("multipart/form-data; boundary="))
        let boundary = String(contentType.dropFirst("multipart/form-data; boundary=".count))

        let body = try XCTUnwrap(capturedBody)
        let expected = ProfilePhotoMultipart.body(imageData: image, mimeType: "image/jpeg", boundary: boundary)
        XCTAssertEqual(body, expected)
        let text = String(decoding: body, as: UTF8.self)
        XCTAssertTrue(text.contains("name=\"photo\"; filename=\"profile.jpg\""))
        XCTAssertTrue(text.contains("Content-Type: image/jpeg"))
        XCTAssertTrue(text.hasSuffix("--\(boundary)--\r\n"))
        XCTAssertNotNil(body.range(of: image))
    }

    func testUploadProfilePhoto_400_surfacesServerMessage() async {
        MockURLProtocol.requestHandler = { request in
            (Self.response(request, status: 400), Data(#"{"error":"photo must be at most 2MB"}"#.utf8))
        }

        do {
            _ = try await makeClient().uploadProfilePhoto(imageData: Data([0x00]), mimeType: "image/png")
            XCTFail("expected rejection")
        } catch {
            XCTAssertEqual(error as? MonacoAPIError, .rejected(status: 400, message: "photo must be at most 2MB"))
        }
    }

    private func makeClient() -> MonacoAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: URLSession(configuration: configuration),
            accessTokenProvider: { TestFixtures.fixtureSessionToken }
        )
    }

    private static func response(_ request: URLRequest, status: Int, headers: [String: String] = [:]) -> HTTPURLResponse {
        var allHeaders = ["Content-Type": "application/json"]
        allHeaders.merge(headers) { _, new in new }
        return HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: allHeaders)!
    }

    /// URLProtocol receives bodies as a stream, not `httpBody`.
    private static func bodyData(of request: URLRequest) -> Data? {
        if let body = request.httpBody {
            return body
        }
        guard let stream = request.httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while stream.hasBytesAvailable {
            let read = stream.read(&buffer, maxLength: buffer.count)
            guard read > 0 else { break }
            data.append(buffer, count: read)
        }
        return data
    }
}

final class MeDTOTests: XCTestCase {
    func testDecode_toleratesMissingOptionalFields() throws {
        let json = #"{"userId":"u1","displayName":"","memberWalletAddress":"addr"}"#

        let dto = try JSONDecoder().decode(MeDTO.self, from: Data(json.utf8))

        XCTAssertEqual(dto.userId, "u1")
        XCTAssertEqual(dto.displayName, "")
        XCTAssertNil(dto.profilePhotoUrl)
        XCTAssertNil(dto.createdAt)
    }

    func testDecode_blankPhotoAndFractionalCreatedAt() throws {
        let json = #"{"userId":"u1","displayName":"A","memberWalletAddress":"addr","profilePhotoUrl":"  ","createdAt":"2026-09-01T14:30:00.250Z"}"#

        let dto = try JSONDecoder().decode(MeDTO.self, from: Data(json.utf8))

        XCTAssertNil(dto.profilePhotoUrl)
        XCTAssertEqual(try XCTUnwrap(dto.createdAt).timeIntervalSince1970, 1_788_273_000.25, accuracy: 0.001)
    }

    func testDecode_unparseableCreatedAtIsNil() throws {
        let json = #"{"userId":"u1","displayName":"A","memberWalletAddress":"addr","createdAt":"yesterday"}"#

        let dto = try JSONDecoder().decode(MeDTO.self, from: Data(json.utf8))

        XCTAssertNil(dto.createdAt)
    }

    func testEncodeDecode_roundTrips() throws {
        let original = MeDTO(
            userId: "u1",
            displayName: "Logan",
            memberWalletAddress: "addr",
            profilePhotoUrl: "https://example.test/a.jpg",
            createdAt: Date(timeIntervalSince1970: 1_788_273_000)
        )

        let decoded = try JSONDecoder().decode(MeDTO.self, from: JSONEncoder().encode(original))

        XCTAssertEqual(decoded, original)
    }

    func testWithDisplayName_keepsOtherFields() {
        let original = MeDTO(userId: "u1", displayName: "Old", memberWalletAddress: "addr", profilePhotoUrl: "p", createdAt: nil)

        let renamed = original.withDisplayName("New")

        XCTAssertEqual(renamed.displayName, "New")
        XCTAssertEqual(renamed.userId, "u1")
        XCTAssertEqual(renamed.profilePhotoUrl, "p")
    }

    func testBoardDTOs_decodeProfilePhotoUrl() throws {
        let homeURL = try XCTUnwrap(Bundle.module.url(forResource: "home_view", withExtension: "json"))
        let home = try JSONDecoder().decode(HomeViewDTO.self, from: Data(contentsOf: homeURL))
        XCTAssertEqual(home.people[0].profilePhotoUrl, "https://example.supabase.co/storage/v1/object/public/avatars/u1/a1.jpg")

        let groupURL = try XCTUnwrap(Bundle.module.url(forResource: "group_view", withExtension: "json"))
        let group = try JSONDecoder().decode(GroupViewDTO.self, from: Data(contentsOf: groupURL))
        XCTAssertEqual(group.members[0].profilePhotoUrl, "https://example.supabase.co/storage/v1/object/public/avatars/u1/a1.jpg")
        XCTAssertNil(group.members[1].profilePhotoUrl)
    }

    func testBoardDTOs_missingProfilePhotoUrlDecodesAsNil() throws {
        let json = #"{"rank":1,"userId":"u1","displayName":"A","percentReturn":null,"dollarPnl":"+0.00"}"#

        let row = try JSONDecoder().decode(LeaderboardRowDTO.self, from: Data(json.utf8))

        XCTAssertNil(row.profilePhotoUrl)
    }
}

final class DisplayNameRulesTests: XCTestCase {
    private func scalar(_ value: UInt32) -> String {
        String(Character(Unicode.Scalar(value)!))
    }

    func testNormalize_accepts() {
        let cases: [(String, String)] = [
            ("Logan", "Logan"),
            ("  Logan \n", "Logan"),
            ("Logan    Norman", "Logan Norman"),
            ("Logan" + scalar(0x00A0) + scalar(0x3000) + "Norman", "Logan Norman"),
            ("L", "L"),
            ("2049", "2049"),
            ("O'Brien-Smith Jr.", "O'Brien-Smith Jr."),
            ("Jose" + scalar(0x0301), "Jos" + scalar(0x00E9)),
            ("Ana " + scalar(0x1F680), "Ana " + scalar(0x1F680)),
            (String(repeating: scalar(0x00E9), count: 32), String(repeating: scalar(0x00E9), count: 32)),
        ]
        for (raw, want) in cases {
            XCTAssertEqual(try? DisplayNameRules.normalize(raw).get(), want, "raw: \(raw.debugDescription)")
        }
    }

    func testNormalize_rejects() {
        let cases: [(String, DisplayNameValidationError)] = [
            ("", .required),
            ("   \n", .required),
            (String(repeating: "a", count: 33), .tooLong),
            (String(repeating: scalar(0x00E9), count: 33), .tooLong),
            ("Lo\ngan", .invalidCharacters),
            ("Lo\tgan", .invalidCharacters),
            ("Lo" + scalar(0x0007) + "gan", .invalidCharacters),
            ("Lo" + scalar(0x200B) + "gan", .invalidCharacters),
            ("Lo" + scalar(0x200D) + "gan", .invalidCharacters),
            (scalar(0x202E) + "Logan", .invalidCharacters),
            (scalar(0x3164), .invalidCharacters),
            ("Lo" + scalar(0x2800) + "gan", .invalidCharacters),
            ("Logan" + scalar(0xE000), .invalidCharacters),
            ("...", .needsLetterOrNumber),
            (scalar(0x1F680), .needsLetterOrNumber),
        ]
        for (raw, want) in cases {
            switch DisplayNameRules.normalize(raw) {
            case .success(let value):
                XCTFail("\(raw.debugDescription) accepted as \(value.debugDescription), want \(want)")
            case .failure(let error):
                XCTAssertEqual(error, want, "raw: \(raw.debugDescription)")
            }
        }
    }

    func testNormalize_rejectsStackedCombiningMarks() {
        let zalgo = "Lox" + scalar(0x0301) + scalar(0x0302) + scalar(0x0303) + "gan"
        XCTAssertEqual(DisplayNameRules.validationMessage(for: zalgo), DisplayNameValidationError.invalidCharacters.message)
    }

    func testValidationMessage_matchesServerCopy() {
        XCTAssertNil(DisplayNameRules.validationMessage(for: "Logan"))
        XCTAssertEqual(DisplayNameRules.validationMessage(for: ""), "Display name is required.")
        XCTAssertEqual(
            DisplayNameRules.validationMessage(for: String(repeating: "a", count: 40)),
            "Display name must be 32 characters or fewer."
        )
    }
}

final class AvatarInitialsTests: XCTestCase {
    func testInitials() {
        XCTAssertEqual(AvatarInitials.from("Logan Norman"), "LN")
        XCTAssertEqual(AvatarInitials.from("logan"), "L")
        XCTAssertEqual(AvatarInitials.from("Mary Ann van Dyke"), "MD")
        XCTAssertEqual(AvatarInitials.from("  ana  "), "A")
        XCTAssertEqual(AvatarInitials.from("O'Brien"), "O")
        XCTAssertEqual(AvatarInitials.from("..."), "")
        XCTAssertEqual(AvatarInitials.from(""), "")
    }

    func testMemberSince_usesViewerTimeZone() {
        // 2026-09-01T02:00Z is still August 31 in Los Angeles.
        let date = Date(timeIntervalSince1970: 1_788_228_000)
        let locale = Locale(identifier: "en_US")

        XCTAssertEqual(MemberSinceFormatter.format(date, timeZone: TimeZone(identifier: "UTC")!, locale: locale), "Member since Sep 2026")
        XCTAssertEqual(MemberSinceFormatter.format(date, timeZone: TimeZone(identifier: "America/Los_Angeles")!, locale: locale), "Member since Aug 2026")
    }
}
