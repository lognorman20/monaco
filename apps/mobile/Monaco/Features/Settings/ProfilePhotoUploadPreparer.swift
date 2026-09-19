import UIKit

enum ProfilePhotoUploadPreparer {
    /// Backend accepts at most 2MB for the photo bytes; leave room for multipart framing.
    private static let maxBytes = (2 * 1024 * 1024) - 4096
    private static let initialMaxDimension: CGFloat = 1024

    static func prepare(from data: Data) -> (Data, String)? {
        guard let image = UIImage(data: data) else { return nil }

        var maxDimension = initialMaxDimension
        while maxDimension >= 256 {
            let scaled = scale(image, maxDimension: maxDimension)
            var quality: CGFloat = 0.85
            while quality >= 0.4 {
                if let jpeg = scaled.jpegData(compressionQuality: quality), jpeg.count <= maxBytes {
                    return (jpeg, "image/jpeg")
                }
                quality -= 0.1
            }
            maxDimension *= 0.75
        }
        return nil
    }

    private static func scale(_ image: UIImage, maxDimension: CGFloat) -> UIImage {
        let size = image.size
        let maxSide = max(size.width, size.height)
        guard maxSide > maxDimension else { return image }

        let scaleFactor = maxDimension / maxSide
        let newSize = CGSize(width: size.width * scaleFactor, height: size.height * scaleFactor)
        let renderer = UIGraphicsImageRenderer(size: newSize)
        return renderer.image { _ in
            image.draw(in: CGRect(origin: .zero, size: newSize))
        }
    }
}
