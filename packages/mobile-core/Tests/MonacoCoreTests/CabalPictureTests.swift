import XCTest

@testable import MonacoCore

/// The cabal picture: what the group payloads decode into, and what the two
/// write routes put on the wire.
final class CabalPictureTests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    // MARK: - Group view decode

    func testGroupView_decodesPictureAndCreatorFlag() throws {
        let json = Self.groupViewJSON(
            extra: #""pictureUrl": "https://cdn.test/groups/g1/abc.jpg", "isCreator": true"#
        )

        let view = try JSONDecoder().decode(GroupViewDTO.self, from: Data(json.utf8))

        XCTAssertEqual(view.pictureUrl, "https://cdn.test/groups/g1/abc.jpg")
        XCTAssertEqual(view.isCreator, true)
        XCTAssertTrue(view.viewerIsCreator)
    }

    func testGroupView_decodesWithoutAPicture() throws {
        let json = Self.groupViewJSON(extra: #""pictureUrl": null, "isCreator": false"#)

        let view = try JSONDecoder().decode(GroupViewDTO.self, from: Data(json.utf8))

        XCTAssertNil(view.pictureUrl)
        XCTAssertEqual(view.isCreator, false)
        XCTAssertFalse(view.viewerIsCreator)
        // The rest of the payload still lands, so a cabal without a picture is
        // not a degraded cabal.
        XCTAssertEqual(view.name, "Weekend investors")
        XCTAssertEqual(view.members.count, 1)
    }

    /// A server that predates the picture fields must keep working: the app is
    /// released ahead of the backend often enough that this cannot throw.
    func testGroupView_decodesWhenTheServerOmitsBothFieldsEntirely() throws {
        let view = try JSONDecoder().decode(GroupViewDTO.self, from: Data(Self.groupViewJSON().utf8))

        XCTAssertNil(view.pictureUrl)
        XCTAssertNil(view.isCreator)
        XCTAssertFalse(view.viewerIsCreator, "a missing isCreator must not offer the picture controls")
    }

    func testGroupViewFixture_stillDecodes() throws {
        let view = try Self.decodeFixture(GroupViewDTO.self, named: "group_view")
        XCTAssertNil(view.pictureUrl)
        XCTAssertFalse(view.viewerIsCreator)
    }

    // MARK: - Board rows

    func testDiscoveryRow_decodesPictureAndToleratesItsAbsence() throws {
        let withPicture = #"""
        {
          "groupId": "g1", "name": "Weekend investors", "memberCount": 4,
          "potValueUsd": "623.01", "percentReturn": "0.12", "dollarPnl": "+48.20",
          "isJoined": true, "joinMode": "open",
          "pictureUrl": "https://cdn.test/groups/g1/abc.jpg"
        }
        """#
        let row = try JSONDecoder().decode(GroupDiscoveryRowDTO.self, from: Data(withPicture.utf8))
        XCTAssertEqual(row.pictureUrl, "https://cdn.test/groups/g1/abc.jpg")

        let without = #"""
        {
          "groupId": "g2", "name": "No picture", "memberCount": 1,
          "potValueUsd": "0.00", "percentReturn": null, "dollarPnl": "+0.00",
          "isJoined": false, "joinMode": "request"
        }
        """#
        let bare = try JSONDecoder().decode(GroupDiscoveryRowDTO.self, from: Data(without.utf8))
        XCTAssertNil(bare.pictureUrl)
        XCTAssertEqual(bare.name, "No picture")
    }

    func testHomeGroupBoardRow_decodesPicture() throws {
        let json = #"""
        {
          "groupId": "g1", "name": "Weekend investors", "potValueUsd": "623.01",
          "percentReturn": "0.12", "dollarPnl": "+48.20", "isJoined": true,
          "pictureUrl": "https://cdn.test/groups/g1/abc.jpg"
        }
        """#
        let row = try JSONDecoder().decode(HomeGroupBoardRowDTO.self, from: Data(json.utf8))
        XCTAssertEqual(row.pictureUrl, "https://cdn.test/groups/g1/abc.jpg")
    }

    func testHomeMyGroupRow_decodesPictureAndToleratesItsAbsence() throws {
        let json = #"""
        {
          "groupId": "g1", "name": "Weekend investors", "equityUsd": "311.50",
          "slicePercent": "0.42", "dollarPnl": "+48.20", "percentReturn": "0.124"
        }
        """#
        let row = try JSONDecoder().decode(HomeMyGroupRowDTO.self, from: Data(json.utf8))
        XCTAssertNil(row.pictureUrl)
        XCTAssertEqual(row.equityUsd, "311.50")
    }

    func testExistingGroupFixtures_stillDecode() throws {
        let search = try Self.decodeFixture(GroupSearchResponseDTO.self, named: "groups_search")
        XCTAssertFalse(search.groups.isEmpty)
        XCTAssertNil(search.groups[0].pictureUrl)

        let home = try Self.decodeFixture(HomeViewDTO.self, named: "home_view")
        XCTAssertFalse(home.groups.isEmpty)
        XCTAssertNil(home.groups[0].pictureUrl)
    }

    // MARK: - CabalPictureDTO

    func testCabalPictureDTO_decodesAUrlAndANullRemoval() throws {
        let set = try JSONDecoder().decode(
            CabalPictureDTO.self,
            from: Data(#"{"groupId":"g1","pictureUrl":"https://cdn.test/g1.jpg"}"#.utf8)
        )
        XCTAssertEqual(set.pictureUrl, "https://cdn.test/g1.jpg")

        let cleared = try JSONDecoder().decode(
            CabalPictureDTO.self,
            from: Data(#"{"groupId":"g1","pictureUrl":null}"#.utf8)
        )
        XCTAssertNil(cleared.pictureUrl)
        XCTAssertEqual(cleared.groupId, "g1")
    }

    /// A blank string would otherwise become a URL the image loader retries and
    /// fails on forever.
    func testCabalPictureDTO_treatsABlankUrlAsNoPicture() throws {
        let blank = try JSONDecoder().decode(
            CabalPictureDTO.self,
            from: Data(#"{"groupId":"g1","pictureUrl":"   "}"#.utf8)
        )
        XCTAssertNil(blank.pictureUrl)
    }

    // MARK: - Client requests

    func testUploadCabalPicture_sendsMultipartPictureField() async throws {
        var captured: URLRequest?
        var capturedBody: Data?
        MockURLProtocol.requestHandler = { request in
            captured = request
            capturedBody = Self.bodyData(of: request)
            return (Self.response(request, status: 200), Data(#"{"groupId":"g1","pictureUrl":"https://cdn.test/g1.jpg"}"#.utf8))
        }
        let image = Data([0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10])

        let result = try await makeClient().uploadCabalPicture(groupID: "g1", imageData: image, mimeType: "image/jpeg")

        XCTAssertEqual(result.pictureUrl, "https://cdn.test/g1.jpg")

        let request = try XCTUnwrap(captured)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/v1/groups/g1/picture")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer \(TestFixtures.fixtureSessionToken)")

        let contentType = try XCTUnwrap(request.value(forHTTPHeaderField: "Content-Type"))
        XCTAssertTrue(contentType.hasPrefix("multipart/form-data; boundary="))
        let boundary = String(contentType.dropFirst("multipart/form-data; boundary=".count))

        let body = try XCTUnwrap(capturedBody)
        let text = String(decoding: body, as: UTF8.self)
        // The field name is what the backend reads; getting it wrong is a 400.
        XCTAssertTrue(text.contains(#"name="picture"; filename="cabal.jpg""#))
        XCTAssertTrue(text.contains("Content-Type: image/jpeg"))
        XCTAssertTrue(text.hasSuffix("--\(boundary)--\r\n"))
        XCTAssertNotNil(body.range(of: image))
    }

    func testRemoveCabalPicture_sendsDeleteWithNoBody() async throws {
        var captured: URLRequest?
        MockURLProtocol.requestHandler = { request in
            captured = request
            return (Self.response(request, status: 200), Data(#"{"groupId":"g1","pictureUrl":null}"#.utf8))
        }

        let result = try await makeClient().removeCabalPicture(groupID: "g1")

        XCTAssertNil(result.pictureUrl)
        let request = try XCTUnwrap(captured)
        XCTAssertEqual(request.httpMethod, "DELETE")
        XCTAssertEqual(request.url?.path, "/v1/groups/g1/picture")
        XCTAssertNil(Self.bodyData(of: request))
    }

    func testUploadCabalPicture_403_surfacesServerMessage() async {
        MockURLProtocol.requestHandler = { request in
            (Self.response(request, status: 403), Data(#"{"error":"only the cabal's creator can change its picture"}"#.utf8))
        }

        do {
            _ = try await makeClient().uploadCabalPicture(groupID: "g1", imageData: Data([0x00]), mimeType: "image/png")
            XCTFail("expected rejection")
        } catch {
            XCTAssertEqual(
                error as? MonacoAPIError,
                .rejected(status: 403, message: "only the cabal's creator can change its picture")
            )
        }
    }

    func testUploadCabalPicture_413_surfacesServerMessage() async {
        MockURLProtocol.requestHandler = { request in
            (Self.response(request, status: 413), Data(#"{"error":"picture must be at most 2MB"}"#.utf8))
        }

        do {
            _ = try await makeClient().uploadCabalPicture(groupID: "g1", imageData: Data([0x00]), mimeType: "image/png")
            XCTFail("expected rejection")
        } catch {
            XCTAssertEqual(error as? MonacoAPIError, .rejected(status: 413, message: "picture must be at most 2MB"))
        }
    }

    // MARK: - Helpers

    private func makeClient() -> MonacoAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: URLSession(configuration: configuration),
            accessTokenProvider: { TestFixtures.fixtureSessionToken }
        )
    }

    /// A minimal group view, optionally with extra top-level keys.
    private static func groupViewJSON(extra: String = "") -> String {
        let tail = extra.isEmpty ? "" : ", \(extra)"
        return """
        {
          "id": "g1",
          "name": "Weekend investors",
          "treasuryAddress": "So11111111111111111111111111111111111111112",
          "potTotalUsd": "623.01",
          "pot": [],
          "you": {
            "shareUnits": "500000", "equityUsd": "311.50", "slicePercent": "0.42",
            "dollarPnl": "+48.20", "percentReturn": "0.124"
          },
          "members": [
            { "rank": 1, "userId": "u1", "displayName": "Alfred", "percentReturn": "0.124", "dollarPnl": "+48.20" }
          ],
          "proposals": []\(tail)
        }
        """
    }

    private static func decodeFixture<T: Decodable>(_ type: T.Type, named name: String) throws -> T {
        let url = try XCTUnwrap(Bundle.module.url(forResource: name, withExtension: "json"))
        return try JSONDecoder().decode(type, from: Data(contentsOf: url))
    }

    private static func response(_ request: URLRequest, status: Int) -> HTTPURLResponse {
        HTTPURLResponse(
            url: request.url!,
            statusCode: status,
            httpVersion: nil,
            headerFields: ["Content-Type": "application/json"]
        )!
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
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return data.isEmpty ? nil : data
    }
}
