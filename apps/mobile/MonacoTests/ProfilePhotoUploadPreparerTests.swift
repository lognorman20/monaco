import ImageIO
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

    /// `prepared` must hop off the main actor: this decode/encode is the work that used to
    /// freeze the picker, spinner and all.
    func testPrepared_offMainActor_producesTheSameResult() async throws {
        let source = try XCTUnwrap(Self.jpeg(width: 2400, height: 1800))

        let prepared = try Self.unwrap(await ProfilePhotoUploadPreparer.prepared(from: source))

        let size = try XCTUnwrap(Self.pixelSize(of: prepared.data))
        XCTAssertEqual(max(size.width, size.height), ProfilePhotoUploadPreparer.maxPixelSize)
    }

    // MARK: - Helpers

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
