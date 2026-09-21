import Foundation

/// Multipart body for `POST /v1/me/profile-photo` (single `photo` file field).
public enum ProfilePhotoMultipart {
    public static func body(imageData: Data, mimeType: String, boundary: String) -> Data {
        ImageUploadMultipart.body(
            fieldName: "photo",
            fileBaseName: "profile",
            imageData: imageData,
            mimeType: mimeType,
            boundary: boundary
        )
    }
}
