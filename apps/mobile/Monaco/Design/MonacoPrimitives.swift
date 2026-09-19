import SwiftUI

/// Light gray sheet with a grayscale wash at the top.
struct MonacoCanvasBackground: View {
    var body: some View {
        ZStack(alignment: .top) {
            MonacoTheme.canvas
            LinearGradient(
                stops: [
                    .init(color: MonacoTheme.canvasWash, location: 0),
                    .init(color: MonacoTheme.canvasWash.opacity(0.28), location: 0.55),
                    .init(color: MonacoTheme.canvas.opacity(0), location: 1),
                ],
                startPoint: .top,
                endPoint: .bottom
            )
            .frame(height: 128)
            .allowsHitTesting(false)
        }
        .ignoresSafeArea()
    }
}

/// Canvas + ink for a tab root or pushed screen.
struct MonacoScreen<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .monacoCanvas()
            .foregroundStyle(MonacoTheme.ink)
    }
}

/// Rounded search field matching Orbix browse chrome.
struct MonacoSearchField: View {
    var placeholder: String
    @Binding var text: String
    var isEnabled: Bool = true

    var body: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            Image(systemName: "magnifyingglass")
                .foregroundStyle(MonacoTheme.muted)
                .symbolRenderingMode(.hierarchical)
            TextField(placeholder, text: $text)
                .font(MonacoTheme.TypeRole.body)
                .foregroundStyle(MonacoTheme.ink)
                .disabled(!isEnabled)
                .accessibilityIdentifier("monaco-search-field")
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, 12)
        .background(MonacoTheme.surface, in: Capsule())
        .overlay {
            Capsule().strokeBorder(MonacoTheme.hairline, lineWidth: 1)
        }
        .opacity(isEnabled ? 1 : 0.7)
    }
}

/// Large-radius surface. Hairline only — no decorative shadow.
struct MonacoCard<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        content
            .padding(MonacoTheme.Space.m)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                MonacoTheme.surface,
                in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                    .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
            }
    }
}

/// Filter / range pill.
struct MonacoChip: View {
    let title: String
    var isSelected: Bool = false

    var body: some View {
        Text(title)
            .font(MonacoTheme.TypeRole.caption.weight(.semibold))
            .foregroundStyle(isSelected ? MonacoTheme.primaryButtonLabel : MonacoTheme.ink)
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
            .background(
                Capsule().fill(isSelected ? MonacoTheme.primaryButtonFill : MonacoTheme.surface)
            )
            .overlay {
                Capsule().strokeBorder(isSelected ? Color.clear : MonacoTheme.hairline, lineWidth: 1)
            }
    }
}

/// Large figure + caption (Home net worth / Profile name).
struct MonacoHeroHeader: View {
    let title: String
    let caption: String

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text(caption)
                .font(MonacoTheme.TypeRole.caption)
                .foregroundStyle(MonacoTheme.muted)
            Text(title)
                .font(MonacoTheme.TypeRole.display)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(2)
                .minimumScaleFactor(0.7)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Image-or-mark + title + trailing metric.
struct MonacoRowCard<Leading: View>: View {
    let title: String
    let subtitle: String?
    let trailing: String?
    var subtitleColor: Color = MonacoTheme.muted
    var trailingColor: Color = MonacoTheme.ink
    let leading: Leading

    init(
        title: String,
        subtitle: String?,
        trailing: String?,
        subtitleColor: Color = MonacoTheme.muted,
        trailingColor: Color = MonacoTheme.ink,
        @ViewBuilder leading: () -> Leading
    ) {
        self.title = title
        self.subtitle = subtitle
        self.trailing = trailing
        self.subtitleColor = subtitleColor
        self.trailingColor = trailingColor
        self.leading = leading()
    }

    var body: some View {
        HStack(spacing: MonacoTheme.Space.m) {
            leading
                .frame(width: 44, height: 44)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(MonacoTheme.TypeRole.title)
                    .foregroundStyle(MonacoTheme.ink)
                if let subtitle, !subtitle.isEmpty {
                    Text(subtitle)
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(subtitleColor)
                }
            }
            Spacer(minLength: 8)
            if let trailing, !trailing.isEmpty {
                Text(trailing)
                    .font(.subheadline.monospacedDigit())
                    .foregroundStyle(trailingColor)
            }
        }
        .padding(MonacoTheme.Space.m)
        .background(
            MonacoTheme.surface,
            in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
        }
    }
}

/// SF Symbol tile used as the default `MonacoRowCard` leading mark.
struct MonacoRowIcon: View {
    let systemImage: String

    var body: some View {
        Image(systemName: systemImage)
            .font(.title3)
            .foregroundStyle(MonacoTheme.accent)
            .symbolRenderingMode(.hierarchical)
            .frame(width: 44, height: 44)
            .background(MonacoTheme.canvas, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }
}

extension MonacoRowCard where Leading == MonacoRowIcon {
    init(
        systemImage: String,
        title: String,
        subtitle: String?,
        trailing: String?,
        subtitleColor: Color = MonacoTheme.muted,
        trailingColor: Color = MonacoTheme.ink
    ) {
        self.init(
            title: title,
            subtitle: subtitle,
            trailing: trailing,
            subtitleColor: subtitleColor,
            trailingColor: trailingColor
        ) {
            MonacoRowIcon(systemImage: systemImage)
        }
    }
}
