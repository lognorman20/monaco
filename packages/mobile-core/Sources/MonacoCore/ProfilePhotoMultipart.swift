import Foundation

/// Multipart body for `POST /v1/me/profile-photo` (single `photo` file field).
public enum ProfilePhotoMultipart {
    public static func body(imageData: Data, mimeType: String, boundary: String) -> Data {
        let fileExtension: String
        switch mimeType {
        case "image/png": fileExtension = "png"
        case "image/webp": fileExtension = "webp"
        default: fileExtension = "jpg"
        }
        var body = Data()
        body.append(Data("--\(boundary)\r\n".utf8))
        body.append(Data("Content-Disposition: form-data; name=\"photo\"; filename=\"profile.\(fileExtension)\"\r\n".utf8))
        body.append(Data("Content-Type: \(mimeType)\r\n\r\n".utf8))
        body.append(imageData)
        body.append(Data("\r\n--\(boundary)--\r\n".utf8))
        return body
    }
}
