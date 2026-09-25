import MonacoCore
import PhotosUI
import SwiftUI

/// Avatar that opens the face sheet on tap: the eight pixel animals, or the photo library.
/// Either way the pick is uploaded as the signed-in member's profile photo. The one place
/// faces are changed; Profile and the first-run screen embed it.
struct ProfilePhotoPicker: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session

    var size: CGFloat = 96
    /// Overridden by the first-run screen, which QA drives as `onboarding-photo`.
    var accessibilityID: String = "profile-photo-picker"
    /// Debug sample harness only: open the face sheet as soon as the screen is up.
    var initiallyOpen = false
    /// Reports upload results so the host screen can toast them.
    var onResult: (MonacoToast) -> Void

    @State private var showFaces = false
    @State private var isUploading = false

    /// The animal the avatar shows now, so the sheet can ring it. Resolved the way
    /// `MonacoAvatar` resolves it, and nil once a photo is set.
    private var currentAnimal: PixelAnimal? {
        let photo = session.me?.profilePhotoUrl?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard photo.isEmpty else { return nil }
        let id = session.me?.userId ?? ""
        return PixelAnimal.forSeed(id.isEmpty ? (session.me?.displayName ?? "") : id)
    }

    var body: some View {
        Button {
            showFaces = true
        } label: {
            ZStack(alignment: .bottomTrailing) {
                MonacoAvatar(
                    photoURL: session.me?.profilePhotoUrl,
                    displayName: session.me?.displayName ?? "",
                    size: size,
                    seed: session.me?.userId
                )
                .overlay {
                    if isUploading {
                        Circle()
                            .fill(MonacoTheme.canvas.opacity(0.6))
                        ProgressView()
                            .tint(MonacoTheme.ink)
                    }
                }

                Image(systemName: "camera.fill")
                    .font(.system(size: max(11, size * 0.14), weight: .semibold))
                    .foregroundStyle(MonacoTheme.primaryButtonLabel)
                    .frame(width: max(24, size * 0.3), height: max(24, size * 0.3))
                    .background(MonacoTheme.primaryButtonFill, in: Circle())
                    .overlay {
                        Circle().strokeBorder(MonacoTheme.canvas, lineWidth: 2)
                    }
            }
        }
        .buttonStyle(.plain)
        .disabled(isUploading || (auth.accessToken == nil && !initiallyOpen))
        .accessibilityLabel(session.me?.profilePhotoUrl == nil ? "Choose your face" : "Change your face")
        .accessibilityIdentifier(accessibilityID)
        .sheet(isPresented: $showFaces) {
            FacePickerSheet(
                currentAnimal: currentAnimal,
                onPickAnimal: { animal in
                    showFaces = false
                    Task { await wear(animal) }
                },
                onPickPhoto: { item in
                    showFaces = false
                    Task { await upload(item) }
                }
            )
        }
        .onAppear {
            if initiallyOpen { showFaces = true }
        }
    }

    /// An animal is uploaded as a PNG straight from the catalog: the preparer's JPEG
    /// pass is for photos, and would only soften the pixels.
    private func wear(_ animal: PixelAnimal) async {
        guard let data = UIImage(named: animal.imageName)?.pngData() else {
            onResult(MonacoToast(message: "That face is missing. Try another.", isSuccess: false))
            return
        }
        await save(data, mimeType: "image/png", success: "You're \(animal.withArticle) now.")
    }

    private func upload(_ item: PhotosPickerItem) async {
        let data: Data?
        do {
            data = try await item.loadTransferable(type: Data.self)
        } catch {
            data = nil
        }
        guard let data else {
            onResult(MonacoToast(message: "Could not read that photo.", isSuccess: false))
            return
        }
        let prepared: ProfilePhotoUploadPreparer.Prepared
        switch await ProfilePhotoUploadPreparer.prepared(from: data) {
        case .success(let ready):
            prepared = ready
        case .failure(let failure):
            onResult(MonacoToast(message: failure.memberMessage, isSuccess: false))
            return
        }

        await save(prepared.data, mimeType: prepared.mimeType, success: "Profile photo updated.")
    }

    private func save(_ data: Data, mimeType: String, success: String) async {
        isUploading = true
        defer { isUploading = false }
        switch await session.uploadProfilePhoto(data, mimeType: mimeType, auth: auth) {
        case .saved, .unchanged:
            onResult(MonacoToast(message: success, isSuccess: true))
        case .failed(let message):
            onResult(MonacoToast(message: message, isSuccess: false))
        }
    }
}
