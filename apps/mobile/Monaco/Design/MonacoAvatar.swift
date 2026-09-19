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

    var body: some View {
        Group {
            if let resolvedURL {
                AsyncImage(url: resolvedURL, transaction: Transaction(animation: .easeOut(duration: 0.2))) { phase in
                    switch phase {
                    case .success(let image):
                        image
                            .resizable()
                            .scaledToFill()
                    case .failure:
                        placeholder
                    default:
                        placeholder.overlay {
                            ProgressView()
                                .controlSize(size >= 64 ? .regular : .mini)
                                .tint(MonacoTheme.muted)
                        }
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
