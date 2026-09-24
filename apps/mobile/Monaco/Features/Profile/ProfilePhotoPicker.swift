import MonacoCore
import PhotosUI
import SwiftUI

/// Avatar that opens the photo library on tap and uploads the pick for the signed-in
/// user. The one place profile photos are changed; Profile embeds it.
struct ProfilePhotoPicker: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session

    var size: CGFloat = 96
    /// Overridden by the first-run screen, which QA drives as `onboarding-photo`.
    var accessibilityID: String = "profile-photo-picker"
    /// Reports upload results so the host screen can toast them.
    var onResult: (MonacoToast) -> Void

    @State private var selection: PhotosPickerItem?
    @State private var isUploading = false

    var body: some View {
        PhotosPicker(selection: $selection, matching: .images, photoLibrary: .shared()) {
            ZStack(alignment: .bottomTrailing) {
                MonacoAvatar(
                    photoURL: session.me?.profilePhotoUrl,
                    displayName: session.me?.displayName ?? "",
                    size: size
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
        .disabled(isUploading || auth.accessToken == nil)
        .accessibilityLabel(session.me?.profilePhotoUrl == nil ? "Add profile photo" : "Change profile photo")
        .accessibilityIdentifier(accessibilityID)
        .onChange(of: selection) { _, item in
            guard let item else { return }
            Task { await upload(item) }
        }
    }

    private func upload(_ item: PhotosPickerItem) async {
        isUploading = true
        defer {
            isUploading = false
            selection = nil
        }

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

        switch await session.uploadProfilePhoto(prepared.data, mimeType: prepared.mimeType, auth: auth) {
        case .saved, .unchanged:
            onResult(MonacoToast(message: "Profile photo updated.", isSuccess: true))
        case .failed(let message):
            onResult(MonacoToast(message: message, isSuccess: false))
        }
    }
}
