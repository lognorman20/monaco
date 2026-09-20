import MonacoCore
import XCTest

@testable import Monaco

/// A writer a test drives: it hands back whatever the test queued, and records
/// what it was asked to do.
@MainActor
private final class StubCabalPictureWriter: CabalPictureWriting {
    enum Call: Equatable {
        case upload(groupId: String, bytes: Int, mimeType: String)
        case remove(groupId: String)
    }

    var calls: [Call] = []
    var uploadResult: Result<String?, Error> = .success("https://cdn.test/new.jpg")
    var removeResult: Result<String?, Error> = .success(nil)
    /// Held until the test releases it, so a write can be observed mid-flight.
    var gate: CheckedContinuation<Void, Never>?
    var shouldWait = false

    func uploadPicture(groupId: String, imageData: Data, mimeType: String) async throws -> String? {
        calls.append(.upload(groupId: groupId, bytes: imageData.count, mimeType: mimeType))
        if shouldWait {
            await withCheckedContinuation { continuation in gate = continuation }
        }
        return try uploadResult.get()
    }

    func removePicture(groupId: String) async throws -> String? {
        calls.append(.remove(groupId: groupId))
        if shouldWait {
            await withCheckedContinuation { continuation in gate = continuation }
        }
        return try removeResult.get()
    }

    func release() {
        gate?.resume()
        gate = nil
    }
}

@MainActor
final class CabalPictureEditorTests: XCTestCase {
    private let image = Data(repeating: 0xAB, count: 128)

    private func makeEditor(
        pictureUrl: String? = nil,
        writer: StubCabalPictureWriter
    ) -> CabalPictureEditor {
        CabalPictureEditor(groupId: "g1", pictureUrl: pictureUrl, writer: writer)
    }

    // MARK: - Setting a picture

    func testSetPicture_adoptsTheSavedUrlAndReportsIt() async {
        let writer = StubCabalPictureWriter()
        writer.uploadResult = .success("https://cdn.test/groups/g1/new.jpg")
        let editor = makeEditor(writer: writer)

        let outcome = await editor.setPicture(imageData: image, mimeType: "image/jpeg")

        XCTAssertEqual(outcome, .saved("https://cdn.test/groups/g1/new.jpg"))
        XCTAssertEqual(editor.pictureUrl, "https://cdn.test/groups/g1/new.jpg")
        XCTAssertFalse(editor.isWorking)
        XCTAssertNil(editor.lastFailure)
        XCTAssertEqual(writer.calls, [.upload(groupId: "g1", bytes: 128, mimeType: "image/jpeg")])
    }

    func testSetPicture_replacingSwapsTheUrl() async {
        let writer = StubCabalPictureWriter()
        writer.uploadResult = .success("https://cdn.test/groups/g1/second.jpg")
        let editor = makeEditor(pictureUrl: "https://cdn.test/groups/g1/first.jpg", writer: writer)

        _ = await editor.setPicture(imageData: image, mimeType: "image/jpeg")

        XCTAssertEqual(editor.pictureUrl, "https://cdn.test/groups/g1/second.jpg")
    }

    /// The write failed, so the cabal still has the picture it had. Clearing it
    /// on screen would tell the member something untrue.
    func testSetPicture_failureKeepsTheCurrentPicture() async {
        let writer = StubCabalPictureWriter()
        writer.uploadResult = .failure(MonacoAPIError.rejected(status: 413, message: "picture must be at most 2MB", requestID: nil))
        let editor = makeEditor(pictureUrl: "https://cdn.test/groups/g1/first.jpg", writer: writer)

        let outcome = await editor.setPicture(imageData: image, mimeType: "image/jpeg")

        XCTAssertEqual(outcome, .failed("picture must be at most 2MB"))
        XCTAssertEqual(editor.pictureUrl, "https://cdn.test/groups/g1/first.jpg")
        XCTAssertEqual(editor.lastFailure, "picture must be at most 2MB")
        XCTAssertFalse(editor.isWorking)
    }

    // MARK: - Removing

    func testRemovePicture_clearsIt() async {
        let writer = StubCabalPictureWriter()
        writer.removeResult = .success(nil)
        let editor = makeEditor(pictureUrl: "https://cdn.test/groups/g1/first.jpg", writer: writer)

        let outcome = await editor.removePicture()

        XCTAssertEqual(outcome, .saved(nil))
        XCTAssertNil(editor.pictureUrl)
        XCTAssertEqual(writer.calls, [.remove(groupId: "g1")])
    }

    func testRemovePicture_failureKeepsIt() async {
        let writer = StubCabalPictureWriter()
        writer.removeResult = .failure(MonacoAPIError.httpStatus(403, nil))
        let editor = makeEditor(pictureUrl: "https://cdn.test/groups/g1/first.jpg", writer: writer)

        let outcome = await editor.removePicture()

        XCTAssertEqual(outcome, .failed("Only the cabal's creator can change its picture."))
        XCTAssertEqual(editor.pictureUrl, "https://cdn.test/groups/g1/first.jpg")
    }

    // MARK: - One write at a time

    func testSecondWriteWhileOneIsInFlightIsRefusedNotQueued() async {
        let writer = StubCabalPictureWriter()
        writer.shouldWait = true
        let editor = makeEditor(writer: writer)

        let first = Task { await editor.setPicture(imageData: image, mimeType: "image/jpeg") }
        await waitUntil { editor.isWorking }

        let second = await editor.setPicture(imageData: image, mimeType: "image/jpeg")
        XCTAssertEqual(second, .failed("Still working on the last change."))
        XCTAssertEqual(writer.calls.count, 1, "the refused write must not reach the network")

        writer.release()
        _ = await first.value
        XCTAssertFalse(editor.isWorking)
    }

    // MARK: - Refresh races

    /// A refresh that started before the upload must not land after it and put
    /// the old picture back.
    func testRefreshDoesNotOverwriteAWriteInFlight() async {
        let writer = StubCabalPictureWriter()
        writer.shouldWait = true
        writer.uploadResult = .success("https://cdn.test/groups/g1/new.jpg")
        let editor = makeEditor(pictureUrl: "https://cdn.test/groups/g1/old.jpg", writer: writer)

        let upload = Task { await editor.setPicture(imageData: image, mimeType: "image/jpeg") }
        await waitUntil { editor.isWorking }

        editor.adoptFromRefresh("https://cdn.test/groups/g1/old.jpg")
        XCTAssertEqual(editor.pictureUrl, "https://cdn.test/groups/g1/old.jpg", "unchanged until the write lands")

        writer.release()
        _ = await upload.value
        XCTAssertEqual(editor.pictureUrl, "https://cdn.test/groups/g1/new.jpg")
    }

    func testRefreshAdoptsThePictureWhenNothingIsInFlight() async {
        let editor = makeEditor(pictureUrl: nil, writer: StubCabalPictureWriter())

        editor.adoptFromRefresh("https://cdn.test/groups/g1/from-refresh.jpg")

        XCTAssertEqual(editor.pictureUrl, "https://cdn.test/groups/g1/from-refresh.jpg")
    }

    // MARK: - Normalising

    func testBlankUrlsBecomeNoPicture() async {
        XCTAssertNil(CabalPictureEditor.normalised(nil))
        XCTAssertNil(CabalPictureEditor.normalised(""))
        XCTAssertNil(CabalPictureEditor.normalised("   \n "))
        XCTAssertEqual(CabalPictureEditor.normalised("  https://cdn.test/a.jpg "), "https://cdn.test/a.jpg")
    }

    func testABlankSavedUrlIsTreatedAsARemoval() async {
        let writer = StubCabalPictureWriter()
        writer.uploadResult = .success("   ")
        let editor = makeEditor(pictureUrl: "https://cdn.test/old.jpg", writer: writer)

        let outcome = await editor.setPicture(imageData: image, mimeType: "image/jpeg")

        XCTAssertEqual(outcome, .saved(nil))
        XCTAssertNil(editor.pictureUrl)
    }

    // MARK: - Failure copy

    func testFailureMessages() {
        let fallback = "fallback copy"

        XCTAssertEqual(
            CabalPictureEditor.failureMessage(
                for: MonacoAPIError.rejected(status: 400, message: "picture must be a jpeg, png, or webp image", requestID: nil),
                fallback: fallback
            ),
            "picture must be a jpeg, png, or webp image",
            "server copy names the rule that was broken; this screen cannot"
        )
        XCTAssertEqual(
            CabalPictureEditor.failureMessage(for: MonacoAPIError.rateLimited(retryAfter: 12, requestID: nil), fallback: fallback),
            "Too many changes. Try again in 12s."
        )
        XCTAssertEqual(
            CabalPictureEditor.failureMessage(for: MonacoAPIError.httpStatus(404, nil), fallback: fallback),
            "This cabal is no longer available."
        )
        XCTAssertEqual(
            CabalPictureEditor.failureMessage(for: MonacoAPIError.httpStatus(503, nil), fallback: fallback),
            "Cabal pictures are not set up on this server."
        )
        XCTAssertEqual(
            CabalPictureEditor.failureMessage(for: URLError(.notConnectedToInternet), fallback: fallback),
            "Could not reach Monaco. Check your connection."
        )
        XCTAssertEqual(
            CabalPictureEditor.failureMessage(for: MonacoAPIError.missingAccessToken, fallback: fallback),
            "Sign in again to change the cabal picture."
        )
        XCTAssertEqual(
            CabalPictureEditor.failureMessage(for: CocoaError(.fileNoSuchFile), fallback: fallback),
            fallback,
            "an error with no copy of its own falls back"
        )
    }

    // MARK: - Helpers

    /// Polls the main actor until `condition` holds, so a test can observe state
    /// while an async write is parked.
    private func waitUntil(
        timeout: TimeInterval = 2,
        _ condition: @MainActor () -> Bool
    ) async {
        let deadline = Date().addingTimeInterval(timeout)
        while !condition(), Date() < deadline {
            await Task.yield()
            try? await Task.sleep(nanoseconds: 1_000_000)
        }
        XCTAssertTrue(condition(), "condition never became true within \(timeout)s")
    }
}
