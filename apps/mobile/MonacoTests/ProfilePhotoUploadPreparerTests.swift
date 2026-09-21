import ImageIO
import MonacoCore
import UIKit
import XCTest
@testable import Monaco

@MainActor
final class ProfilePhotoUploadPreparerTests: XCTestCase {
    private struct NotPrepared: Error { let failure: ProfilePhotoUploadPreparer.Failure }

    /// The bug this locks out: `UIGraphicsImageRenderer` rendered at the screen's scale, so
    /// a "1024 max" target came out at 3072px on every current iPhone.
    func testPrepare_capsTheLongestEdgeInPixelsNotPoints() throws {
        let source = try XCTUnwrap(Self.jpeg(width: 2400, height: 1800))

        let prepared = try Self.unwrap(ProfilePhotoUploadPreparer.prepare(from: source))

        let size = try XCTUnwrap(Self.pixelSize(of: prepared.data))
        XCTAssertEqual(max(size.width, size.height), ProfilePhotoUploadPreparer.maxPixelSize)
        XCTAssertEqual(
            Double(size.width) / Double(size.height),
            2400.0 / 1800.0,
            accuracy: 0.01,
            "aspect ratio must survive the downsample"
        )
        XCTAssertEqual(prepared.mimeType, "image/jpeg")
    }

    func testPrepare_staysUnderTheUploadCap() throws {
        let source = try XCTUnwrap(Self.jpeg(width: 2400, height: 1800))

        let prepared = try Self.unwrap(ProfilePhotoUploadPreparer.prepare(from: source))

        XCTAssertLessThanOrEqual(prepared.data.count, ProfilePhotoUploadPreparer.maxBytes)
    }

    func testPrepare_smallPhotoIsNotUpscaled() throws {
        let source = try XCTUnwrap(Self.jpeg(width: 240, height: 240))

        let prepared = try Self.unwrap(ProfilePhotoUploadPreparer.prepare(from: source))

        let size = try XCTUnwrap(Self.pixelSize(of: prepared.data))
        XCTAssertEqual(max(size.width, size.height), 240)
    }

    /// An unreadable pick and an oversized one read differently to the member, so they must
    /// not collapse into one "could not be shrunk under 2MB" message.
    func testPrepare_undecodableDataIsUnreadableNotTooLarge() {
        XCTAssertEqual(
            ProfilePhotoUploadPreparer.prepare(from: Data("<html>not an image</html>".utf8)),
            .failure(.unreadable)
        )
        XCTAssertEqual(ProfilePhotoUploadPreparer.prepare(from: Data()), .failure(.unreadable))
    }

    /// Bytes that were produced and measured over the cap are the only thing allowed to tell
    /// the member their photo is too big. At the real 2MB cap a 1024px JPEG never gets there,
    /// which is how this branch shipped untested — so the cap is a parameter.
    func testPrepare_overTheCapIsTooLarge() throws {
        let source = try XCTUnwrap(Self.jpeg(width: 2400, height: 1800))

        XCTAssertEqual(
            ProfilePhotoUploadPreparer.prepare(from: source, maxBytes: 64),
            .failure(.tooLarge)
        )
    }

    /// The decode/encode is what used to freeze the picker, spinner and all, so it must not be
    /// able to run on the main actor.
    ///
    /// The assertion is in two halves. This helper is `nonisolated`, so if `prepare` stopped
    /// being `nonisolated` — the target defaults every declaration to `@MainActor` — the call
    /// below would not compile. And the work is checked at run time to have happened on a
    /// thread that is not the main one, which the previous version of this test, asserting
    /// only the output pixel size, would have passed without.
    func testPrepare_runsOffTheMainThreadAndAgreesWithTheMainActorResult() async throws {
        let source = try XCTUnwrap(Self.jpeg(width: 2400, height: 1800))

        let (offMain, wasOnMainThread) = await Self.prepareOffTheMainActor(source)

        XCTAssertFalse(wasOnMainThread, "the decode and encode must not happen on the main thread")
        XCTAssertEqual(offMain, ProfilePhotoUploadPreparer.prepare(from: source))
    }

    /// And `prepared` is the entry point the picker calls, which has to do that hop for it.
    func testPrepared_hopsOffTheMainActorForTheCaller() async throws {
        let source = try XCTUnwrap(Self.jpeg(width: 2400, height: 1800))

        let prepared = try Self.unwrap(await ProfilePhotoUploadPreparer.prepared(from: source))

        let size = try XCTUnwrap(Self.pixelSize(of: prepared.data))
        XCTAssertEqual(max(size.width, size.height), ProfilePhotoUploadPreparer.maxPixelSize)
    }

    /// The member-facing half. Both sentences say what happened and what to do next, and
    /// neither may reach for the vocabulary the product voice keeps out of the main flows.
    func testFailureCopy_saysWhatToDoAndPassesTheMainFlowAudit() {
        XCTAssertEqual(
            ProfilePhotoUploadPreparer.Failure.unreadable.memberMessage,
            "That photo could not be opened. Try another."
        )
        XCTAssertEqual(
            ProfilePhotoUploadPreparer.Failure.tooLarge.memberMessage,
            "That photo is too big to upload. Try another."
        )
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean([
            ProfilePhotoUploadPreparer.Failure.unreadable.memberMessage,
            ProfilePhotoUploadPreparer.Failure.tooLarge.memberMessage,
        ]))
    }

    // MARK: - Helpers

    /// Deliberately `nonisolated`: it is what makes the `prepare` call below a compile-time
    /// check that the preparer never drifts back onto the main actor.
    private nonisolated static func prepareOffTheMainActor(
        _ data: Data
    ) async -> (Result<ProfilePhotoUploadPreparer.Prepared, ProfilePhotoUploadPreparer.Failure>, Bool) {
        await Task.detached(priority: .userInitiated) {
            (ProfilePhotoUploadPreparer.prepare(from: data), Thread.isMainThread)
        }.value
    }

    private static func unwrap(
        _ result: Result<ProfilePhotoUploadPreparer.Prepared, ProfilePhotoUploadPreparer.Failure>
    ) throws -> ProfilePhotoUploadPreparer.Prepared {
        switch result {
        case .success(let prepared):
            return prepared
        case .failure(let failure):
            XCTFail("expected a prepared photo, got \(failure)")
            throw NotPrepared(failure: failure)
        }
    }

    /// Reads the stored dimensions without decoding, so the assertion is about real pixels.
    private static func pixelSize(of data: Data) -> (width: Int, height: Int)? {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil),
              let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
              let width = properties[kCGImagePropertyPixelWidth] as? Int,
              let height = properties[kCGImagePropertyPixelHeight] as? Int
        else { return nil }
        return (width, height)
    }

    /// Noise rather than flat colour: a flat fill compresses to almost nothing and would
    /// never exercise the quality steps.
    private static func jpeg(width: Int, height: Int) -> Data? {
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        format.opaque = true
        let renderer = UIGraphicsImageRenderer(size: CGSize(width: width, height: height), format: format)
        return renderer.jpegData(withCompressionQuality: 1.0) { context in
            for x in stride(from: 0, to: width, by: 8) {
                for y in stride(from: 0, to: height, by: 8) {
                    UIColor(
                        red: .random(in: 0...1),
                        green: .random(in: 0...1),
                        blue: .random(in: 0...1),
                        alpha: 1
                    ).setFill()
                    context.fill(CGRect(x: x, y: y, width: 8, height: 8))
                }
            }
        }
    }
}
