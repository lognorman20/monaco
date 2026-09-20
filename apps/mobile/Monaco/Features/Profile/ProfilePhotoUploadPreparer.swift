import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

/// Turns a photo picked from the library into the JPEG the profile endpoint accepts.
///
/// `nonisolated` on purpose: the target defaults every declaration to `@MainActor`, and this
/// work (decode, downsample, encode) is exactly what must not run on the main thread. It goes
/// through ImageIO rather than `UIImage` + `UIGraphicsImageRenderer` for two reasons: a
/// thumbnail is decoded straight to the target size, so a 48MP original never becomes a
/// full-size bitmap in memory, and `maxPixelSize` counts *pixels* — the renderer used the
/// screen's scale, so the old "1024 max" produced a 3072px image on every current iPhone.
nonisolated enum ProfilePhotoUploadPreparer {
    /// Backend accepts at most 2MB for the photo bytes; leave room for multipart framing.
    static let maxBytes = (2 * 1024 * 1024) - 4096
    /// Longest edge kept, in pixels. Avatars are drawn at 320px at most
    /// (`MonacoRemoteImageStore.avatarMaxPixelSize`), so this is already generous.
    static let maxPixelSize = 1024

    /// Why a pick could not be turned into an upload. The two cases read differently to the
    /// member, so they must not collapse into one "could not be shrunk" message.
    enum Failure: Error, Equatable, Sendable {
        /// Nothing here this device can decode: a RAW the decoder refuses, a corrupt file,
        /// an iCloud photo that never finished downloading.
        case unreadable
        /// Decoded fine, but still over the cap at the smallest size and quality we send.
        case tooLarge
    }

    struct Prepared: Equatable, Sendable {
        let data: Data
        let mimeType: String
    }

    /// Prepares off the main actor, so the picker's spinner can actually spin.
    static func prepared(from data: Data) async -> Result<Prepared, Failure> {
        await Task.detached(priority: .userInitiated) { prepare(from: data) }.value
    }

    /// One downsample at 1024px, then quality steps. The fallback to 512px only exists as a
    /// guard: a 1024px JPEG at 0.8 lands far under 2MB.
    static func prepare(from data: Data) -> Result<Prepared, Failure> {
        let sourceOptions = [kCGImageSourceShouldCache: false] as CFDictionary
        guard let source = CGImageSourceCreateWithData(data as CFData, sourceOptions) else {
            return .failure(.unreadable)
        }

        var decodedAnything = false
        for pixelSize in [maxPixelSize, maxPixelSize / 2] {
            guard let image = downsample(source, maxPixelSize: pixelSize) else { continue }
            decodedAnything = true
            for quality in [0.8, 0.6, 0.4] as [CGFloat] {
                if let jpeg = encodeJPEG(image, quality: quality), jpeg.count <= maxBytes {
                    return .success(Prepared(data: jpeg, mimeType: "image/jpeg"))
                }
            }
        }
        return .failure(decodedAnything ? .tooLarge : .unreadable)
    }

    private static func downsample(_ source: CGImageSource, maxPixelSize: Int) -> CGImage? {
        let options = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            // Applies the EXIF orientation, which `image.draw(in:)` used to handle.
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixelSize,
        ] as CFDictionary
        return CGImageSourceCreateThumbnailAtIndex(source, 0, options)
    }

    private static func encodeJPEG(_ image: CGImage, quality: CGFloat) -> Data? {
        let buffer = NSMutableData()
        guard let destination = CGImageDestinationCreateWithData(
            buffer,
            UTType.jpeg.identifier as CFString,
            1,
            nil
        ) else { return nil }
        CGImageDestinationAddImage(
            destination,
            image,
            [kCGImageDestinationLossyCompressionQuality: quality] as CFDictionary
        )
        guard CGImageDestinationFinalize(destination) else { return nil }
        return buffer as Data
    }
}
