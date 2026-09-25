import MonacoCore
import PhotosUI
import SwiftUI

/// The cabal's mark, and — for its creator — the control that changes it.
///
/// A member who did not create the cabal gets the plain mark: no badge, no tap
/// target, nothing to discover that would only answer 403. The creator gets the
/// same mark with a camera badge, a photo picker on tap, and "Remove picture" in
/// a long-press menu once there is one to remove.
struct CabalPicturePicker: View {
    let groupId: String
    let name: String
    /// Only the creator is offered the controls. The server checks again anyway.
    let canEdit: Bool
    var size: CGFloat = 36
    var onInk: Bool = false
    /// Reports results so the host screen can toast them.
    var onResult: (MonacoToast) -> Void = { _ in }

    @ObservedObject var editor: CabalPictureEditor

    @State private var selection: PhotosPickerItem?

    var body: some View {
        Group {
            if canEdit {
                editableMark
            } else {
                CabalMark(
                    groupId: groupId,
                    name: name,
                    size: size,
                    onInk: onInk,
                    pictureUrl: editor.pictureUrl,
                    accessibilityLabel: readOnlyAccessibilityLabel
                )
            }
        }
        .onChange(of: selection) { _, item in
            guard let item else { return }
            Task { await upload(item) }
        }
    }

    private var editableMark: some View {
        // Built out here and handed to the picker whole: its label builder is not main-actor
        // isolated and the editor is, so reading the editor inside it is an isolation error
        // the compiler was only warning about. The picker re-renders with the editor, so the
        // label is never stale.
        let label = editableLabel(pictureUrl: editor.pictureUrl, isWorking: editor.isWorking)
        return PhotosPicker(selection: $selection, matching: .images, photoLibrary: .shared()) {
            label
        }
        .buttonStyle(.plain)
        .disabled(editor.isWorking)
        .accessibilityLabel(editAccessibilityLabel)
        .accessibilityHint("Opens your photo library")
        .accessibilityIdentifier("cabal-picture-picker")
        .contextMenu {
            if editor.pictureUrl != nil {
                Button(role: .destructive) {
                    Task { await remove() }
                } label: {
                    Label("Remove picture", systemImage: "trash")
                }
                .accessibilityIdentifier("cabal-picture-remove")
            }
        }
    }

    private func editableLabel(pictureUrl: String?, isWorking: Bool) -> some View {
        ZStack(alignment: .bottomTrailing) {
            CabalMark(
                groupId: groupId,
                name: name,
                size: size,
                onInk: onInk,
                pictureUrl: pictureUrl
            )
            .overlay {
                if isWorking {
                    // The mark's own corner, so the veil covers the tile and nothing else.
                    RoundedRectangle(cornerRadius: size * MonacoTheme.Radius.tile / 44, style: .continuous)
                        .fill(MonacoTheme.canvas.opacity(0.6))
                    ProgressView()
                        .controlSize(size >= 64 ? .regular : .mini)
                        .tint(MonacoTheme.ink)
                }
            }
            cameraBadge
        }
    }

    /// The camera on the mark's corner. On the hero's ink it is paper with an ink glyph: in the
    /// brand fill it was ink on ink, and only its ring showed. Off the hero it is the brand fill.
    private var cameraBadge: some View {
        let diameter = max(18, size * 0.44)
        return Image(systemName: "camera.fill")
            .font(.system(size: max(9, size * 0.22), weight: .semibold))
            .foregroundStyle(onInk ? MonacoTheme.heroInk : MonacoTheme.primaryButtonLabel)
            .frame(width: diameter, height: diameter)
            .background(onInk ? MonacoTheme.onHero : MonacoTheme.primaryButtonFill, in: Circle())
            .overlay { Circle().strokeBorder(onInk ? MonacoTheme.heroInk : MonacoTheme.canvas, lineWidth: 1.5) }
            .offset(x: 5, y: 5)
    }

    /// VoiceOver still needs to be told there is a picture, even where the mark
    /// is decoration for sighted members.
    private var readOnlyAccessibilityLabel: String {
        editor.pictureUrl == nil ? "\(name), no picture" : "\(name) picture"
    }

    private var editAccessibilityLabel: String {
        if editor.isWorking { return "Updating cabal picture" }
        return editor.pictureUrl == nil ? "Add cabal picture" : "Change cabal picture"
    }

    private func upload(_ item: PhotosPickerItem) async {
        defer { selection = nil }

        let data: Data?
        do {
            data = try await item.loadTransferable(type: Data.self)
        } catch {
            data = nil
        }
        guard let data else {
            onResult(MonacoToast(message: "Could not read that picture.", isSuccess: false))
            return
        }

        // The same preparer profile photos use: downsample and re-encode off the
        // main thread, so the cap is met before the bytes leave the phone.
        let prepared: ProfilePhotoUploadPreparer.Prepared
        switch await ProfilePhotoUploadPreparer.prepared(from: data) {
        case .success(let ready):
            prepared = ready
        case .failure(.unreadable):
            onResult(MonacoToast(message: "That picture could not be opened. Try another.", isSuccess: false))
            return
        case .failure(.tooLarge):
            onResult(MonacoToast(message: "That picture is too big to upload. Try another.", isSuccess: false))
            return
        }

        switch await editor.setPicture(imageData: prepared.data, mimeType: prepared.mimeType) {
        case .saved:
            onResult(MonacoToast(message: "Cabal picture updated.", isSuccess: true))
        case .failed(let message):
            onResult(MonacoToast(message: message, isSuccess: false))
        }
    }

    private func remove() async {
        switch await editor.removePicture() {
        case .saved:
            onResult(MonacoToast(message: "Cabal picture removed.", isSuccess: true))
        case .failed(let message):
            onResult(MonacoToast(message: message, isSuccess: false))
        }
    }
}
