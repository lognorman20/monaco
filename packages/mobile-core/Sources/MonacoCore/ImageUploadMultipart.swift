import Foundation

/// Multipart body for the backend's single-file image uploads: the member's
/// profile photo and the cabal's picture. Both endpoints take one file field
/// and nothing else, so one builder covers them.
public enum ImageUploadMultipart {
    /// The file extension the backend's sniffer will agree with. The server
    /// decides the real format from the bytes, so this only makes the part
    /// readable in a request log.
    static func fileExtension(for mimeType: String) -> String {
        switch mimeType {
        case "image/png": "png"
        case "image/webp": "webp"
        default: "jpg"
        }
    }

    /// Builds a body with one file part named `fieldName`.
    public static func body(
        fieldName: String,
        fileBaseName: String,
        imageData: Data,
        mimeType: String,
        boundary: String
    ) -> Data {
        let name = "\(fileBaseName).\(fileExtension(for: mimeType))"
        var body = Data()
        body.append(Data("--\(boundary)\r\n".utf8))
        body.append(Data("Content-Disposition: form-data; name=\"\(fieldName)\"; filename=\"\(name)\"\r\n".utf8))
        body.append(Data("Content-Type: \(mimeType)\r\n\r\n".utf8))
        body.append(imageData)
        body.append(Data("\r\n--\(boundary)--\r\n".utf8))
        return body
    }
}
