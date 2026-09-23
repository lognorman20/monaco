import MonacoCore
import SwiftUI

/// Circular profile photo with an initials placeholder. Used on Profile, board rows, chat runs,
/// feed rows and — with a ring — the vote tally.
struct MonacoAvatar: View {
    let photoURL: String?
    let displayName: String
    var size: CGFloat = 44

    /// The ring drawn around the avatar. `nil` keeps the default 1pt hairline.
    ///
    /// `MonacoVoteFace` passes a 2pt `brand` or `loss` ring, because on a screen deciding whether
    /// real money gets spent the face has to say *how* someone voted, not just that they did.
    var ring: Color?

    var ringWidth: CGFloat = 1

    private var resolvedURL: URL? {
        let trimmed = photoURL?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !trimmed.isEmpty else { return nil }
        return URL(string: trimmed)
    }

    @State private var loadedImage: UIImage?
    @State private var didFail = false

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Group {
            if let resolvedURL {
                // A photo seen before is drawn on the first pass, with no placeholder flash.
                if let image = loadedImage ?? MonacoAvatarImageStore.shared.cachedImage(for: resolvedURL) {
                    Image(uiImage: image)
                        .resizable()
                        .scaledToFill()
                } else if didFail {
                    placeholder
                } else {
                    placeholder.overlay {
                        ProgressView()
                            .controlSize(size >= 64 ? .regular : .mini)
                            // A placeholder spinner, not an accent: deliberately quiet, and
                            // deliberately not `controlTint`, which would make a loading avatar
                            // louder than a loaded one.
                            .tint(MonacoTheme.fgMuted)
                    }
                }
            } else {
                placeholder
            }
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .overlay {
            Circle().strokeBorder(ring ?? MonacoTheme.line, lineWidth: ring == nil ? 1 : ringWidth)
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
        let store = MonacoAvatarImageStore.shared
        if let cached = store.cachedImage(for: resolvedURL) {
            loadedImage = cached
            return
        }
        let image = await store.image(for: resolvedURL)
        guard !Task.isCancelled else { return }
        withAnimation(MonacoMotion.glide.reduced(reduceMotion)) {
            loadedImage = image
            didFail = image == nil
        }
    }

    /// Initials are sized against the disc *inside* the ring, not the whole frame. A 2pt vote ring
    /// on a 22pt face takes 4pt of diameter, and initials scaled against the full 22 ran under it.
    private var innerSize: CGFloat {
        max(size - (ring == nil ? 0 : ringWidth * 2), 1)
    }

    private var placeholder: some View {
        let initials = AvatarInitials.from(displayName)
        return ZStack {
            Circle().fill(MonacoTheme.fillQuiet)
            // Initials are `fgMuted`, not the brand accent. Blue only ever means tap, and an
            // avatar is not a tap target — a blue-lettered face next to a red "voted no" ring was
            // the clearest case of the accent being used as decoration. Muted is also the right
            // weight: the face supports the name beside it, it is not the headline.
            if initials.isEmpty {
                Image(systemName: "person.fill")
                    .font(.system(size: innerSize * 0.42, weight: .semibold))
                    .foregroundStyle(MonacoTheme.fgMuted)
            } else {
                Text(initials)
                    .font(.system(size: innerSize * 0.38, weight: .bold))
                    .foregroundStyle(MonacoTheme.fgMuted)
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
