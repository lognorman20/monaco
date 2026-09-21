import MonacoCore
import SwiftUI

/// Circular profile photo with an initials placeholder. Used on Profile
/// and board rows.
struct MonacoAvatar: View {
    let photoURL: String?
    let displayName: String
    var size: CGFloat = 44

    private var resolvedURL: URL? {
        let trimmed = photoURL?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !trimmed.isEmpty else { return nil }
        return URL(string: trimmed)
    }

    @State private var loadedImage: UIImage?
    @State private var didFail = false

    var body: some View {
        Group {
            if let resolvedURL {
                // A photo seen before is drawn on the first pass, with no placeholder flash.
                if let image = loadedImage ?? MonacoRemoteImageStore.avatars.cachedImage(for: resolvedURL) {
                    Image(uiImage: image)
                        .resizable()
                        .scaledToFill()
                } else if didFail {
                    placeholder
                } else {
                    placeholder.overlay {
                        ProgressView()
                            .controlSize(size >= 64 ? .regular : .mini)
                            .tint(MonacoTheme.muted)
                    }
                }
            } else {
                placeholder
            }
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .overlay {
            Circle().strokeBorder(MonacoTheme.hairline, lineWidth: 1)
        }
        .accessibilityHidden(true)
        .task(id: resolvedURL) {
            await loadPhoto()
        }
    }

    private func loadPhoto() async {
        loadedImage = nil
        didFail = false
        guard let resolvedURL else { return }
        let store = MonacoRemoteImageStore.avatars
        if let cached = store.cachedImage(for: resolvedURL) {
            loadedImage = cached
            return
        }
        let image = await store.image(for: resolvedURL)
        guard !Task.isCancelled else { return }
        withAnimation(.easeOut(duration: 0.2)) {
            loadedImage = image
            didFail = image == nil
        }
    }

    private var placeholder: some View {
        let initials = AvatarInitials.from(displayName)
        return ZStack {
            Circle().fill(MonacoTheme.canvasWash)
            if initials.isEmpty {
                Image(systemName: "person.fill")
                    .font(.system(size: size * 0.42, weight: .semibold))
                    .foregroundStyle(MonacoTheme.accent)
            } else {
                Text(initials)
                    .font(.custom("AvenirNext-DemiBold", size: size * 0.38))
                    .foregroundStyle(MonacoTheme.accent)
                    .minimumScaleFactor(0.5)
                    .lineLimit(1)
            }
        }
    }
}

#Preview {
    HStack(spacing: 16) {
        MonacoAvatar(photoURL: nil, displayName: "Logan Norman", size: 96)
        MonacoAvatar(photoURL: nil, displayName: "", size: 44)
        MonacoAvatar(photoURL: nil, displayName: "Ana", size: 28)
    }
    .padding()
    .monacoCanvas()
}
