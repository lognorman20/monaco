import ImageIO
import SwiftUI
import UIKit

/// Decoded remote images, kept in memory and shared by every view that draws one.
///
/// `AsyncImage` keeps nothing between appearances: each time a board row scrolled back into a
/// lazy stack, or a tab came back, it went to the network layer again, decoded the full-size
/// photo on the main thread and flashed the placeholder while it did. Here a photo is fetched
/// once, downsampled off the main thread to the largest size it is ever drawn at, and every
/// later appearance gets the finished bitmap synchronously.
///
/// Keyed by URL only: the backend writes every uploaded photo to a fresh object key, and a
/// company logo lives at a stable one, so a new image is always a new URL.
///
/// There is one store per size class rather than one for everything, because the point of
/// downsampling is the target size: a 44pt stock mark decoded at avatar resolution is four
/// times the bitmap it needs, and a screen of market rows is twenty of them.
final class MonacoRemoteImageStore {
    /// Longest edge kept for profile photos: the 96pt profile avatar at 3x, with a little headroom.
    nonisolated static let avatarMaxPixelSize = 320
    /// Longest edge kept for the square marks on market rows: 44pt at 3x.
    nonisolated static let markMaxPixelSize = 132

    static let avatars = MonacoRemoteImageStore(maxPixelSize: avatarMaxPixelSize, countLimit: 200)
    /// Logos outnumber avatars on screen (every market row and holding draws one) and each
    /// bitmap is small, so this one holds more of them.
    static let stockLogos = MonacoRemoteImageStore(maxPixelSize: markMaxPixelSize, countLimit: 400)

    private let cache = NSCache<NSURL, UIImage>()
    private var inFlight: [URL: Task<UIImage?, Never>] = [:]
    private let session: URLSession
    private let maxPixelSize: Int

    init(session: URLSession = .shared, maxPixelSize: Int = MonacoRemoteImageStore.avatarMaxPixelSize, countLimit: Int = 200) {
        self.session = session
        self.maxPixelSize = maxPixelSize
        cache.countLimit = countLimit
    }

    func cachedImage(for url: URL) -> UIImage? {
        cache.object(forKey: url as NSURL)
    }

    /// Nil when the image can't be fetched or isn't an image; the caller then shows its
    /// fallback. Views asking for the same URL at the same time share one download.
    func image(for url: URL) async -> UIImage? {
        if let cached = cachedImage(for: url) { return cached }
        if let running = inFlight[url] { return await running.value }

        let session = session
        let maxPixelSize = maxPixelSize
        let task = Task.detached(priority: .utility) { () -> UIImage? in
            guard let (data, response) = try? await session.data(from: url),
                  (response as? HTTPURLResponse).map({ (200..<300).contains($0.statusCode) }) ?? true else {
                return nil
            }
            return Self.downsampledImage(from: data, maxPixelSize: maxPixelSize)
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
    nonisolated static func downsampledImage(from data: Data, maxPixelSize: Int) -> UIImage? {
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
