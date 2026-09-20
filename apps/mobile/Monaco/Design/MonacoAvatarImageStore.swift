import ImageIO
import SwiftUI
import UIKit

/// Decoded avatars, kept in memory and shared by every `MonacoAvatar`.
///
/// `AsyncImage` keeps nothing between appearances: each time a board row scrolled back into a
/// lazy stack, or a tab came back, it went to the network layer again, decoded the full-size
/// photo on the main thread and flashed the placeholder while it did. Here a photo is fetched
/// once, downsampled off the main thread to the largest size an avatar is ever drawn at, and
/// every later appearance gets the finished bitmap synchronously.
///
/// Keyed by URL only: the backend writes every uploaded photo to a fresh object key, so a new
/// photo is always a new URL.
final class MonacoAvatarImageStore {
    static let shared = MonacoAvatarImageStore()

    /// Longest edge kept, in pixels: the 96pt profile avatar at 3x, with a little headroom.
    nonisolated static let maxPixelSize = 320

    private let cache = NSCache<NSURL, UIImage>()
    private var inFlight: [URL: Task<UIImage?, Never>] = [:]
    private let session: URLSession

    init(session: URLSession = .shared) {
        self.session = session
        cache.countLimit = 200
    }

    func cachedImage(for url: URL) -> UIImage? {
        cache.object(forKey: url as NSURL)
    }

    /// Nil when the photo can't be fetched or isn't an image; the avatar then shows initials.
    /// Rows asking for the same photo at the same time share one download.
    func image(for url: URL) async -> UIImage? {
        if let cached = cachedImage(for: url) { return cached }
        if let running = inFlight[url] { return await running.value }

        let session = session
        let task = Task.detached(priority: .utility) { () -> UIImage? in
            guard let (data, response) = try? await session.data(from: url),
                  (response as? HTTPURLResponse).map({ (200..<300).contains($0.statusCode) }) ?? true else {
                return nil
            }
            return Self.downsampledImage(from: data)
        }
        inFlight[url] = task
        let image = await task.value
        inFlight[url] = nil
        if let image {
            cache.setObject(image, forKey: url as NSURL)
        }
        return image
    }

    /// Decodes straight to the target size so the full-resolution bitmap never exists.
    nonisolated static func downsampledImage(from data: Data) -> UIImage? {
        let sourceOptions = [kCGImageSourceShouldCache: false] as CFDictionary
        guard let source = CGImageSourceCreateWithData(data as CFData, sourceOptions) else { return nil }
        let thumbnailOptions = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixelSize,
        ] as CFDictionary
        guard let thumbnail = CGImageSourceCreateThumbnailAtIndex(source, 0, thumbnailOptions) else { return nil }
        return UIImage(cgImage: thumbnail)
    }
}
